package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Client wraps two minio-go clients:
//   - internal: used for server-side I/O from within the platform network (e.g. control-plane
//     uploading test fixtures, build-service downloading source). Endpoint is the docker-compose
//     service hostname like "minio:9000".
//   - public:   used to generate presigned URLs that *external* clients (the Skill on a developer
//     laptop) follow. Endpoint must be a hostname the client can resolve, e.g. "10.0.0.5:9000" or
//     "s3.lab.example.com".
//
// minio-go's PresignedXxx methods are pure client-side computation; the URL contains whatever
// endpoint the client was created with, and SigV4 includes the Host header in the signature.
// So the only way to get a useful presigned URL is to construct a minio.Client whose endpoint
// matches what the consumer will hit.
type Client struct {
	internal *minio.Client
	public   *minio.Client
	bucket   string
}

type Options struct {
	Endpoint       string // e.g. "minio:9000" (server-side I/O)
	PublicEndpoint string // e.g. "10.0.0.5:9000" — defaults to Endpoint when empty
	AccessKey      string
	SecretKey      string
	Bucket         string
	UseSSL         bool
	PublicUseSSL   bool
}

func New(opts Options) (*Client, error) {
	internal, err := minio.New(opts.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(opts.AccessKey, opts.SecretKey, ""),
		Secure: opts.UseSSL,
	})
	if err != nil {
		return nil, err
	}
	public := internal
	if opts.PublicEndpoint != "" && opts.PublicEndpoint != opts.Endpoint {
		p, err := minio.New(opts.PublicEndpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(opts.AccessKey, opts.SecretKey, ""),
			Secure: opts.PublicUseSSL,
		})
		if err != nil {
			return nil, fmt.Errorf("init public minio client: %w", err)
		}
		public = p
	}
	return &Client{internal: internal, public: public, bucket: opts.Bucket}, nil
}

func (c *Client) EnsureBucket(ctx context.Context) error {
	ok, err := c.internal.BucketExists(ctx, c.bucket)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return c.internal.MakeBucket(ctx, c.bucket, minio.MakeBucketOptions{})
}

// PresignedUploadURL returns a PUT-able URL the Skill can directly upload to.
// Uses the public client so the URL host is reachable from outside the cluster.
func (c *Client) PresignedUploadURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	u, err := c.public.PresignedPutObject(ctx, c.bucket, key, ttl)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// PresignedDownloadURL — used when an external client needs a direct GET.
func (c *Client) PresignedDownloadURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	u, err := c.public.PresignedGetObject(ctx, c.bucket, key, ttl, url.Values{})
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// Put streams data into the bucket (server-side path).
func (c *Client) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := c.internal.PutObject(ctx, c.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	return err
}

// Get fetches an object (server-side path — used by Build Service).
func (c *Client) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := c.internal.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	return obj, nil
}

func (c *Client) Bucket() string { return c.bucket }

// SourceKey returns the canonical object key for a deployment's source tarball.
func SourceKey(slug, deploymentID string) string {
	return fmt.Sprintf("%s/%s.tar.zst", slug, deploymentID)
}
