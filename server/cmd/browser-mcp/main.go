/*
 *  Copyright (c) 2026 Mikhail Knyazhev <markus621@yandex.com>. All rights reserved.
 *  Use of this source code is governed by a MIT license that can be found in the LICENSE file.
 */

package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/arwos/go-brower-mcp/internal/bridge"
	"github.com/arwos/go-brower-mcp/internal/mcp"
	"github.com/coder/websocket"
)

func main() {
	var mode, httpAddr, wsAddr, token, bridgeToken, searchEngine, allowedOrigin string
	var timeout time.Duration
	flag.StringVar(&mode, "mode", env("BROWSER_MCP_MODE", "both"), "transport mode: stdio, http, or both")
	flag.StringVar(&httpAddr, "http-addr", env("BROWSER_MCP_HTTP_ADDR", "127.0.0.1:8787"), "HTTP MCP listener address")
	flag.StringVar(&wsAddr, "ws-addr", env("BROWSER_MCP_WS_ADDR", "127.0.0.1:8788"), "Chrome extension WebSocket listener address")
	flag.StringVar(&token, "http-token", env("BROWSER_MCP_HTTP_TOKEN", ""), "required HTTP bearer token (generated if empty)")
	flag.StringVar(&bridgeToken, "bridge-token", env("BROWSER_MCP_BRIDGE_TOKEN", ""), "Chrome extension WebSocket token (generated if empty)")
	flag.StringVar(&searchEngine, "search-engine", env("BROWSER_MCP_SEARCH_ENGINE", "google"), "server-side search engine: google, bing, or duckduckgo")
	flag.StringVar(&allowedOrigin, "allowed-origin", env("BROWSER_MCP_ALLOWED_ORIGIN", ""), "allowed browser Origin for HTTP MCP CORS (default: local port 8080 origins)")
	flag.DurationVar(&timeout, "request-timeout", 2*time.Minute, "browser command timeout")
	flag.Parse()
	if mode != "stdio" && mode != "http" && mode != "both" {
		log.Fatalf("invalid -mode %q", mode)
	}
	if err := mcp.ValidateSearchEngine(searchEngine); err != nil {
		log.Fatal(err)
	}
	if bridgeToken == "" {
		bridgeToken = secret()
	}
	if token == "" {
		token = secret()
	}
	if !localOnly(wsAddr) || ((mode == "http" || mode == "both") && !localOnly(httpAddr)) {
		log.Fatal("listeners must use loopback addresses; use a reverse proxy for deliberate remote deployment")
	}

	hub := bridge.NewHub(bridgeToken, timeout)
	server := mcp.New(hub, searchEngine)
	log.SetFlags(log.LstdFlags | log.LUTC)
	log.Printf("Chrome extension WebSocket: ws://%s/bridge", wsAddr)
	log.Printf("Chrome extension token (enter in extension popup): %s", bridgeToken)
	if mode == "http" || mode == "both" {
		log.Printf("MCP Streamable HTTP: http://%s/mcp", httpAddr)
		log.Printf("MCP HTTP bearer token: %s", token)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	wsServer := &http.Server{Addr: wsAddr, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bridge" {
			http.NotFound(w, r)
			return
		}
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"chrome-extension://*"}})
		if err != nil {
			log.Printf("accept extension WebSocket: %v", err)
			return
		}
		go hub.Serve(c)
	})}
	go func() {
		if err := wsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("WebSocket server error: %v", err)
			stop()
		}
	}()
	if mode == "http" || mode == "both" {
		hs := &http.Server{Addr: httpAddr, Handler: mcp.HTTPHandler{Server: server, Token: token, AllowedOrigin: allowedOrigin}, ReadHeaderTimeout: 5 * time.Second}
		go func() {
			if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("HTTP server error: %v", err)
				stop()
			}
		}()
		defer func() {
			if err := hs.Shutdown(context.Background()); err != nil {
				log.Printf("shutdown HTTP server: %v", err)
			}
		}()
	}
	if mode == "stdio" || mode == "both" {
		go func() {
			if err := mcp.ServeStdio(ctx, os.Stdin, os.Stdout, server); err != nil {
				log.Printf("stdio ended: %v", err)
			}
			if mode == "stdio" {
				stop()
			}
		}()
	}
	<-ctx.Done()
	if err := wsServer.Shutdown(context.Background()); err != nil {
		log.Printf("shutdown WebSocket server: %v", err)
	}
}
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func secret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func localOnly(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	return host == "127.0.0.1" || host == "::1" || strings.EqualFold(host, "localhost")
}
