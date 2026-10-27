package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareTargetRefusesSamePath(t *testing.T) {
	d := t.TempDir()
	in := filepath.Join(d, "x.img")
	os.WriteFile(in, []byte("card"), 0o644)
	if _, err := prepareTarget(in, in, ".anon", false, true); err == nil {
		t.Fatal("prepareTarget let -out name -in")
	}
	if _, statErr := os.Stat(in); statErr != nil {
		t.Errorf("in was touched: %v", statErr)
	}
}

func TestPrepareTargetRefusesRespelledPath(t *testing.T) {
	d := t.TempDir()
	in := filepath.Join(d, "x.img")
	os.WriteFile(in, []byte("card"), 0o644)
	out := filepath.Join(d, "sub", "..", "x.img")
	if _, err := prepareTarget(in, out, ".anon", false, true); err == nil {
		t.Fatal("prepareTarget let a re-spelled -out name -in")
	}
	if _, statErr := os.Stat(in); statErr != nil {
		t.Errorf("in was touched: %v", statErr)
	}
}

func TestPrepareTargetRefusesSymlinkedOut(t *testing.T) {
	d := t.TempDir()
	in := filepath.Join(d, "x.img")
	os.WriteFile(in, []byte("card"), 0o644)
	out := filepath.Join(d, "link.img")
	if err := os.Symlink(in, out); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareTarget(in, out, ".anon", false, true); err == nil {
		t.Fatal("prepareTarget let a symlinked -out name -in")
	}
	if _, statErr := os.Stat(in); statErr != nil {
		t.Errorf("in was touched: %v", statErr)
	}
}

func TestPrepareTargetRefusesHardlinkedOut(t *testing.T) {
	d := t.TempDir()
	in := filepath.Join(d, "x.img")
	os.WriteFile(in, []byte("card"), 0o644)
	out := filepath.Join(d, "link.img")
	if err := os.Link(in, out); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareTarget(in, out, ".anon", false, true); err == nil {
		t.Fatal("prepareTarget let a hard-linked -out name -in")
	}
	if _, statErr := os.Stat(in); statErr != nil {
		t.Errorf("in was touched: %v", statErr)
	}
}

func TestPrepareTargetAllowsADifferentFile(t *testing.T) {
	d := t.TempDir()
	in := filepath.Join(d, "x.img")
	os.WriteFile(in, []byte("card"), 0o644)
	out, err := prepareTarget(in, filepath.Join(d, "y.img"), ".anon", false, true)
	if err != nil {
		t.Fatalf("prepareTarget refused a genuinely different -out: %v", err)
	}
	if _, statErr := os.Stat(out); statErr != nil {
		t.Errorf("copy wasn't written: %v", statErr)
	}
}

func TestAnonymizeCopyRefusesSameFile(t *testing.T) {
	d := t.TempDir()
	in := filepath.Join(d, "x.img")
	os.WriteFile(in, []byte("card"), 0o644)
	if err := anonymizeCopy(in, in, func(string) {}); err == nil {
		t.Fatal("anonymizeCopy let -out name -in")
	}
	if got, statErr := os.ReadFile(in); statErr != nil || string(got) != "card" {
		t.Errorf("in was touched: %q, %v", got, statErr)
	}
}

func TestBackupCopyRefusesSameFile(t *testing.T) {
	d := t.TempDir()
	in := filepath.Join(d, "x.img")
	os.WriteFile(in, []byte("card"), 0o644)
	if err := backupCopy(in, in, func(string) {}); err == nil {
		t.Fatal("backupCopy let the destination name the source")
	}
	if got, statErr := os.ReadFile(in); statErr != nil || string(got) != "card" {
		t.Errorf("in was touched: %q, %v", got, statErr)
	}
}

func TestPatchJobRefusesSameFile(t *testing.T) {
	d := t.TempDir()
	in := filepath.Join(d, "x.img")
	os.WriteFile(in, []byte("card"), 0o644)
	if err := patchJob(in, in, []int{0}, nil, func(string) {}); err == nil {
		t.Fatal("patchJob let -out name -in")
	}
	if got, statErr := os.ReadFile(in); statErr != nil || string(got) != "card" {
		t.Errorf("in was touched: %q, %v", got, statErr)
	}
}

func putLE32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func clarionSector1(lba, count uint32) []byte {
	s := make([]byte, 512)
	copy(s, []byte("CLARION ID"))
	e := s[0x1be : 0x1be+16]
	e[4] = 0x07
	putLE32(e[8:12], lba)
	putLE32(e[12:16], count)
	s[510], s[511] = 0x55, 0xaa
	return s
}

// The image must be bigger than trim.StockSize to have anything to trim; the body stays sparse so the test is instant.
func writeTrimmableImage(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteAt(clarionSector1(2048, 31254528), 512); err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(16_039_018_496 + 4<<20); err != nil {
		t.Fatal(err)
	}
}

func TestTrimCLIRefusesSameFile(t *testing.T) {
	d := t.TempDir()
	in := filepath.Join(d, "x.img")
	writeTrimmableImage(t, in)
	if code := runTrimCLI([]string{"-in", in, "-out", in, "-force"}); code == 0 {
		t.Fatal("trim let -out name -in")
	}
	fi, statErr := os.Stat(in)
	if statErr != nil {
		t.Fatalf("in was touched: %v", statErr)
	}
	if fi.Size() != 16_039_018_496+4<<20 {
		t.Errorf("in was truncated: size is now %d", fi.Size())
	}
}

func TestRunAnonymizeCLICleansUpOnFailure(t *testing.T) {
	d := t.TempDir()
	in := filepath.Join(d, "x.img")
	os.WriteFile(in, []byte("not a clarion image"), 0o644)
	out := filepath.Join(d, "x.anon.img")

	if code := runAnonymizeCLI([]string{"-in", in, "-out", out}); code == 0 {
		t.Fatal("anonymize succeeded on a non-Clarion file")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("half-made output was left behind: %v", err)
	}
}

func TestRunConsentCLICleansUpOnFailure(t *testing.T) {
	d := t.TempDir()
	in := filepath.Join(d, "x.img")
	os.WriteFile(in, []byte("not a nav image"), 0o644)
	out := filepath.Join(d, "x.consent.img")

	if code := runConsentCLI([]string{"-in", in, "-out", out}); code == 0 {
		t.Fatal("consent patch succeeded on a non-nav file")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("half-made output was left behind: %v", err)
	}
}
