package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

// S3Storage implements StorageBackend for AWS S3 storage.
type S3Storage struct {
	bucket      string
	prefix      string
	region      string
	s3Client    *s3.S3
	uploader    *s3manager.Uploader
	downloader  *s3manager.Downloader
	session     *session.Session
}

// NewS3Storage creates a new S3 storage backend.
// If accessKey and secretKey are empty, AWS SDK will use default credential chain.
func NewS3Storage(bucket, region, prefix, endpoint string, usePathStyle bool, accessKey, secretKey string) (*S3Storage, error) {
	if bucket == "" {
		return nil, fmt.Errorf("bucket cannot be empty: %w", ErrInvalidArg)
	}

	if region == "" {
		return nil, fmt.Errorf("region cannot be empty: %w", ErrInvalidArg)
	}

	// Build session configuration
	cfg := aws.NewConfig().
		WithRegion(region).
		WithS3ForcePathStyle(usePathStyle)

	// Set custom endpoint if provided (for S3-compatible services)
	if endpoint != "" {
		cfg = cfg.WithEndpoint(endpoint)
	}

	// Set credentials if provided
	if accessKey != "" && secretKey != "" {
		cfg = cfg.WithCredentials(credentials.NewStaticCredentials(accessKey, secretKey, ""))
	}

	sess, err := session.NewSession(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	s3Client := s3.New(sess)
	uploader := s3manager.NewUploader(sess)
	downloader := s3manager.NewDownloader(sess)

	return &S3Storage{
		bucket:     bucket,
		prefix:     prefix,
		region:     region,
		s3Client:   s3Client,
		uploader:   uploader,
		downloader: downloader,
		session:    sess,
	}, nil
}

// Initialize performs initial S3 bucket checks.
func (s *S3Storage) Initialize(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Check if bucket exists
	_, err := s.s3Client.HeadBucketWithContext(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})

	if err != nil {
		return fmt.Errorf("failed to access S3 bucket: %w", err)
	}

	return nil
}

// Write uploads data to S3.
func (s *S3Storage) Write(ctx context.Context, namespace string, data []byte) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	if namespace == "" {
		return "", fmt.Errorf("namespace cannot be empty: %w", ErrInvalidArg)
	}

	// Generate S3 key with namespace prefix
	key := s.buildKey(namespace)

	// Upload to S3
	_, err := s.uploader.UploadWithContext(ctx, &s3manager.UploadInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   strings.NewReader(string(data)),
	})

	if err != nil {
		return "", fmt.Errorf("failed to upload to S3: %w", err)
	}

	return key, nil
}

// Read downloads data from S3.
func (s *S3Storage) Read(ctx context.Context, path string) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if path == "" {
		return nil, fmt.Errorf("path cannot be empty: %w", ErrInvalidArg)
	}

	// Prevent directory traversal
	if strings.Contains(path, "..") {
		return nil, fmt.Errorf("invalid path: %w", ErrInvalidArg)
	}

	buffer := &aws.WriteAtBuffer{}
	_, err := s.downloader.DownloadWithContext(ctx, buffer, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(path),
	})

	if err != nil {
		if strings.Contains(err.Error(), "NoSuchKey") {
			return nil, fmt.Errorf("object not found: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("failed to download from S3: %w", err)
	}

	return buffer.Bytes(), nil
}

// Delete removes an object from S3.
func (s *S3Storage) Delete(ctx context.Context, path string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if path == "" {
		return fmt.Errorf("path cannot be empty: %w", ErrInvalidArg)
	}

	if strings.Contains(path, "..") {
		return fmt.Errorf("invalid path: %w", ErrInvalidArg)
	}

	_, err := s.s3Client.DeleteObjectWithContext(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(path),
	})

	if err != nil {
		return fmt.Errorf("failed to delete from S3: %w", err)
	}

	return nil
}

// List returns all objects in a namespace prefix.
func (s *S3Storage) List(ctx context.Context, namespace string) ([]string, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if namespace == "" {
		return nil, fmt.Errorf("namespace cannot be empty: %w", ErrInvalidArg)
	}

	prefix := namespace + "/"
	if s.prefix != "" {
		prefix = s.prefix + "/" + prefix
	}

	var paths []string

	// Use pagination to handle large result sets
	err := s.s3Client.ListObjectsPagesWithContext(ctx, &s3.ListObjectsInput{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	}, func(page *s3.ListObjectsOutput, lastPage bool) bool {
		for _, obj := range page.Contents {
			paths = append(paths, *obj.Key)
		}
		return true // Continue paginating
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list S3 objects: %w", err)
	}

	return paths, nil
}

// Health checks if S3 is accessible.
func (s *S3Storage) Health(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	_, err := s.s3Client.HeadBucketWithContext(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})

	if err != nil {
		return fmt.Errorf("S3 health check failed: %w", err)
	}

	return nil
}

// Close closes the AWS session.
func (s *S3Storage) Close(ctx context.Context) error {
	if s.session != nil {
		return nil // AWS SDK session doesn't need explicit close
	}
	return nil
}

// buildKey generates the S3 object key from namespace and prefix.
func (s *S3Storage) buildKey(namespace string) string {
	if s.prefix != "" {
		return fmt.Sprintf("%s/%s/data_%d.dat", s.prefix, namespace, 0) // Timestamp would go here
	}
	return fmt.Sprintf("%s/data_%d.dat", namespace, 0)
}
