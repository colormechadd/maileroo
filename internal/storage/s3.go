package storage

import (
	"context"
	"fmt"
	"io"
	"path"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	internalconfig "github.com/colormechadd/mailaroo/internal/config"
)

type S3Storage struct {
	client *s3.Client
	bucket string
	prefix string
}

func NewS3Storage(ctx context.Context, cfg internalconfig.S3StorageConfig) (*S3Storage, error) {
	// Modern way to handle custom endpoints in v2
	sdkCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(cfg.Region),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to load SDK config: %w", err)
	}

	client := s3.NewFromConfig(sdkCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true // Often needed for custom endpoints like Minio
		}
	})

	return &S3Storage{
		client: client,
		bucket: cfg.Bucket,
		prefix: cfg.Prefix,
	}, nil
}

func (s *S3Storage) fullKey(key string) string {
	return path.Join(s.prefix, key)
}

func (s *S3Storage) Save(ctx context.Context, key string, reader io.Reader) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.fullKey(key)),
		Body:   reader,
	})
	return err
}

func (s *S3Storage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.fullKey(key)),
	})
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}

func (s *S3Storage) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.fullKey(key)),
	})
	return err
}

func (s *S3Storage) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.fullKey(key)),
	})
	if err == nil {
		return true, nil
	}
	return false, nil
}
