package storage

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abdulovia/cookcompass/internal/config"
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

func TestNormalizeEndpoint(t *testing.T) {
	cases := []struct {
		in         string
		secure     bool
		want       string
		wantSecure bool
	}{
		{"minio:9000", false, "minio:9000", false},
		{"minio:9000", true, "minio:9000", true},
		{"http://minio:9000", true, "minio:9000", false},
		{"https://s3.example.com/", false, "s3.example.com", true},
		{" http://localhost:9000/ ", false, "localhost:9000", false},
	}
	for _, c := range cases {
		got, sec := NormalizeEndpoint(c.in, c.secure)
		if got != c.want || sec != c.wantSecure {
			t.Errorf("NormalizeEndpoint(%q, %v) = %q, %v; want %q, %v", c.in, c.secure, got, sec, c.want, c.wantSecure)
		}
	}
}

// fakeS3 answers just enough of the S3 API for BucketExists: the bucket
// location lookup and a HEAD on the bucket. Until up is set it fails.
func fakeS3(t *testing.T, up *atomic.Bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up.Load() {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		if _, ok := r.URL.Query()["location"]; ok {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNewS3RetriesUntilReachable(t *testing.T) {
	oldEvery, oldN := retryEvery, retryAttempts
	retryEvery, retryAttempts = 20*time.Millisecond, 200
	t.Cleanup(func() { retryEvery, retryAttempts = oldEvery, oldN })

	var up atomic.Bool
	srv := fakeS3(t, &up)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	b := NewS3(config.S3{Endpoint: srv.URL, AccessKey: "ak", SecretKey: "sk", Bucket: "cookcompass"}, log)
	if b.Enabled() {
		t.Fatal("backend must not be enabled while the endpoint is down")
	}
	if err := b.Put(context.Background(), "k", "image/png", strings.NewReader("x"), 1); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Put while down: %v", err)
	}
	up.Store(true)
	deadline := time.Now().Add(3 * time.Second)
	for !b.Enabled() {
		if time.Now().After(deadline) {
			t.Fatal("backend never became ready after the endpoint came up")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestNewS3BadEndpoint(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, ok := NewS3(config.S3{Endpoint: "http://bad host:9000", Bucket: "b"}, log).(Disabled); !ok {
		t.Fatal("malformed endpoint should disable storage")
	}
}
