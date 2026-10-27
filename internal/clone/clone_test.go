package clone

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSameFile_SamePath(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "x.img")
	os.WriteFile(p, []byte("card"), 0o644)
	same, err := SameFile(p, p)
	if err != nil {
		t.Fatal(err)
	}
	if !same {
		t.Error("same path reported as different files")
	}
}

func TestSameFile_DifferentSpellingOfSamePath(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "x.img")
	os.WriteFile(p, []byte("card"), 0o644)
	spelled := filepath.Join(d, "sub", "..", "x.img")
	same, err := SameFile(p, spelled)
	if err != nil {
		t.Fatal(err)
	}
	if !same {
		t.Error("a re-spelled path to the same file reported as different")
	}
}

func TestSameFile_Symlink(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "x.img")
	os.WriteFile(p, []byte("card"), 0o644)
	link := filepath.Join(d, "link.img")
	if err := os.Symlink(p, link); err != nil {
		t.Fatal(err)
	}
	same, err := SameFile(p, link)
	if err != nil {
		t.Fatal(err)
	}
	if !same {
		t.Error("a symlink to the input reported as a different file")
	}
}

func TestSameFile_HardLink(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "x.img")
	os.WriteFile(p, []byte("card"), 0o644)
	link := filepath.Join(d, "link.img")
	if err := os.Link(p, link); err != nil {
		t.Fatal(err)
	}
	same, err := SameFile(p, link)
	if err != nil {
		t.Fatal(err)
	}
	if !same {
		t.Error("a hard link to the input reported as a different file")
	}
}

func TestSameFile_CaseOnlySpelling(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "x.img")
	os.WriteFile(p, []byte("card"), 0o644)
	upper := filepath.Join(d, "X.IMG")
	if _, err := os.Stat(upper); err != nil {
		t.Skip("volume is case-sensitive, nothing to check")
	}
	same, err := SameFile(p, upper)
	if err != nil {
		t.Fatal(err)
	}
	if !same {
		t.Error("a case-only spelling on a case-insensitive volume reported as a different file")
	}
}

func TestSameFile_NeitherExists(t *testing.T) {
	d := t.TempDir()
	same, err := SameFile(filepath.Join(d, "a.img"), filepath.Join(d, "b.img"))
	if err != nil {
		t.Fatal(err)
	}
	if same {
		t.Error("two paths that don't exist reported as the same file")
	}
}

func TestSameFile_OutDoesNotExistYet(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "x.img")
	os.WriteFile(p, []byte("card"), 0o644)
	same, err := SameFile(p, filepath.Join(d, "new.img"))
	if err != nil {
		t.Fatal(err)
	}
	if same {
		t.Error("a path that doesn't exist yet reported as the same file as the input")
	}
}

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
