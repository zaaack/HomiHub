package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Driver is a key → content store. Keys are slash-separated logical paths.
type Driver interface {
	Save(ctx context.Context, key string, data io.Reader) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
}

type Local struct {
	Root string
}

// safeKey rejects absolute paths and traversal.
func safeKey(key string) bool {
	if key == "" || strings.HasPrefix(key, "/") || strings.HasPrefix(key, "\\") {
		return false
	}
	return !strings.Contains(key, "..")
}

func (l *Local) path(key string) string {
	return filepath.Join(l.Root, filepath.FromSlash(key))
}

func (l *Local) Save(_ context.Context, key string, data io.Reader) error {
	if !safeKey(key) {
		return os.ErrInvalid
	}
	p := l.path(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	// Write to a temp file in the same directory, then atomically rename into
	// place so a failed/interrupted upload never leaves a partial file.
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = os.Remove(tmpName)
	}
	if _, err := io.Copy(tmp, data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, p); err != nil {
		cleanup()
		return err
	}
	return nil
}

func (l *Local) Open(_ context.Context, key string) (io.ReadCloser, error) {
	if !safeKey(key) {
		return nil, os.ErrInvalid
	}
	return os.Open(l.path(key))
}

func (l *Local) Delete(_ context.Context, key string) error {
	if !safeKey(key) {
		return os.ErrInvalid
	}
	err := os.Remove(l.path(key))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (l *Local) Exists(_ context.Context, key string) (bool, error) {
	if !safeKey(key) {
		return false, nil
	}
	_, err := os.Stat(l.path(key))
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}
