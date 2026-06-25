// Package translate talks to a self-hosted LibreTranslate instance to draft
// recipe translations. Machine output is always marked for review; it never
// publishes itself.
package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

type response []struct {
	TranslatedText string `json:"translatedText"`
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
		resp, err := c.Client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("translate: %s", resp.Status)
		}
		var parsed response
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			return nil, err
		}
		if len(parsed) != end-start {
			return nil, fmt.Errorf("translate: got %d results for %d texts", len(parsed), end-start)
		}
		for _, p := range parsed {
			out = append(out, p.TranslatedText)
		}
	}
	return out, nil
}
