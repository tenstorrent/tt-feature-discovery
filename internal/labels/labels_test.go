package labels

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tt-fd")

	if err := Write(path, map[string]string{
		"tenstorrent.com/device.present": "true",
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "tenstorrent.com/device.present=true\n"
	if string(got) != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestWriteDeterministicOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tt-fd")

	if err := Write(path, map[string]string{
		"tenstorrent.com/b": "2",
		"tenstorrent.com/a": "1",
		"tenstorrent.com/c": "3",
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, _ := os.ReadFile(path)
	want := "tenstorrent.com/a=1\ntenstorrent.com/b=2\ntenstorrent.com/c=3\n"
	if string(got) != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestWriteCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "subdir", "tt-fd")
	if err := Write(path, map[string]string{"k": "v"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file at %s: %v", path, err)
	}
}

func TestWriteIsNoopWhenUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tt-fd")
	labels := map[string]string{"tenstorrent.com/device.present": "true"}

	if err := Write(path, labels); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	stat1, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	// Second write with same content must not touch the file (NFD watches
	// this directory; rewriting would cause spurious reconciles).
	if err := Write(path, labels); err != nil {
		t.Fatalf("second Write: %v", err)
	}
	stat2, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !stat1.ModTime().Equal(stat2.ModTime()) {
		t.Errorf("mtime changed on no-op write: %v -> %v", stat1.ModTime(), stat2.ModTime())
	}
}
