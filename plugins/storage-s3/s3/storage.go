package s3

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	cedana_config "github.com/cedana/cedana/pkg/config"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/rs/zerolog/log"
)

const PATH_PREFIX = "s3://"

// S3 storage
type Storage struct {
	ctx    context.Context
	client *s3.Client
}

func NewStorage(ctx context.Context) (cedana_io.Storage, error) {
	if cedana_config.Global.AWS.AccessKeyID == "" || cedana_config.Global.AWS.SecretAccessKey == "" {
		return nil, fmt.Errorf("AWS AccessKeyID and SecretAccessKey must be set in config for using S3 storage")
	}
	if cedana_config.Global.AWS.Region == "" {
		log.Warn().Str("storage", "S3").Msg("AWS Region is not set in the configuration")
	}

	if cedana_config.Global.AWS.Endpoint != "" {
		log.Info().Str("storage", "S3").Msgf("Using custom S3 endpoint: %s", cedana_config.Global.AWS.Endpoint)
	}

	credProvider := credentials.NewStaticCredentialsProvider(
		cedana_config.Global.AWS.AccessKeyID,
		cedana_config.Global.AWS.SecretAccessKey,
		"",
	)

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithCredentialsProvider(credProvider),
		config.WithRegion(cedana_config.Global.AWS.Region),
	)

	if err != nil {
		return nil, err
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpoint := cedana_config.Global.AWS.Endpoint; endpoint != "" {
			o.BaseEndpoint = &endpoint
			o.UsePathStyle = true
		}
	})

	storage := &Storage{
		ctx:    ctx,
		client: client,
	}

	return storage, nil
}

func (s *Storage) Open(path string) (io.ReadCloser, error) {
	bucket, key, err := s.sanitizePath(path)
	if err != nil {
		return nil, err
	}

	// Sanity check: ensure the bucket exists
	_, err = s.client.GetBucketEncryption(s.ctx, &s3.GetBucketEncryptionInput{
		Bucket: &bucket,
	})
	if err != nil {
		return nil, err
	}

	return NewFile(s.ctx, s.client, bucket, key), nil
}

func (s *Storage) Create(path string) (io.WriteCloser, error) {
	bucket, key, err := s.sanitizePath(path)
	if err != nil {
		return nil, err
	}

	// Sanity check: ensure the bucket exists
	_, err = s.client.GetBucketEncryption(s.ctx, &s3.GetBucketEncryptionInput{
		Bucket: &bucket,
	})
	if err != nil {
		return nil, err
	}

	return NewFile(s.ctx, s.client, bucket, key), nil
}

func (s *Storage) Delete(path string) error {
	_, _, err := s.sanitizePath(path)
	if err != nil {
		return err
	}

	return fmt.Errorf("this operation is currently not supported for s3 storage")
}

func (s *Storage) IsDir(path string) (bool, error) {
	_, _, err := s.sanitizePath(path)
	if err != nil {
		return false, err
	}

	return true, nil // S3 does not support directories in the same way as local filesystems
}

func (s *Storage) ReadDir(path string) ([]string, error) {
	bucket, key, err := s.sanitizePath(path)
	if err != nil {
		return nil, err
	}

	if !strings.HasSuffix(key, "/") {
		key += "/" // Required if bucket is an "S3 directory bucket"
	}

	query := &s3.ListObjectsV2Input{
		Bucket: &bucket,
		Prefix: &key,
	}

	var list []string

	paginator := s3.NewListObjectsV2Paginator(s.client, query)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(s.ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list objects in bucket %s: %w", bucket, err)
		}

		for _, obj := range page.Contents {
			if obj.Key != nil {
				list = append(list, *obj.Key)
			}
		}
	}

	return list, nil
}

func (s *Storage) IsRemote() bool {
	return true
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
	if len(parts) < 2 {
		return "", "", fmt.Errorf("path must be of the form %s<bucket>/<key>", PATH_PREFIX)
	}

	bucket = parts[0]
	key = parts[1]

	return bucket, key, err
}
