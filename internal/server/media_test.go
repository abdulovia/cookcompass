package server

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abdulovia/cookcompass/internal/fetch"
	"github.com/abdulovia/cookcompass/internal/storage"
)

// fakeStorage is an in-memory media bucket for tests.
type fakeStorage struct {
	objects map[string][]byte
}

func (f *fakeStorage) Enabled() bool { return true }
func (f *fakeStorage) Put(_ context.Context, key, _ string, r io.Reader, _ int64) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.objects[key] = b
	return nil
}
func (f *fakeStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	b, ok := f.objects[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

// tinyPNG renders a 1×1 PNG fixture.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 31, G: 111, B: 80, A: 255})
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestMediaCacheAndServe: a recipe with a remote image gets it mirrored into
// storage, the recipe switches to /media, the proxy serves the bytes, and
// cards/thumbnails pick it up.
func TestMediaCacheAndServe(t *testing.T) {
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "borscht.png")
	if err := os.WriteFile(pngPath, tinyPNG(t), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t)
	fake := &fakeStorage{objects: map[string][]byte{}}
	h.app.Media.Backend = fake
	h.app.Media.Fetcher = fetch.New("CookCompassBot/test", h.app.Cfg.Scrape.CacheDir, 0, 0, 0)

	r, err := h.app.Recipes.GetBySlug(context.Background(), "borscht")
	if err != nil {
		t.Fatal(err)
	}
	r.ImageURL = "file:" + pngPath
	if err := h.app.Recipes.SetImageURL(context.Background(), r.ID, r.ImageURL); err != nil {
		t.Fatal(err)
	}

	// Cache: bytes land in the bucket under the recipe slug.
	if err := h.app.Media.CacheRecipeImage(context.Background(), r); err != nil {
		t.Fatalf("cache: %v", err)
	}
	if _, ok := fake.objects["recipes/borscht.png"]; !ok {
		t.Fatalf("bucket keys: %v", fake.objects)
	}
	fresh, _ := h.app.Recipes.GetBySlug(context.Background(), "borscht")
	if fresh.ImageURL != "/media/recipes/borscht.png" {
		t.Fatalf("image_url now %q", fresh.ImageURL)
	}
	// Second run is a no-op for already-local images.
	if err := h.app.Media.CacheRecipeImage(context.Background(), fresh); err != nil {
		t.Fatalf("re-cache: %v", err)
	}

	// The proxy serves the stored bytes with long-lived caching.
	resp, body := h.get("/media/recipes/borscht.png")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" ||
		resp.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" || len(body) == 0 {
		t.Fatalf("media: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if resp, _ := h.get("/media/recipes/missing.png"); resp.StatusCode != http.StatusNotFound {
		t.Fatal("missing object should 404")
	}
	if resp, _ := h.get("/media/..%2fetc%2fpasswd"); resp.StatusCode != http.StatusNotFound {
		t.Fatal("traversal attempt should 404")
	}

	// The recipe page shows the hero image; cards show the thumbnail.
	if _, body := h.get("/recipes/borscht"); !strings.Contains(body, `class="hero-img" src="/media/recipes/borscht.png"`) {
		t.Fatal("hero image missing")
	}
	if _, body := h.get("/recipes"); !strings.Contains(body, `<img src="/media/recipes/borscht.png"`) {
		t.Fatal("card thumbnail missing")
	}
	// Cards without a photo fall back to a gradient placeholder.
	if _, body := h.get("/recipes"); !strings.Contains(body, "thumb placeholder") {
		t.Fatal("placeholder missing on imageless cards")
	}
}

// TestMediaDisabled: without storage everything keeps working and no recipe
// is rewritten.
func TestMediaDisabled(t *testing.T) {
	h := newHarness(t)
	r, err := h.app.Recipes.GetBySlug(context.Background(), "borscht")
	if err != nil {
		t.Fatal(err)
	}
	r.ImageURL = "https://example.com/x.png"
	if err := h.app.Recipes.SetImageURL(context.Background(), r.ID, r.ImageURL); err != nil {
		t.Fatal(err)
	}
	if h.app.Media.Enabled() {
		t.Fatal("storage should be disabled by default in tests")
	}
	if err := h.app.Media.CacheRecipeImage(context.Background(), r); err != nil {
		t.Fatalf("no-op expected, got %v", err)
	}
	if n, err := h.app.Media.CacheImages(context.Background()); err != nil || n != 0 {
		t.Fatalf("bulk no-op: %d %v", n, err)
	}
	fresh, _ := h.app.Recipes.GetBySlug(context.Background(), "borscht")
	if fresh.ImageURL != "https://example.com/x.png" {
		t.Fatal("disabled storage must not rewrite URLs")
	}
	// The recipe still shows the remote image directly (CSP allows https:).
	if _, body := h.get("/recipes/borscht"); !strings.Contains(body, `src="https://example.com/x.png"`) {
		t.Fatal("remote image should render")
	}
}
