package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/colormechadd/mailaroo/internal/config"
)

type LocalStorage struct {
	basePath string
}

func NewLocalStorage(cfg config.LocalStorageConfig) (*LocalStorage, error) {
	if err := os.MkdirAll(cfg.BasePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base storage directory: %w", err)
	}
	return &LocalStorage{basePath: cfg.BasePath}, nil
}

func (l *LocalStorage) fullPath(key string) (string, error) {
	p := filepath.Join(l.basePath, key)
	if !strings.HasPrefix(p, filepath.Clean(l.basePath)+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid storage key")
	}
	return p, nil
}


func (l *LocalStorage) Save(ctx context.Context, key string, reader io.Reader) error {
	fullPath, err := l.fullPath(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return err
	}

	f, err := os.Create(fullPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, reader)
	return err
}

func (l *LocalStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	fullPath, err := l.fullPath(key)
	if err != nil {
		return nil, err
	}
	return os.Open(fullPath)
}

func (l *LocalStorage) Delete(ctx context.Context, key string) error {
	fullPath, err := l.fullPath(key)
	if err != nil {
		return err
	}
	return os.Remove(fullPath)
}

func (l *LocalStorage) Exists(ctx context.Context, key string) (bool, error) {
	fullPath, err := l.fullPath(key)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(fullPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
