// Package storage is an S3-compatible object store client.
//
// One interface, three backends — dev and production share the exact same
// code path:
//
//   - MinIO (local dev via docker-compose)
//   - Backblaze B2 (cheapest prod: $6/TB + free egress via Cloudflare)
//   - Cloudflare R2 (alternative: $15/TB, always free egress)
//
// The S3 API is the common denominator. Swap backends by changing env
// vars — no code changes.
//
// Layout in the bucket:
//
//	originals/<episode_id>/<uuid>.<ext>     raw upload from admin
//	pages/<episode_id>/<page>-<variant>.jpg transformed, public, immutable
//
// Filenames are immutable once written (no overwrites), so we can set
// Cache-Control: public, max-age=31536000, immutable and let the CDN
// cache forever.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Client is the storage abstraction.
type Client struct {
	mc        *minio.Client
	bucket    string
	publicURL string // e.g. "http://localhost:9000" or "https://cdn.example.com"
}

// Config mirrors the fields in config.Config we care about.
// Exposed separately so the package has no import on the application config.
type Config struct {
	Endpoint  string // "localhost:9000" or "s3.us-west-002.backblazeb2.com"
	Region    string // "us-east-1" is fine for most
	Bucket    string
	AccessKey string
	SecretKey string
	UseSSL    bool
	PublicURL string // CDN origin or bucket URL, no trailing slash
}

// New creates a storage client and ensures the bucket exists (idempotent).
func New(ctx context.Context, cfg Config) (*Client, error) {
	mc, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: new client: %w", err)
	}

	// In dev we auto-create the bucket; in production the bucket is
	// pre-provisioned so this just reports existence.
	exists, err := mc.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("storage: bucket check: %w", err)
	}
	if !exists {
		if err := mc.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{Region: cfg.Region}); err != nil {
			// Someone else may have created it between Exists and MakeBucket.
			if ok, _ := mc.BucketExists(ctx, cfg.Bucket); !ok {
				return nil, fmt.Errorf("storage: make bucket: %w", err)
			}
		}
	}

	return &Client{
		mc:        mc,
		bucket:    cfg.Bucket,
		publicURL: strings.TrimRight(cfg.PublicURL, "/"),
	}, nil
}

// PresignPut returns a time-limited URL the browser can PUT an object to
// directly — bytes never pass through the Go API. This is the standard
// pattern for large uploads (multi-MB images) and is the only way to avoid
// API memory/CPU costs scaling with upload size.
//
// contentType is required because signature v4 signs it as a header; the
// browser PUT must send the exact same Content-Type or S3 rejects it.
func (c *Client) PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, error) {
	reqParams := make(url.Values)
	// Force Content-Type binding into the signature so clients can't upload
	// executables under an image key.
	u, err := c.mc.PresignHeader(ctx, "PUT", c.bucket, key, ttl, reqParams, map[string][]string{
		"Content-Type": {contentType},
	})
	if err != nil {
		return "", fmt.Errorf("storage: presign put %q: %w", key, err)
	}
	return u.String(), nil
}

// Get fetches an object from the bucket. Used by the transform worker to
// read the original upload before re-encoding.
func (c *Client) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("storage: get %q: %w", key, err)
	}
	// GetObject returns lazily; trigger the request so errors surface now
	// rather than on first Read.
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		return nil, fmt.Errorf("storage: stat %q: %w", key, err)
	}
	return obj, nil
}

// Put uploads bytes server-side (used by the transform worker to write
// resized variants). Sets long immutable Cache-Control — variants are
// content-addressable so their bytes never change.
func (c *Client) Put(ctx context.Context, key, contentType string, body io.Reader, size int64) error {
	_, err := c.mc.PutObject(ctx, c.bucket, key, body, size, minio.PutObjectOptions{
		ContentType:  contentType,
		CacheControl: "public, max-age=31536000, immutable",
	})
	if err != nil {
		return fmt.Errorf("storage: put %q: %w", key, err)
	}
	return nil
}

// Delete removes an object. Used on rollback / admin cleanup.
func (c *Client) Delete(ctx context.Context, key string) error {
	if err := c.mc.RemoveObject(ctx, c.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("storage: delete %q: %w", key, err)
	}
	return nil
}

// Exists reports whether a key is present.
func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	_, err := c.mc.StatObject(ctx, c.bucket, key, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	var merr minio.ErrorResponse
	if errors.As(err, &merr) && merr.StatusCode == 404 {
		return false, nil
	}
	return false, err
}

// PublicURL returns the browser-reachable URL for a key. For local MinIO
// this is http://localhost:9000/<bucket>/<key>; for a CDN-fronted bucket
// it's https://cdn.example.com/<key>.
//
// We detect whether the public URL already includes the bucket path
// (common with CDN + custom hostname) and skip double-prefixing.
func (c *Client) PublicURL(key string) string {
	if strings.Contains(c.publicURL, "/"+c.bucket) || strings.HasSuffix(c.publicURL, c.bucket) {
		return c.publicURL + "/" + key
	}
	return c.publicURL + "/" + c.bucket + "/" + key
}

// Bucket returns the configured bucket name (useful for logs).
func (c *Client) Bucket() string { return c.bucket }
