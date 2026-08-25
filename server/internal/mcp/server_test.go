package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/arwos/go-brower-mcp/internal/bridge"
)

func TestToolsExposeRequiredOperations(t *testing.T) {
	want := map[string]bool{"browser_list_tabs": false, "browser_open_tab": false, "browser_search": false, "browser_get_page": false, "browser_get_links": false, "web_search": false, "web_get_link": false}
	for _, tool := range Tools() {
		if tool.Name == "web_search" {
			properties := tool.InputSchema["properties"].(map[string]any)
			if _, ok := properties["queries"]; !ok {
				t.Fatal("web_search schema must accept queries")
			}
		}
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
		if tool.InputSchema["type"] != "object" {
			t.Fatalf("%s lacks object schema", tool.Name)
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing %s", name)
		}
	}
}
func TestToolSchemasAlwaysEncodeRequiredAsArray(t *testing.T) {
	for _, tool := range Tools() {
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal %s schema: %v", tool.Name, err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		required, ok := decoded["required"].([]any)
		if !ok || required == nil {
			t.Fatalf("%s has invalid required field: %s", tool.Name, raw)
		}
	}
}

func TestUnconnectedToolResultIsError(t *testing.T) {
	s := New(bridge.NewHub("token", time.Second), "google")
	response := s.Handle(context.Background(), Request{JSONRPC: JSONRPCVersion, ID: json.RawMessage(`1`), Method: "tools/call", Params: json.RawMessage(`{"name":"browser_list_tabs","arguments":{}}`)})
	result, ok := response.Result.(ToolResult)
	if !ok || !result.IsError {
		t.Fatalf("expected error tool result, got %#v", response.Result)
	}
}
func TestPageResultFormatsMarkdownAndImage(t *testing.T) {
	result := pageResult(bridge.PageResult{Title: "[Title]", URL: "https://example.test", Text: "Hello", Links: []bridge.Link{{Text: "go", URL: "https://go.dev"}}, Images: []bridge.Image{{Data: "YWJj", MIMEType: "image/png"}}})
	if len(result.Content) != 2 {
		t.Fatalf("got %d content entries", len(result.Content))
	}
	text := result.Content[0].(TextContent).Text
	if !strings.Contains(text, "[go](https://go.dev)") {
		t.Fatal(text)
	}
	image := result.Content[1].(ImageContent)
	if image.Data != "YWJj" || image.MIMEType != "image/png" {
		t.Fatal(image)
	}
}
func TestUnsafeURLsAreNotRendered(t *testing.T) {
	if got := safeURL("javascript:alert(1)"); got != "" {
		t.Fatal(got)
	}
}
