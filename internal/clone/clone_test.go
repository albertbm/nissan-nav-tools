package clone

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyOfReadOnlySourceIsWritable(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "src")
	if err := os.WriteFile(src, []byte("card"), 0o444); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(d, "dst")
	if err := Copy(src, dst); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(dst, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("copy of a read-only source isn't writable: %v", err)
	}
	f.Close()
}
