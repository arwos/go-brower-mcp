/*
 *  Copyright (c) 2026 Mikhail Knyazhev <markus621@yandex.com>. All rights reserved.
 *  Use of this source code is governed by a MIT license that can be found in the LICENSE file.
 */

package bridge

import "encoding/json"

type Envelope struct {
	Type      string          `json:"type"`
	RequestID string          `json:"requestId,omitempty"`
	Token     string          `json:"token,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Error     string          `json:"error,omitempty"`
}

type Command struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type Image struct {
	Data     string `json:"data"`
	MIMEType string `json:"mimeType"`
	Alt      string `json:"alt,omitempty"`
}

type PageResult struct {
	Title     string   `json:"title"`
	URL       string   `json:"url"`
	Text      string   `json:"text"`
	Links     []Link   `json:"links,omitempty"`
	Images    []Image  `json:"images,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
	Warnings  []string `json:"warnings,omitempty"`
}

type LinkResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	ContentType string `json:"contentType"`
	Markdown    string `json:"markdown,omitempty"`
	Data        string `json:"data,omitempty"`
	MIMEType    string `json:"mimeType,omitempty"`
}

type Link struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

type Tab struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Active bool   `json:"active"`
}
