/*
 *  Copyright (c) 2026 Mikhail Knyazhev <markus621@yandex.com>. All rights reserved.
 *  Use of this source code is governed by a MIT license that can be found in the LICENSE file.
 */

package bridge

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/coder/websocket"
)

var ErrUnavailable = errors.New("browser extension is not connected")

const maxWebSocketMessageSize = 5 << 20 // 5 MiB, including base64 image payloads

type Hub struct {
	token   string
	timeout time.Duration

	mu      sync.Mutex
	writeMu sync.Mutex
	conn    *websocket.Conn
	pending map[string]chan Envelope
	nextID  uint64
}

func NewHub(token string, timeout time.Duration) *Hub {
	return &Hub{token: token, timeout: timeout, pending: make(map[string]chan Envelope)}
}

func (h *Hub) Connected() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conn != nil
}

func (h *Hub) Serve(conn *websocket.Conn) {
	conn.SetReadLimit(maxWebSocketMessageSize)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, raw, err := conn.Read(ctx)
	if err != nil {
		log.Printf("Extension handshake failed")
		if closeErr := conn.Close(websocket.StatusPolicyViolation, "handshake required"); closeErr != nil {
			log.Printf("bridge handshake read error: %v; close error: %v", err, closeErr)
		} else {
			log.Printf("bridge handshake read error: %v", err)
		}
		return
	}
	var hello Envelope
	if err := json.Unmarshal(raw, &hello); err != nil {
		if closeErr := conn.Close(websocket.StatusPolicyViolation, "invalid handshake"); closeErr != nil {
			log.Printf("invalid bridge handshake: %v; close error: %v", err, closeErr)
		} else {
			log.Printf("invalid bridge handshake: %v", err)
		}
		return
	}
	if hello.Type != "hello" || subtle.ConstantTimeCompare([]byte(hello.Token), []byte(h.token)) != 1 {
		if err := conn.Close(websocket.StatusPolicyViolation, "invalid token"); err != nil {
			log.Printf("invalid bridge token; close error: %v", err)
		} else {
			log.Printf("invalid bridge token")
		}
		return
	}

	h.mu.Lock()
	if h.conn != nil {
		if err := h.conn.Close(websocket.StatusGoingAway, "replaced by newer connection"); err != nil {
			log.Printf("close previous extension connection: %v", err)
		}
	}
	h.conn = conn
	h.mu.Unlock()
	log.Printf("Extension is connected")
	defer func() {
		log.Printf("Extension is disconnected")
	}()
	if err := h.write(context.Background(), Envelope{Type: "hello_ack"}); err != nil {
		log.Printf("send bridge hello acknowledgement: %v", err)
	}

	for {
		_, raw, err = conn.Read(context.Background())
		if err != nil {
			log.Printf("extension read error: %v", err)
			break
		}
		var message Envelope
		if err := json.Unmarshal(raw, &message); err != nil {
			log.Printf("invalid extension message: %v", err)
			continue
		}
		if message.Type == "heartbeat" {
			if err := h.write(context.Background(), Envelope{Type: "heartbeat_ack"}); err != nil {
				log.Printf("send heartbeat acknowledgement: %v", err)
			}
			continue
		}
		h.mu.Lock()
		waiter := h.pending[message.RequestID]
		h.mu.Unlock()
		if waiter != nil && (message.Type == "result" || message.Type == "error") {
			select {
			case waiter <- message:
			default:
			}
		}
	}
	h.mu.Lock()
	if h.conn == conn {
		h.conn = nil
	}
	h.mu.Unlock()
}

func (h *Hub) Call(ctx context.Context, name string, args any, target any) error {
	raw, err := json.Marshal(args)
	if err != nil {
		return err
	}
	h.mu.Lock()
	if h.conn == nil {
		h.mu.Unlock()
		return ErrUnavailable
	}
	h.nextID++
	id := fmt.Sprintf("cmd-%d", h.nextID)
	response := make(chan Envelope, 1)
	h.pending[id] = response
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.pending, id); h.mu.Unlock() }()

	payload, err := json.Marshal(Command{Name: name, Args: raw})
	if err != nil {
		return fmt.Errorf("encode browser command: %w", err)
	}
	if err := h.write(ctx, Envelope{Type: "command", RequestID: id, Payload: payload}); err != nil {
		return err
	}
	wait := h.timeout
	if deadline, ok := ctx.Deadline(); ok {
		wait = time.Until(deadline)
	}
	select {
	case result := <-response:
		if result.Type == "error" {
			return errors.New(result.Error)
		}
		if err := json.Unmarshal(result.Payload, target); err != nil {
			return fmt.Errorf("decode browser result: %w", err)
		}
		return nil
	case <-time.After(wait):
		return fmt.Errorf("browser extension request timed out after %s", wait)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Hub) write(ctx context.Context, message Envelope) error {
	h.mu.Lock()
	conn := h.conn
	h.mu.Unlock()
	if conn == nil {
		return ErrUnavailable
	}
	raw, err := json.Marshal(message)
	if err != nil {
		return err
	}
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	return conn.Write(ctx, websocket.MessageText, raw)
}
