package storage

import (
	"context"
	"testing"
)

func TestContentTypes(t *testing.T) {
	for key, want := range map[string]string{
		"recipes/x.jpg":  "image/jpeg",
		"recipes/x.jpeg": "image/jpeg",
		"recipes/x.PNG":  "image/png",
		"recipes/x.webp": "image/webp",
		"recipes/x.svg":  "image/svg+xml",
		"recipes/x.bin":  "application/octet-stream",
	} {
		if got := ContentTypeByExt(key); got != want {
			t.Errorf("ContentTypeByExt(%q) = %q, want %q", key, got, want)
		}
	}
	for ct, want := range map[string]string{
		"image/jpeg":      ".jpg",
		"image/png":       ".png",
		"image/webp":      ".webp",
		"text/html":       "",
		"application/zip": "",
	} {
		if got := ExtFromContentType(ct); got != want {
			t.Errorf("ExtFromContentType(%q) = %q, want %q", ct, got, want)
		}
	}
}

func TestSniffContentType(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0}
	if got := SniffContentType(png); got != "image/png" {
		t.Fatalf("png sniffed as %q", got)
	}
	if got := SniffContentType([]byte("<html>not an image</html>")); got != "" {
		t.Fatalf("html sniffed as %q, want empty", got)
	}
}

func TestDisabledBackend(t *testing.T) {
	var b Backend = Disabled{}
	if b.Enabled() {
		t.Fatal("disabled reports enabled")
	}
	if err := b.Put(context.TODO(), "k", "image/png", nil, 0); err != nil {
		t.Fatal("put should no-op")
	}
	if _, err := b.Get(context.TODO(), "k"); err != ErrNotFound {
		t.Fatalf("get should be ErrNotFound, got %v", err)
	}
}
