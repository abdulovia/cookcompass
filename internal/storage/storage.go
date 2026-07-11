// Package storage keeps recipe images in an S3-compatible bucket (MinIO by
// default) behind a tiny interface. When no bucket is configured the app
// runs without one: nothing crashes, images simply stay wherever their URLs
// point.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"sync/atomic"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/abdulovia/cookcompass/internal/config"
)

// ErrNotFound is returned by Get for missing objects.
var ErrNotFound = errors.New("object not found")

// Backend stores and serves binary objects by key.
type Backend interface {
	Enabled() bool
	Put(ctx context.Context, key, contentType string, r io.Reader, size int64) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

// Disabled is the no-storage fallback: every call is a graceful no-op.
type Disabled struct{}

// Enabled implements Backend.
func (Disabled) Enabled() bool { return false }

// Put implements Backend.
func (Disabled) Put(context.Context, string, string, io.Reader, int64) error { return nil }

// Get implements Backend.
func (Disabled) Get(context.Context, string) (io.ReadCloser, error) { return nil, ErrNotFound }

// NormalizeEndpoint turns "http://minio:9000/" style URLs into the bare
// host:port minio-go expects; an https:// scheme implies TLS.
func NormalizeEndpoint(raw string, secure bool) (string, bool) {
	ep := strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(ep, "https://"):
		ep, secure = strings.TrimPrefix(ep, "https://"), true
	case strings.HasPrefix(ep, "http://"):
		ep, secure = strings.TrimPrefix(ep, "http://"), false
	}
	return strings.TrimRight(ep, "/"), secure
}

// S3 talks to one bucket of an S3-compatible endpoint.
type S3 struct {
	client *minio.Client
	bucket string
	log    *slog.Logger
	ready  atomic.Bool
}

// ErrUnavailable is returned while the endpoint has not answered yet.
var ErrUnavailable = errors.New("storage unavailable")

// Probe pacing for endpoints that are not up yet (compose starts MinIO and
// the app together, and MinIO can take a while on a cold volume).
var (
	retryEvery    = 10 * time.Second
	retryAttempts = 60 // ~10 minutes, then give up until restart
)

// NewS3 connects to the configured endpoint and makes sure the bucket
// exists. A malformed endpoint returns Disabled; an unreachable one returns
// a backend that reports Enabled() == false and keeps probing in the
// background, so a fresh laptop `make run` keeps working without MinIO and
// a slow-starting container is picked up once it answers.
func NewS3(cfg config.S3, log *slog.Logger) Backend {
	if cfg.Endpoint == "" {
		return Disabled{}
	}
	endpoint, secure := NormalizeEndpoint(cfg.Endpoint, cfg.Secure)
	cl, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: secure,
	})
	if err != nil {
		log.Warn("storage disabled: bad endpoint", "err", err)
		return Disabled{}
	}
	s := &S3{client: cl, bucket: cfg.Bucket, log: log}
	if err := s.probe(); err != nil {
		log.Warn("storage not reachable yet; retrying in the background (images stay on their original URLs)",
			"endpoint", endpoint, "err", err)
		go s.retry()
		return s
	}
	log.Info("image storage ready", "endpoint", endpoint, "bucket", cfg.Bucket)
	return s
}

// probe checks the bucket (creating it when missing) and marks s ready.
func (s *S3) probe() error {
	// Bound each probe so a black-holed endpoint cannot hang boot.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return err
	}
	if !exists {
		if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("create bucket %s: %w", s.bucket, err)
		}
		s.log.Info("storage bucket created", "bucket", s.bucket)
	}
	s.ready.Store(true)
	return nil
}

func (s *S3) retry() {
	for i := 0; i < retryAttempts; i++ {
		time.Sleep(retryEvery)
		if err := s.probe(); err == nil {
			s.log.Info("image storage ready", "bucket", s.bucket, "attempt", i+2)
			return
		}
	}
	s.log.Warn("storage still unreachable; giving up until restart", "bucket", s.bucket)
}

// Enabled implements Backend.
func (s *S3) Enabled() bool { return s.ready.Load() }

// Put implements Backend.
func (s *S3) Put(ctx context.Context, key, contentType string, r io.Reader, size int64) error {
	if !s.Enabled() {
		return ErrUnavailable
	}
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil && s.log != nil {
		s.log.Warn("storage put", "key", key, "err", err)
	}
	return err
}

// Get implements Backend.
func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if !s.Enabled() {
		return nil, ErrUnavailable
	}
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		if isNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	// minio defers the request; touch the object so a miss surfaces here.
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		if isNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return obj, nil
}

func isNotFound(err error) bool {
	var resp minio.ErrorResponse
	return errors.As(err, &resp) && resp.Code == "NoSuchKey"
}

// ContentTypeByExt maps media keys to their Content-Type.
func ContentTypeByExt(key string) string {
	switch strings.ToLower(path.Ext(key)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".avif":
		return "image/avif"
	default:
		return "application/octet-stream"
	}
}

// ExtFromContentType is the inverse mapping used when a fetched image has no
// usable file extension.
func ExtFromContentType(ct string) string {
	switch {
	case strings.Contains(ct, "jpeg"), strings.Contains(ct, "jpg"):
		return ".jpg"
	case strings.Contains(ct, "png"):
		return ".png"
	case strings.Contains(ct, "webp"):
		return ".webp"
	case strings.Contains(ct, "gif"):
		return ".gif"
	case strings.Contains(ct, "svg"):
		return ".svg"
	case strings.Contains(ct, "avif"):
		return ".avif"
	default:
		return ""
	}
}

// SniffContentType detects an image type from the first bytes. It returns ""
// for anything that is not a known image format.
func SniffContentType(head []byte) string {
	ct := http.DetectContentType(head)
	if !strings.HasPrefix(ct, "image/") {
		return ""
	}
	return ct
}
