package utils

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteToS3Integration(t *testing.T) {
	// Skip if not running integration tests
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// TODO: Test getting auth info from propagator

	bucketName := os.Getenv("TEST_S3_BUCKET")
	if bucketName == "" {
		bucketName = "cedana-test"
	}

	testDir, err := os.MkdirTemp("", "s3-upload-test")
	require.NoError(t, err)
	defer os.RemoveAll(testDir)

	testFilePath := filepath.Join(testDir, "testfile.dat")
	testFileSize := int64(1024 * 1024) // 1MB
	err = createRandomFile(testFilePath, testFileSize)

	cfg, err := config.LoadDefaultConfig(context.Background())

	s3Client := s3.NewFromConfig(cfg)

	compressionTypes := []string{"gzip", "lz4"}

	for _, compression := range compressionTypes {
		t.Run(fmt.Sprintf("Compression_%s", compression), func(t *testing.T) {
			source, err := os.Open(testFilePath)

			key := fmt.Sprintf("test-uploads/%s/testfile-%s.dat", compression, generateUniqueID())

			start := time.Now()
			written, err := WriteToS3(
				context.Background(),
				s3Client,
				source,
				bucketName,
				key,
				compression,
			)
			require.NoError(t, err, "WriteToS3 failed")
			assert.Equal(t, testFileSize, written, "Number of bytes written doesn't match file size")
			elapsed := time.Since(start)
			t.Logf("Uploaded %d bytes in %s using %s", written, elapsed, compression)

			_, err = s3Client.HeadObject(context.Background(), &s3.HeadObjectInput{
				Bucket: aws.String(bucketName),
				Key:    aws.String(key),
			})
			assert.NoError(t, err, "Uploaded file not found in S3")

			_, err = s3Client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
				Bucket: aws.String(bucketName),
				Key:    aws.String(key),
			})
			assert.NoError(t, err, "Failed to clean up test file from S3")
		})
	}

	t.Run("Invalid_Compression", func(t *testing.T) {
		source, err := os.Open(testFilePath)
		require.NoError(t, err)

		key := fmt.Sprintf("test-uploads/invalid/testfile-%s.dat", generateUniqueID())

		_, err = WriteToS3(
			context.Background(),
			s3Client,
			source,
			bucketName,
			key,
			"invalid-compression",
		)
		assert.Error(t, err, "Expected error with invalid compression type")
		assert.Contains(t, err.Error(), "compression", "Error should mention compression")
	})

	t.Run("Invalid_Bucket", func(t *testing.T) {
		source, err := os.Open(testFilePath)
		require.NoError(t, err)

		// Call with invalid bucket
		_, err = WriteToS3(
			context.Background(),
			s3Client,
			source,
			"bucket-that-definitely-does-not-exist-123456789",
			"test.dat",
			"gzip",
		)
		assert.Error(t, err, "Expected error with invalid bucket")
	})
}

func createRandomFile(path string, size int64) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	buf := make([]byte, 1024)
	var written int64

	for written < size {
		toWrite := size - written
		if toWrite > int64(len(buf)) {
			toWrite = int64(len(buf))
		}

		_, err := rand.Read(buf[:toWrite])
		if err != nil {
			return err
		}

		n, err := file.Write(buf[:toWrite])
		if err != nil {
			return err
		}

		written += int64(n)
	}

	return nil
}

func generateUniqueID() string {
	buf := make([]byte, 8)
	_, err := rand.Read(buf)
	if err != nil {
		return fmt.Sprintf("%d", os.Getpid())
	}
	return fmt.Sprintf("%x", buf)
}
