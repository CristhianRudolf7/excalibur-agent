package wireengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

// browserFiles provides durable embedded captures when no daemon DB is open.
// Files are private, content-addressed and published by atomic rename.
type browserFiles struct{ dir string }

func (b browserFiles) Put(ctx context.Context, _ string, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(b.dir, 0700); err != nil {
		return "", errors.New("browser: blob directory unavailable")
	}
	sum := sha256.Sum256(data)
	ref := hex.EncodeToString(sum[:])
	f, err := os.CreateTemp(b.dir, ".capture-")
	if err != nil {
		return "", errors.New("browser: blob unavailable")
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return "", errors.New("browser: blob write failed")
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	if err = os.Rename(f.Name(), filepath.Join(b.dir, ref)); err != nil {
		return "", err
	}
	return ref, nil
}
