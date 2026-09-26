// Package s3compat implements StorageProvider for one S3-compatible bucket
// (R2, MinIO, B2, AWS) via minio-go. File IDs are object keys; directories
// are prefixes with a trailing slash; root is "". Deletions move objects to
// the in-bucket .airdrive-trash/ prefix — never permanent.
package s3compat

//go:generate go run go.uber.org/mock/mockgen -source=s3compat.go -destination=mock/mock.go -package=mock

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/radosny/airdrive/internal/provider"
)

// TrashPrefix is the in-bucket location deletions are moved to.
const TrashPrefix = ".airdrive-trash/"

// Config carries everything needed to reach a bucket — no secrets.
type Config struct {
	Endpoint  string // https://<account>.r2.cloudflarestorage.com
	Region    string
	Bucket    string
	PathStyle bool
}

// Credentials pairs with Config and lives in the macOS Keychain.
type Credentials struct {
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
}

// Provider implements provider.StorageProvider for one bucket.
type Provider struct {
	locationID string
	client     *minio.Client
	bucket     string
	logger     *slog.Logger
}

// Interface conformance.
var (
	_ provider.StorageProvider = (*Provider)(nil)
	_ provider.Reconciler      = (*Provider)(nil)
)

// Reconciles reports that the bucket has no changes feed — the crawl must
// diff the walk against the index to detect deletions.
func (p *Provider) Reconciles() bool { return true }

// BucketLister validates connectivity for the connect wizard's Test button.
type BucketLister interface {
	ListBuckets(ctx context.Context) ([]string, error)
	// BucketExists probes one bucket — the fallback for bucket-scoped R2
	// tokens, which are denied account-level ListBuckets but can read their
	// own bucket.
	BucketExists(ctx context.Context, bucket string) (bool, error)
}

// NewBucketLister builds a client from raw connection parameters.
func NewBucketLister(endpoint, region, accessKeyID, secretAccessKey string, pathStyle bool) (BucketLister, error) {
	host, secure, err := parseEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	lookup := minio.BucketLookupAuto
	if pathStyle {
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(host, &minio.Options{
		Creds:        credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure:       secure,
		Region:       region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("s3compat: client: %w", err)
	}
	return &bucketLister{client: client}, nil
}

type bucketLister struct {
	client *minio.Client
}

func (b *bucketLister) ListBuckets(ctx context.Context) ([]string, error) {
	buckets, err := b.client.ListBuckets(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(buckets))
	for _, bk := range buckets {
		out = append(out, bk.Name)
	}
	return out, nil
}

func (b *bucketLister) BucketExists(ctx context.Context, bucket string) (bool, error) {
	return b.client.BucketExists(ctx, bucket)
}

// New builds a bucket-scoped provider from a non-secret config plus
// Keychain-resident credentials.
func New(locationID string, cfg Config, creds Credentials, logger *slog.Logger) (*Provider, error) {
	if logger == nil {
		logger = slog.Default()
	}
	endpoint, secure, err := parseEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	lookup := minio.BucketLookupAuto
	if cfg.PathStyle {
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(creds.AccessKeyID, creds.SecretAccessKey, ""),
		Secure:       secure,
		Region:       cfg.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("s3compat: client: %w", err)
	}
	return &Provider{locationID: locationID, client: client, bucket: cfg.Bucket, logger: logger}, nil
}

// DescribeError renders a minio error response with its status code and
// request id — the fields needed to diagnose R2 permission problems. minio
// returns ErrorResponse by value; both value and pointer forms are accepted.
func DescribeError(err error) string {
	var val minio.ErrorResponse
	if errors.As(err, &val) {
		return fmt.Sprintf("%s (HTTP %d, request %s)", val.Code, val.StatusCode, val.RequestID)
	}
	var ptr *minio.ErrorResponse
	if errors.As(err, &ptr) {
		return fmt.Sprintf("%s (HTTP %d, request %s)", ptr.Code, ptr.StatusCode, ptr.RequestID)
	}
	return err.Error()
}

// parseEndpoint splits scheme://host into minio-go's scheme-less endpoint.
func parseEndpoint(raw string) (string, bool, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", false, fmt.Errorf("s3compat: invalid endpoint %q", raw)
	}
	return u.Host, u.Scheme != "http", nil
}

func (p *Provider) ID() string          { return p.locationID }
func (p *Provider) Kind() provider.Kind { return provider.KindS3 }

// joinKey builds an object key under parentID ("" = bucket root);
// parentIDs without the trailing slash are normalised.
func joinKey(parent, name string) string {
	if parent == "" {
		return name
	}
	if !strings.HasSuffix(parent, "/") {
		parent += "/"
	}
	return parent + name
}

// parentOf maps a key to its directory prefix; root children map to "".
func parentOf(key string) string {
	trimmed := strings.TrimSuffix(key, "/")
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		return trimmed[:i+1]
	}
	return ""
}

// baseName maps a key to its display name.
func baseName(key string) string {
	trimmed := strings.TrimSuffix(key, "/")
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		return trimmed[i+1:]
	}
	return trimmed
}

func isDirKey(key string) bool { return strings.HasSuffix(key, "/") }

// toFileInfo builds the provider record; zero dates mark synthesized dirs.
func (p *Provider) toFileInfo(key string, size int64, etag string, mod time.Time) provider.FileInfo {
	kind := provider.FileKindFile
	if isDirKey(key) {
		kind = provider.FileKindDir
	}
	mime := provider.MimeFor(baseName(key), kind == provider.FileKindDir)
	return provider.FileInfo{
		ID:          key,
		LocationID:  p.locationID,
		ParentID:    parentOf(key),
		Name:        baseName(key),
		Kind:        kind,
		Size:        size,
		MIME:        mime,
		Etag:        etag,
		PreviewKind: provider.PreviewKind(mime),
		ModifiedAt:  mod,
	}
}
