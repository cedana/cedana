package gcs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"cloud.google.com/go/storage"
	cedana_config "github.com/cedana/cedana/pkg/config"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/rs/zerolog/log"
	"google.golang.org/api/iterator"
)

const PATH_PREFIX = "gs://"

// GCS storage
type Storage struct {
	client *storage.Client
}

func NewStorage(ctx context.Context) (cedana_io.Storage, error) {
	settings := cedana_config.Global.GCS
	if settings.EmulatorHost != "" {
		log.Info().Str("storage", "GCS").Str("emulator_host", settings.EmulatorHost).Msg("Using a GCS emulator")
	}
	opts, err := ClientOptions(settings)
	if err != nil {
		return nil, err
	}
	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCS client: %w", err)
	}
	return &Storage{client: client}, nil
}

// NewStorageWithClient is the storage over a given client, for tests
func NewStorageWithClient(client *storage.Client) *Storage {
	return &Storage{client: client}
}

func (s *Storage) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	bucket, key, err := s.sanitizePath(path)
	if err != nil {
		return nil, err
	}
	log.Debug().Str("bucket", bucket).Str("key", key).Msg("using GCS storage path")

	reader, err := s.client.Bucket(bucket).Object(key).NewReader(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) || errors.Is(err, storage.ErrBucketNotExist) {
			return nil, fmt.Errorf("%s/%s does not exist", bucket, key)
		}
		return nil, fmt.Errorf("failed to get object %s/%s: %w", bucket, key, err)
	}
	return reader, nil
}

func (s *Storage) Create(ctx context.Context, path string) (io.WriteCloser, error) {
	bucket, key, err := s.sanitizePath(path)
	if err != nil {
		return nil, err
	}

	// Sanity check: ensure the bucket exists, so a dump fails before it writes
	if _, err := s.client.Bucket(bucket).Attrs(ctx); err != nil {
		return nil, fmt.Errorf("failed to access bucket %q: %w", bucket, err)
	}

	return NewFile(ctx, s.client, bucket, key), nil
}

// Delete removes the object at path and, since a streamed checkpoint is a
// "directory" of shard objects under its path, every object under path/.
func (s *Storage) Delete(ctx context.Context, path string) error {
	bucket, key, err := s.sanitizePath(path)
	if err != nil {
		return err
	}

	keys := []string{key}
	prefix := strings.TrimSuffix(key, "/") + "/"
	it := s.client.Bucket(bucket).Objects(ctx, &storage.Query{Prefix: prefix})
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to list objects under %s/%s: %w", bucket, prefix, err)
		}
		keys = append(keys, attrs.Name)
	}

	for _, key := range keys {
		err := s.client.Bucket(bucket).Object(key).Delete(ctx)
		if err != nil && !errors.Is(err, storage.ErrObjectNotExist) {
			return fmt.Errorf("failed to delete object %s/%s: %w", bucket, key, err)
		}
	}
	return nil
}

// ChecksumPath returns the store's CRC32C of the object at path, "crc32c:<hex>".
// GCS computes it for every object, of the whole object.
func (s *Storage) ChecksumPath(ctx context.Context, path string) (string, error) {
	bucket, key, err := s.sanitizePath(path)
	if err != nil {
		return "", err
	}
	attrs, err := s.client.Bucket(bucket).Object(key).Attrs(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get the attributes of %s/%s: %w", bucket, key, err)
	}
	return cedana_io.FormatChecksum(attrs.CRC32C), nil
}

func (s *Storage) IsDir(_ context.Context, path string) (bool, error) {
	_, _, err := s.sanitizePath(path)
	if err != nil {
		return false, err
	}

	return true, nil // GCS has no directories; a prefix stands for one, as in S3
}

func (s *Storage) ReadDir(ctx context.Context, path string) ([]string, error) {
	bucket, key, err := s.sanitizePath(path)
	if err != nil {
		return nil, err
	}

	if !strings.HasSuffix(key, "/") {
		key += "/"
	}

	var list []string
	it := s.client.Bucket(bucket).Objects(ctx, &storage.Query{Prefix: key})
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list objects in bucket %s: %w", bucket, err)
		}
		list = append(list, attrs.Name)
	}

	return list, nil
}

func (s *Storage) IsRemote() bool {
	return true
}

func (s *Storage) CreatePath(ctx context.Context, dir, name string) (path string, cleanup func(cancel bool) error, err error) {
	return "", nil, fmt.Errorf("GCS Plugin Does not support CreatePath()")
}

func (s *Storage) ReadPath(ctx context.Context, path string) (string, func() error, error) {
	return path, nil, nil
}

/////////////
// Helpers //
/////////////

func (s *Storage) sanitizePath(path string) (bucket string, key string, err error) {
	if !strings.HasPrefix(path, PATH_PREFIX) {
		return "", "", fmt.Errorf("path must start with %s", PATH_PREFIX)
	}

	path = strings.TrimPrefix(path, PATH_PREFIX)
	path = strings.TrimPrefix(path, "/")

	if path == "" {
		return "", "", fmt.Errorf("path cannot be empty")
	}

	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 || parts[1] == "" {
		return "", "", fmt.Errorf("path must be of the form %s<bucket>/<key>", PATH_PREFIX)
	}

	return parts[0], parts[1], nil
}
