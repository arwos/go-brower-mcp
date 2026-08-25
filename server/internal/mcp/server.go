/*
 *  Copyright (c) 2026 Mikhail Knyazhev <markus621@yandex.com>. All rights reserved.
 *  Use of this source code is governed by a MIT license that can be found in the LICENSE file.
 */

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/arwos/go-brower-mcp/internal/bridge"
)

type Server struct {
	hub          *bridge.Hub
	searchEngine string
}

func New(hub *bridge.Hub, searchEngine string) *Server {
	return &Server{hub: hub, searchEngine: searchEngine}
}

func (s *Server) Handle(ctx context.Context, request Request) *Response {
	if request.JSONRPC != JSONRPCVersion {
		r := Failure(request.ID, -32600, "invalid JSON-RPC version", nil)
		return &r
	}
	var result any
	switch request.Method {
	case "initialize":
		result = map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "local-browser-mcp", "version": "0.1.0"}}
	case "notifications/initialized":
		return nil
	case "ping":
		result = map[string]any{}
	case "tools/list":
		result = map[string]any{"tools": Tools()}
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			log.Printf("decode tools/call parameters: %v", err)
			r := Failure(request.ID, -32602, "invalid tool parameters", err.Error())
			return &r
		}
		if params.Name == "web_search" {
			var searchArgs struct {
				Query   string   `json:"query"`
				Queries []string `json:"queries"`
			}
			if err := json.Unmarshal(params.Arguments, &searchArgs); err != nil {
				log.Printf("decode web_search arguments: %v", err)
				r := Failure(request.ID, -32602, "invalid web_search arguments", err.Error())
				return &r
			}
			if len(searchArgs.Queries) > 0 && strings.TrimSpace(searchArgs.Query) == "" {
				searchArgs.Query = searchArgs.Queries[0]
				params.Arguments, _ = json.Marshal(searchArgs)
			}
		}
		out := s.call(ctx, params.Name, params.Arguments)
		result = out
	default:
		r := Failure(request.ID, -32601, "method not found", request.Method)
		return &r
	}
	r := Success(request.ID, result)
	return &r
}

func Tools() []Tool {
	return []Tool{
		{Name: "browser_list_tabs", Description: "List browser tabs available through the local Chrome extension.", InputSchema: schema(map[string]any{})},
		{Name: "browser_open_tab", Description: "Open a URL or a search query in a new Chrome tab.", InputSchema: schema(map[string]any{"url": map[string]any{"type": "string", "description": "HTTP(S) URL or search text"}, "active": map[string]any{"type": "boolean", "default": true}}, "url")},
		{Name: "browser_search", Description: "Legacy alias for web_search using the server-configured search engine.", InputSchema: schema(map[string]any{"query": map[string]any{"type": "string"}, "active": map[string]any{"type": "boolean", "default": true}}, "query")},
		{Name: "web_search", Description: "Search using the configured engine. Provide query or queries; multiple queries are executed sequentially in the managed tab and their Markdown results are combined.", InputSchema: schema(map[string]any{"query": map[string]any{"type": "string", "description": "Single search query"}, "queries": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "description": "Multiple search queries"}, "active": map[string]any{"type": "boolean", "default": true}})},
		{Name: "web_get_link", Description: "Open an HTTP(S) link in the managed browser tab and return the final URL plus Markdown or base64 image content.", InputSchema: schema(map[string]any{"url": map[string]any{"type": "string", "format": "uri"}}, "url")},
		{Name: "browser_get_page", Description: "Extract visible page content as Markdown and optional base64 images.", InputSchema: schema(map[string]any{"tabId": map[string]any{"type": "integer"}, "url": map[string]any{"type": "string"}, "includeLinks": map[string]any{"type": "boolean", "default": true}, "includeImages": map[string]any{"type": "boolean", "default": false}, "maxTextChars": map[string]any{"type": "integer", "minimum": 1}, "maxLinks": map[string]any{"type": "integer", "minimum": 0}, "maxImages": map[string]any{"type": "integer", "minimum": 0}})},
		{Name: "browser_get_links", Description: "Extract page links as a Markdown list.", InputSchema: schema(map[string]any{"tabId": map[string]any{"type": "integer"}, "url": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer", "minimum": 1}, "domain": map[string]any{"type": "string"}})},
	}
}
func schema(properties map[string]any, required ...string) map[string]any {
	// JSON Schema requires `required` to be an array when present. A nil
	// variadic slice would be encoded as JSON null, which some MCP clients
	// (including DeepSeek Harness) reject during tools/list validation.
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func (s *Server) openTab(ctx context.Context, args json.RawMessage) ToolResult {
	var tab bridge.Tab
	if err := s.hub.Call(ctx, "open_tab", json.RawMessage(args), &tab); err != nil {
		return errorResult(err.Error())
	}
	return textResult(fmt.Sprintf("# Opened tab\n\n- ID: `%d`\n- [%s](%s)\n", tab.ID, esc(tab.Title), safeURL(tab.URL)))
}

func (s *Server) searchPage(ctx context.Context, query string, active bool) (bridge.PageResult, error) {
	searchURL, err := BuildSearchURL(s.searchEngine, query)
	if err != nil {
		return bridge.PageResult{}, err
	}
	payload, err := json.Marshal(map[string]any{"url": searchURL, "active": active})
	if err != nil {
		return bridge.PageResult{}, fmt.Errorf("encode search arguments: %w", err)
	}
	var tab bridge.Tab
	if err := s.hub.Call(ctx, "open_tab", json.RawMessage(payload), &tab); err != nil {
		return bridge.PageResult{}, err
	}
	pageArgs, err := json.Marshal(map[string]any{"tabId": tab.ID, "includeLinks": true, "includeImages": false})
	if err != nil {
		return bridge.PageResult{}, fmt.Errorf("encode page arguments: %w", err)
	}
	var page bridge.PageResult
	if err := s.hub.Call(ctx, "get_page", json.RawMessage(pageArgs), &page); err != nil {
		return bridge.PageResult{}, err
	}
	return page, nil
}

func (s *Server) call(ctx context.Context, name string, args json.RawMessage) ToolResult {
	if !s.hub.Connected() {
		return errorResult("Chrome extension is not connected. Open its popup and connect it to this local server.")
	}
	switch name {
	case "browser_list_tabs":
		var tabs []bridge.Tab
		if err := s.hub.Call(ctx, "list_tabs", map[string]any{}, &tabs); err != nil {
			return errorResult(err.Error())
		}
		var b strings.Builder
		b.WriteString("# Browser tabs\n\n")
		for _, t := range tabs {
			active := ""
			if t.Active {
				active = " **(active)**"
			}
			fmt.Fprintf(&b, "- `%d`%s [%s](%s)\n", t.ID, active, esc(t.Title), safeURL(t.URL))
		}
		return textResult(b.String())
	case "browser_open_tab":
		return s.openTab(ctx, args)
	case "web_get_link":
		var result bridge.LinkResult
		if err := s.hub.Call(ctx, "open_link", args, &result); err != nil {
			return errorResult(err.Error())
		}
		return linkResult(result)
	case "browser_search", "web_search":
		var input struct {
			Query   string   `json:"query"`
			Queries []string `json:"queries"`
			Active  *bool    `json:"active"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			return errorResult("invalid search arguments: " + err.Error())
		}
		queries := input.Queries
		if len(queries) == 0 && strings.TrimSpace(input.Query) != "" {
			queries = []string{input.Query}
		}
		if len(queries) == 0 {
			return errorResult("provide either query or queries")
		}
		active := true
		if input.Active != nil {
			active = *input.Active
		}
		var combined strings.Builder
		for index, query := range queries {
			page, err := s.searchPage(ctx, query, active)
			if err != nil {
				return errorResult(fmt.Sprintf("search query %d failed: %v", index+1, err))
			}
			if len(queries) > 1 {
				fmt.Fprintf(&combined, "\n\n## Search %d: %s\n\n", index+1, esc(strings.TrimSpace(query)))
			}
			combined.WriteString(pageResult(page).Content[0].(TextContent).Text)
		}
		return textResult(combined.String())

	case "browser_get_page":
		var page bridge.PageResult
		if err := s.hub.Call(ctx, "get_page", json.RawMessage(args), &page); err != nil {
			return errorResult(err.Error())
		}
		return pageResult(page)
	case "browser_get_links":
		var page bridge.PageResult
		if err := s.hub.Call(ctx, "get_links", json.RawMessage(args), &page); err != nil {
			return errorResult(err.Error())
		}
		var b strings.Builder
		fmt.Fprintf(&b, "# Links — %s\n\nSource: <%s>\n\n", esc(page.Title), safeURL(page.URL))
		for _, l := range page.Links {
			fmt.Fprintf(&b, "- [%s](%s)\n", esc(l.Text), safeURL(l.URL))
		}
		return textResult(b.String())
	default:
		return errorResult("unknown tool: " + name)
	}
}
func linkResult(result bridge.LinkResult) ToolResult {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\nFinal URL: <%s>\n\n", esc(result.Title), safeURL(result.URL))
	if result.ContentType == "image" {
		return ToolResult{Content: []any{TextContent{Type: "text", Text: b.String()}, ImageContent{Type: "image", Data: result.Data, MIMEType: result.MIMEType}}}
	}
	b.WriteString(result.Markdown)
	return textResult(b.String())
}

func pageResult(page bridge.PageResult) ToolResult {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\nSource: <%s>\n\n%s\n", esc(page.Title), safeURL(page.URL), page.Text)
	if len(page.Links) > 0 {
		b.WriteString("\n## Links\n\n")
		for _, l := range page.Links {
			fmt.Fprintf(&b, "- [%s](%s)\n", esc(l.Text), safeURL(l.URL))
		}
	}
	if page.Truncated {
		b.WriteString("\n> Content was truncated by configured limits.\n")
	}
	for _, w := range page.Warnings {
		fmt.Fprintf(&b, "\n> Warning: %s\n", esc(w))
	}
	content := []any{TextContent{Type: "text", Text: b.String()}}
	for _, i := range page.Images {
		content = append(content, ImageContent{Type: "image", Data: i.Data, MIMEType: i.MIMEType})
	}
	return ToolResult{Content: content}
}
func textResult(text string) ToolResult {
	return ToolResult{Content: []any{TextContent{Type: "text", Text: text}}}
}
func errorResult(message string) ToolResult {
	return ToolResult{Content: []any{TextContent{Type: "text", Text: "Error: " + message}}, IsError: true}
}
func safeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return raw
}
func esc(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(s), "[", "\\["), "]", "\\]")
}
