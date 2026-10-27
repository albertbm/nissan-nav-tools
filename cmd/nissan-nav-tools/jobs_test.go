package main

import (
	"os"
	"path/filepath"
	"testing"
)

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
