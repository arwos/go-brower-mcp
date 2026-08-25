package mcp

import (
	"strings"
	"testing"
)

func TestBuildSearchURL(t *testing.T) {
	for _, tc := range []struct{ engine, want string }{
		{"google", "https://www.google.com/search?q=hello+world"},
		{"bing", "https://www.bing.com/search?q=hello+world"},
		{"duckduckgo", "https://duckduckgo.com/?q=hello+world"},
	} {
		got, err := BuildSearchURL(tc.engine, "hello world")
		if err != nil || got != tc.want {
			t.Fatalf("%s: got %q, err %v", tc.engine, got, err)
		}
	}
}

func TestBuildSearchURLRejectsInvalidInput(t *testing.T) {
	if _, err := BuildSearchURL("unknown", "query"); err == nil {
		t.Fatal("expected unknown engine error")
	}
	if _, err := BuildSearchURL("google", " "); err == nil {
		t.Fatal("expected empty query error")
	}
	got, err := BuildSearchURL("GOOGLE", "a&b?c")
	if err != nil || !strings.Contains(got, "a%26b%3Fc") {
		t.Fatalf("got %q, err %v", got, err)
	}
}
