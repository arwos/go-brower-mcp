/*
 *  Copyright (c) 2026 Mikhail Knyazhev <markus621@yandex.com>. All rights reserved.
 *  Use of this source code is governed by a MIT license that can be found in the LICENSE file.
 */

package mcp

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

func ServeStdio(ctx context.Context, in io.Reader, out io.Writer, server *Server) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	encoder := json.NewEncoder(out)
	for scanner.Scan() {
		var request Request
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			log.Printf("parse stdio JSON-RPC request: %v", err)
			if err := encoder.Encode(Failure(nil, -32700, "parse error", err.Error())); err != nil {
				log.Printf("write stdio parse error response: %v", err)
				return err
			}
			continue
		}
		response := server.Handle(ctx, request)
		if response != nil && len(request.ID) > 0 {
			if err := encoder.Encode(response); err != nil {
				log.Printf("write stdio JSON-RPC response: %v", err)
				return err
			}
		}
	}
	return scanner.Err()
}

type HTTPHandler struct {
	Server        *Server
	Token         string
	AllowedOrigin string
}

func (h HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin != "" && h.originAllowed(origin) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
	if r.URL.Path != "/mcp" {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodOptions {
		if origin == "" || !h.originAllowed(origin) {
			http.Error(w, "CORS origin is not allowed", http.StatusForbidden)
			return
		}
		w.Header().Set("Allow", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, Mcp-Session-Id, Mcp-Protocol-Version, Last-Event-ID")
		w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id, Last-Event-ID")
		w.Header().Set("Access-Control-Max-Age", "600")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, OPTIONS")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	if h.Token != "" && subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")), []byte(h.Token)) != 1 {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	defer func() {
		if err := r.Body.Close(); err != nil {
			log.Printf("close HTTP request body: %v", err)
		}
	}()
	var request Request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&request); err != nil {
		log.Printf("parse HTTP JSON-RPC request: %v", err)
		writeJSON(w, http.StatusBadRequest, Failure(nil, -32700, "parse error", err.Error()))
		return
	}
	response := h.Server.Handle(r.Context(), request)
	if response == nil || len(request.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSON(w, http.StatusOK, response)
}
func (h HTTPHandler) originAllowed(origin string) bool {
	if h.AllowedOrigin == "*" {
		return true
	}
	if h.AllowedOrigin != "" {
		return origin == h.AllowedOrigin
	}
	switch {
	case strings.Contains(origin, "://127."):
		return true
	case strings.Contains(origin, "://192.168."):
		return true
	case strings.Contains(origin, "://10."):
		return true
	case strings.Contains(origin, "://localhost:"):
		return true
	default:
		return false
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write HTTP JSON response: %v", err)
	}
}
