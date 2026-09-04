// Package store provides private atomic files and process-wide update locks.
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// WithLock holds a separate lock file across an entire read-modify-write operation.
// Keeping the lock separate is required because AtomicWrite replaces the data inode.
func WithLock(path string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("open update lock: %w", err)
	}
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return err
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("update lock is busy: %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}

// AtomicWrite never truncates the old file and always installs mode 0600.
func AtomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".shbx-write-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func CloneMap[T any](src map[string]T) map[string]T {
	dst := make(map[string]T, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// MergeMap applies only local changes and rejects changes to the same record.
func MergeMap[T comparable](kind string, base, local, current map[string]T) (map[string]T, error) {
	out := CloneMap(current)
	keys := map[string]bool{}
	for k := range base {
		keys[k] = true
	}
	for k := range local {
		keys[k] = true
	}
	for k := range keys {
		b, bok := base[k]
		l, lok := local[k]
		c, cok := current[k]
		if b == l && bok == lok {
			continue
		}
		if (b != c || bok != cok) && (l != c || lok != cok) {
			return nil, fmt.Errorf("%s %q changed in another process; reload and try again", kind, k)
		}
		if lok {
			out[k] = l
		} else {
			delete(out, k)
		}
	}
	return out, nil
}
