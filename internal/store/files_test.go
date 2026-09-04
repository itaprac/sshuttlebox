package store

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestAtomicWriteFailurePreservesDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "destination")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(path, "original")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(path, []byte("replacement")); err == nil {
		t.Fatal("expected rename error")
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "keep" {
		t.Fatalf("destination damaged: %q %v", data, err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, ".shbx-write-*"))
	if len(files) != 0 {
		t.Fatalf("temporary files leaked: %v", files)
	}
}

func TestReadersNeverSeePartialWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	first := strings.Repeat("a", 10000)
	second := strings.Repeat("b", 20000)
	if err := AtomicWrite(path, []byte(first)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	stop := make(chan struct{})
	fail := make(chan string, 1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			data, err := os.ReadFile(path)
			if err != nil || (string(data) != first && string(data) != second) {
				select {
				case fail <- "reader saw partial or absent file":
				default:
				}
				return
			}
		}
	}()
	for i := 0; i < 8; i++ {
		value := first
		if i%2 == 0 {
			value = second
		}
		if err := AtomicWrite(path, []byte(value)); err != nil {
			t.Error(err)
			break
		}
	}
	close(stop)
	wg.Wait()
	select {
	case msg := <-fail:
		t.Fatal(msg)
	default:
	}
}
