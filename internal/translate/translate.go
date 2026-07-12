// Package translate talks to a self-hosted LibreTranslate instance to draft
// recipe translations. Machine output is always marked for review; it never
// publishes itself.
package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client drafts translations through one LibreTranslate endpoint.
type Client struct {
	URL    string // e.g. http://localhost:15000
	Client *http.Client
}

// New returns a Client for url, or nil when no endpoint is configured.
func New(url string) *Client {
	if strings.TrimSpace(url) == "" {
		return nil
	}
	return &Client{URL: strings.TrimRight(strings.TrimSpace(url), "/"),
		Client: &http.Client{Timeout: 60 * time.Second}}
}

// request is the LibreTranslate /translate payload.
type request struct {
	Q      []string `json:"q"`
	Source string   `json:"source"`
	Target string   `json:"target"`
	Format string   `json:"format"`
}

// response is LibreTranslate's reply: {"translatedText": "…"} for one q,
// {"translatedText": ["…", …]} for a batch.
type response struct {
	TranslatedText json.RawMessage `json:"translatedText"`
	Error          string          `json:"error"`
}

// decode extracts n translations from a /translate body. A bare JSON array
// of {"translatedText": "…"} objects is accepted too (some proxies).
func decode(body []byte, n int) ([]string, error) {
	var texts []string
	var obj response
	if err := json.Unmarshal(body, &obj); err == nil {
		if obj.Error != "" {
			return nil, fmt.Errorf("translate: %s", obj.Error)
		}
		var list []string
		var one string
		switch {
		case json.Unmarshal(obj.TranslatedText, &list) == nil:
			texts = list
		case json.Unmarshal(obj.TranslatedText, &one) == nil:
			texts = []string{one}
		default:
			return nil, fmt.Errorf("translate: unexpected translatedText %s", truncate(obj.TranslatedText))
		}
	} else {
		var pairs []struct {
			TranslatedText string `json:"translatedText"`
		}
		if err := json.Unmarshal(body, &pairs); err != nil {
			return nil, fmt.Errorf("translate: unexpected response %s", truncate(body))
		}
		for _, p := range pairs {
			texts = append(texts, p.TranslatedText)
		}
	}
	if len(texts) != n {
		return nil, fmt.Errorf("translate: got %d results for %d texts", len(texts), n)
	}
	return texts, nil
}

func truncate(b []byte) string {
	if len(b) > 120 {
		return string(b[:120]) + "…"
	}
	return string(b)
}

// Translate renders texts from source to target, preserving order. An empty
// input yields an empty output.
func (c *Client) Translate(ctx context.Context, source, target string, texts ...string) ([]string, error) {
	if c == nil {
		return nil, fmt.Errorf("translator is not configured")
	}
	var out []string
	// Batch to keep payloads modest; the endpoint is a local container.
	for start := 0; start < len(texts); start += 20 {
		end := min(start+20, len(texts))
		body, err := json.Marshal(request{Q: texts[start:end], Source: source, Target: target, Format: "text"})
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+"/translate", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		got, err := c.post(req, end-start)
		if err != nil {
			return nil, err
		}
		out = append(out, got...)
	}
	return out, nil
}

// post runs one /translate request and decodes n results.
func (c *Client) post(req *http.Request, n int) ([]string, error) {
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		var e response
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			return nil, fmt.Errorf("translate: %s: %s", resp.Status, e.Error)
		}
		return nil, fmt.Errorf("translate: %s", resp.Status)
	}
	return decode(body, n)
}
