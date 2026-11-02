package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// StoreBlobs adapts the daemon's compressed db.Blob() store. DirBlobs has a
// different layout and must not be used to access that directory directly.
type StoreBlobs struct{ Store store.BlobStore }

func (b StoreBlobs) Put(digest string, r io.Reader) error {
	data, err := io.ReadAll(io.LimitReader(r, MaxBlobBytes+1))
	if err != nil {
		return err
	}
	if len(data) > MaxBlobBytes {
		return fmt.Errorf("gateway: blob exceeds limit")
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != digest {
		return fmt.Errorf("gateway: digest mismatch")
	}
	_, err = b.Store.Put(context.Background(), bytes.NewReader(data))
	return err
}
func (b StoreBlobs) Get(digest string) (io.ReadCloser, int64, error) {
	info, err := b.Store.Stat(context.Background(), digest)
	if err != nil {
		return nil, 0, err
	}
	r, err := b.Store.Open(context.Background(), digest)
	return r, info.Size, err
}
func (b StoreBlobs) Stat(digest string) (int64, bool) {
	info, err := b.Store.Stat(context.Background(), digest)
	return info.Size, err == nil
}
