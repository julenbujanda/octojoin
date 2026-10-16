// Copyright 2025 Matthew Gall <me@matthewgall.dev>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

// NtfyNotifier publishes push notifications to an ntfy topic (https://ntfy.sh or self-hosted)
type NtfyNotifier struct {
	URL    string // Full topic URL, e.g. https://ntfy.sh/my-octojoin-topic
	Token  string // Optional access token for protected topics
	client *http.Client
}

type NtfyMessage struct {
	Title    string
	Message  string
	Tags     []string // Emoji shortcodes / tags, e.g. "zap"
	Priority string   // min, low, default, high, urgent
	Click    string   // Optional URL opened when the notification is tapped
}

func NewNtfyNotifier(url, token string) *NtfyNotifier {
	return &NtfyNotifier{
		URL:   url,
		Token: token,
		client: &http.Client{
			Timeout: HTTPClientTimeout,
		},
	}
}

// SendTest publishes a test notification to check ntfy is set up
func (n *NtfyNotifier) SendTest() error {
	return n.Send(NtfyMessage{
		Title:   "OctoJoin test notification",
		Message: "ntfy notifications from OctoJoin are working.",
		Tags:    []string{"white_check_mark"},
	})
}

func (n *NtfyNotifier) Send(msg NtfyMessage) error {
	req, err := http.NewRequest("POST", n.URL, strings.NewReader(msg.Message))
	if err != nil {
		return fmt.Errorf("failed to create ntfy request: %w", err)
	}

	req.Header.Set("User-Agent", GetUserAgent())
	if msg.Title != "" {
		req.Header.Set("Title", msg.Title)
	}
	if len(msg.Tags) > 0 {
		req.Header.Set("Tags", strings.Join(msg.Tags, ","))
	}
	if msg.Priority != "" {
		req.Header.Set("Priority", msg.Priority)
	}
	if msg.Click != "" {
		req.Header.Set("Click", msg.Click)
	}
	if n.Token != "" {
		req.Header.Set("Authorization", "Bearer "+n.Token)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send ntfy notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("ntfy returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}
