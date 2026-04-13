package storage

import (
	"context"
	"fmt"
	"io"
	"path"

	"cloud.google.com/go/storage"
	"github.com/colormechadd/mailaroo/internal/config"
)

type GCSStorage struct {
	client *storage.Client
	bucket string
	prefix string
}

func NewGCSStorage(ctx context.Context, cfg config.GCSStorageConfig) (*GCSStorage, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCS client: %w", err)
	}

	return &GCSStorage{
		client: client,
		bucket: cfg.Bucket,
		prefix: cfg.Prefix,
	}, nil
}

func (g *GCSStorage) fullKey(key string) string {
	return path.Join(g.prefix, key)
}

func (g *GCSStorage) Save(ctx context.Context, key string, reader io.Reader) error {
	w := g.client.Bucket(g.bucket).Object(g.fullKey(key)).NewWriter(ctx)
	if _, err := io.Copy(w, reader); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

func (g *GCSStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return g.client.Bucket(g.bucket).Object(g.fullKey(key)).NewReader(ctx)
}

func (g *GCSStorage) Delete(ctx context.Context, key string) error {
	return g.client.Bucket(g.bucket).Object(g.fullKey(key)).Delete(ctx)
}

func (g *GCSStorage) Exists(ctx context.Context, key string) (bool, error) {
	_, err := g.client.Bucket(g.bucket).Object(g.fullKey(key)).Attrs(ctx)
	if err == nil {
		return true, nil
	}
	if err == storage.ErrObjectNotExist {
		return false, nil
	}
	return false, err
}
