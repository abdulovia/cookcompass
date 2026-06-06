// Package storage keeps recipe images in an S3-compatible bucket (MinIO by
// default) behind a tiny interface. When no bucket is configured the app
// runs without one: nothing crashes, images simply stay wherever their URLs
// point.
package storage

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
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

// S3 talks to one bucket of an S3-compatible endpoint.
type S3 struct {
	client *minio.Client
	bucket string
	log    *slog.Logger
}

// NewS3 connects to the configured endpoint and makes sure the bucket
// exists. It returns Disabled when the endpoint is unreachable — a fresh
// laptop `make run` must keep working without a MinIO container.
func NewS3(cfg config.S3, log *slog.Logger) Backend {
	if cfg.Endpoint == "" {
		return Disabled{}
	}
	cl, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.Secure,
	})
	if err != nil {
		log.Warn("storage disabled: bad endpoint", "err", err)
		return Disabled{}
	}
	// Bound the startup probe so a black-holed endpoint cannot hang boot.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	exists, err := cl.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		log.Warn("storage disabled: endpoint unreachable (images stay on their original URLs)", "endpoint", cfg.Endpoint, "err", err)
		return Disabled{}
	}
	if !exists {
		if err := cl.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			log.Warn("storage disabled: cannot create bucket", "bucket", cfg.Bucket, "err", err)
			return Disabled{}
		}
		log.Info("storage bucket created", "bucket", cfg.Bucket)
	}
	log.Info("image storage ready", "endpoint", cfg.Endpoint, "bucket", cfg.Bucket)
	return &S3{client: cl, bucket: cfg.Bucket, log: log}
}

// Enabled implements Backend.
func (s *S3) Enabled() bool { return true }

// Put implements Backend.
func (s *S3) Put(ctx context.Context, key, contentType string, r io.Reader, size int64) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil && s.log != nil {
		s.log.Warn("storage put", "key", key, "err", err)
	}
	return err
}

// Get implements Backend.
func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
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
