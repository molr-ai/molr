//go:build linux || darwin

package installation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"molr.ai/runtime/internal/installrecord"
)

// Keep only digest-addressed, verified archives in the instance's private cache.
// Repair can reuse these bytes while still installing into a fresh directory.
func (e *Engine) cachedComponent(r *installrecord.Record, a componentAsset, destination string) error {
	cache := filepath.Join(componentRoot(r), "downloads")
	if err := mkdirOwned(cache, 0700, r.UID, r.GID); err != nil {
		return err
	}
	source := filepath.Join(cache, a.SHA256)
	if err := checkManaged(source, r.UID, nil); err == nil {
		if err = copyVerifiedComponent(source, destination, a); err == nil {
			return nil
		}
		os.Remove(destination)
	}
	if err := e.fetchComponent(a, destination); err != nil {
		return err
	}
	// A failed cache write must not discard an otherwise verified installation.
	temp, err := os.CreateTemp(cache, ".download-")
	if err != nil {
		return nil
	}
	name := temp.Name()
	temp.Close()
	os.Remove(name)
	defer os.Remove(name)
	if err = copyFile(name, destination, 0600, r.UID, r.GID); err == nil {
		_ = os.Rename(name, source)
	}
	return nil
}

func copyVerifiedComponent(source, destination string, a componentAsset) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != a.Size {
		return fmt.Errorf("invalid cached component")
	}
	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer dst.Close()
	digest := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, digest), io.LimitReader(src, a.Size+1))
	if err != nil {
		return err
	}
	if n != a.Size || hex.EncodeToString(digest.Sum(nil)) != a.SHA256 {
		return fmt.Errorf("cached component checksum mismatch")
	}
	return dst.Sync()
}
