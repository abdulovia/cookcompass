package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"

	"github.com/abdulovia/cookcompass/internal/fetch"
	"github.com/abdulovia/cookcompass/internal/recipes"
	"github.com/abdulovia/cookcompass/internal/storage"
)

// Media mirrors recipe images into the storage bucket and rewrites their
// URLs to the local /media proxy, so pages never hotlink other sites.
type Media struct {
	Backend storage.Backend
	Fetcher *fetch.Client
	Recipes *recipes.Store
}

// Enabled reports whether image storage is configured and reachable.
func (m *Media) Enabled() bool { return m != nil && m.Backend != nil && m.Backend.Enabled() }

// Open returns the object stream for a media key.
func (m *Media) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if m == nil || m.Backend == nil {
		return nil, storage.ErrNotFound
	}
	return m.Backend.Get(ctx, key)
}

// CacheRecipeImage downloads r's image (when it has a remote one) into the
// bucket and points r.ImageURL at /media/<key>. Recipes already served from
// /media are left alone; without storage the call is a no-op. The image
// rewrite is bookkeeping, not content: no revision row is recorded.
func (m *Media) CacheRecipeImage(ctx context.Context, r *recipes.Recipe) error {
	if !m.Enabled() || r.ImageURL == "" || strings.HasPrefix(r.ImageURL, "/media/") {
		return nil
	}
	body, err := m.Fetcher.Get(ctx, r.ImageURL)
	if err != nil {
		return fmt.Errorf("fetch image: %w", err)
	}
	ext := extFrom(r.ImageURL)
	ct := storage.SniffContentType(body[:min(len(body), 512)])
	if ct == "" {
		return errors.New("not an image")
	}
	if ext == "" {
		ext = storage.ExtFromContentType(ct)
	}
	key := "recipes/" + r.Slug + ext
	if err := m.Backend.Put(ctx, key, ct, bytes.NewReader(body), int64(len(body))); err != nil {
		return fmt.Errorf("store image: %w", err)
	}
	return m.Recipes.SetImageURL(ctx, r.ID, "/media/"+key)
}

func extFrom(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	ext := strings.ToLower(path.Ext(u.Path))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".avif":
		return ext
	default:
		return ""
	}
}

// CacheImages caches the images of every non-deleted recipe that still
// points at a remote URL. It returns how many images were stored; failures
// on individual recipes are skipped, not fatal.
func (m *Media) CacheImages(ctx context.Context) (int, error) {
	if !m.Enabled() {
		return 0, nil
	}
	list, err := m.Recipes.List(ctx, recipes.Filter{})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range list {
		if !strings.HasPrefix(r.ImageURL, "http") {
			continue
		}
		if err := m.CacheRecipeImage(ctx, r); err == nil {
			n++
		}
	}
	return n, nil
}
