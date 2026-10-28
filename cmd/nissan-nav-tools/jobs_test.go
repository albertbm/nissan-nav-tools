package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"nissan-nav-tools/internal/disk"
)

// fakeCard stands in for a card's raw device handle: Read/Write are sequential,
// ReadAt/WriteAt are positional, same as the real thing.
type fakeCard struct {
	data []byte
	pos  int
}

func (f *fakeCard) Read(p []byte) (int, error) {
	if f.pos >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.pos:])
	f.pos += n
	return n, nil
}

func (f *fakeCard) ReadAt(p []byte, off int64) (int, error) {
	if int(off) >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (f *fakeCard) Write(p []byte) (int, error) {
	n := copy(f.data[f.pos:], p)
	f.pos += n
	return n, nil
}

func (f *fakeCard) WriteAt(p []byte, off int64) (int, error) {
	return copy(f.data[off:], p), nil
}

func (f *fakeCard) Close() error { return nil }

// countingOpener always hands back dev, and counts how many times it was asked to.
func countingOpener(dev disk.RawDevice) (opens *int, opener func(disk.Info, bool) (disk.RawDevice, error)) {
	n := 0
	return &n, func(disk.Info, bool) (disk.RawDevice, error) {
		n++
		return dev, nil
	}
}

func swapCardAccess(t *testing.T, opener func(disk.Info, bool) (disk.RawDevice, error)) *int {
	t.Helper()
	origOpen, origUnmount := openCard, unmountCard
	unmounts := 0
	openCard = opener
	unmountCard = func(string) error { unmounts++; return nil }
	t.Cleanup(func() { openCard, unmountCard = origOpen, origUnmount })
	return &unmounts
}

func TestBackupJobOpensTheCardOnce(t *testing.T) {
	data := bytes.Repeat([]byte("CARD"), 100)
	dev := &fakeCard{data: data}
	opens, opener := countingOpener(dev)
	unmounts := swapCardAccess(t, opener)

	d := disk.Info{Identifier: "disk9", Size: int64(len(data))}
	out := filepath.Join(t.TempDir(), "backup.img")
	size, err := backupJob(d, out, false, nil, nil, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if *opens != 1 {
		t.Errorf("openCard called %d times, want 1", *opens)
	}
	if *unmounts != 1 {
		t.Errorf("unmountCard called %d times, want 1", *unmounts)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) || size != int64(len(data)) {
		t.Error("backup content doesn't match the card")
	}
}

func TestApplyCardPatchOpensTheCardOnce(t *testing.T) {
	dev := &fakeCard{data: make([]byte, 2048)}
	opens, opener := countingOpener(dev)
	unmounts := swapCardAccess(t, opener)

	d := disk.Info{Identifier: "disk9", Size: int64(len(dev.data))}
	patch := bytes.Repeat([]byte{0xAB}, 512)
	ranges := []disk.Range{{Offset: 512, Data: patch}}
	if err := applyCardPatch(d, ranges, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if *opens != 1 {
		t.Errorf("openCard called %d times, want 1", *opens)
	}
	if *unmounts != 1 {
		t.Errorf("unmountCard called %d times, want 1", *unmounts)
	}
	if !bytes.Equal(dev.data[512:1024], patch) {
		t.Error("patch wasn't written to the card")
	}
}

func TestRestoreJobAndVerifyOpenTheCardOnce(t *testing.T) {
	data := bytes.Repeat([]byte("IMG1"), 128)
	dev := &fakeCard{data: make([]byte, len(data))}
	opens, opener := countingOpener(dev)
	unmounts := swapCardAccess(t, opener)

	imgPath := filepath.Join(t.TempDir(), "card.img")
	if err := os.WriteFile(imgPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	d := disk.Info{Identifier: "disk9", Size: int64(len(data))}
	card, err := restoreJob(d, imgPath, nil, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyRestore(card, imgPath, nil, func(string) {}); err != nil {
		t.Fatal(err)
	}
	card.Close()

	if *opens != 1 {
		t.Errorf("openCard called %d times, want 1", *opens)
	}
	if *unmounts != 1 {
		t.Errorf("unmountCard called %d times, want 1", *unmounts)
	}
}

// The ME0 card used to leave a full-sized, untouched-looking copy behind on a failed patch.
func TestPatchJobCleansUpOnFailure(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.img")
	out := filepath.Join(dir, "out.img")
	if err := os.WriteFile(in, []byte("not a nav image"), 0o644); err != nil {
		t.Fatal(err)
	}

	var logged []string
	err := patchJob(in, out, []int{0}, nil, func(s string) { logged = append(logged, s) })
	if err == nil {
		t.Fatal("patchJob patched a file with no nav image in it")
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("out still exists after a failed patch: %v", statErr)
	}
	if _, statErr := os.Stat(in); statErr != nil {
		t.Errorf("in was touched: %v", statErr)
	}
}

func TestPatchJobRefusesWithNothingSelected(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.img")
	os.WriteFile(in, []byte("x"), 0o644)
	if err := patchJob(in, filepath.Join(dir, "out.img"), nil, nil, func(string) {}); err == nil {
		t.Fatal("patchJob ran with no row selected")
	}
}
