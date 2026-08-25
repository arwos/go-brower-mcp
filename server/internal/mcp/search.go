/*
 *  Copyright (c) 2026 Mikhail Knyazhev <markus621@yandex.com>. All rights reserved.
 *  Use of this source code is governed by a MIT license that can be found in the LICENSE file.
 */

package mcp

import (
	"fmt"
	"net/url"
	"strings"
)

var searchTemplates = map[string]string{
	"google":     "https://www.google.com/search?q=%s",
	"bing":       "https://www.bing.com/search?q=%s",
	"duckduckgo": "https://duckduckgo.com/?q=%s",
}

func ValidateSearchEngine(engine string) error {
	if _, ok := searchTemplates[strings.ToLower(strings.TrimSpace(engine))]; !ok {
		return fmt.Errorf("unsupported search engine %q; choose one of: google, bing, duckduckgo", engine)
	}
	return nil
}

func BuildSearchURL(engine, query string) (string, error) {
	engine = strings.ToLower(strings.TrimSpace(engine))
	if err := ValidateSearchEngine(engine); err != nil {
		return "", err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("search query must not be empty")
	}
	return fmt.Sprintf(searchTemplates[engine], url.QueryEscape(query)), nil
}
