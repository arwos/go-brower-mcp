package mcp

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arwos/go-brower-mcp/internal/bridge"
)

func newTestServer() *Server { return New(bridge.NewHub("bridge", time.Second), "google") }
func TestHTTPPreflightAllowsLocalLlamaOrigin(t *testing.T) {
	r := httptest.NewRequest(http.MethodOptions, "/mcp", nil)
	r.Header.Set("Origin", "http://127.0.0.1:8080")
	r.Header.Set("Access-Control-Request-Method", "POST")
	r.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	w := httptest.NewRecorder()
	HTTPHandler{Server: newTestServer(), Token: "secret"}.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "http://127.0.0.1:8080" {
		t.Fatal("missing CORS allow origin")
	}
	allowHeaders := w.Header().Get("Access-Control-Allow-Headers")
	if !strings.Contains(allowHeaders, "Authorization") || !strings.Contains(allowHeaders, "Mcp-Protocol-Version") {
		t.Fatal("missing MCP CORS headers")
	}
}
func TestHTTPRejectsMissingToken(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	HTTPHandler{Server: newTestServer(), Token: "secret"}.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", w.Code)
	}
}
func TestHTTPPing(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	HTTPHandler{Server: newTestServer(), Token: "secret"}.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"result":{}`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
func TestStdioListsTools(t *testing.T) {
	in := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"}\n")
	var out bytes.Buffer
	if err := ServeStdio(t.Context(), in, &out, newTestServer()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "browser_get_page") {
		t.Fatal(out.String())
	}
}
