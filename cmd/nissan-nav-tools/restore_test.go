package main

import (
	"testing"

	"nissan-nav-tools/internal/disk"
)

func TestRefuseIfTooSmallRejectsASmallCard(t *testing.T) {
	d := disk.Info{Identifier: "disk5", Size: 1000}
	if err := refuseIfTooSmall(d, 2000); err == nil {
		t.Error("accepted a card smaller than the image")
	}
}

func TestRefuseIfTooSmallAcceptsARoomyCard(t *testing.T) {
	d := disk.Info{Identifier: "disk5", Size: 2000}
	if err := refuseIfTooSmall(d, 2000); err != nil {
		t.Errorf("rejected a card the same size as the image: %v", err)
	}
	if err := refuseIfTooSmall(d, 1000); err != nil {
		t.Errorf("rejected a card bigger than the image: %v", err)
	}
}

// Mirrors patch_tab_test.go so the CLI can't drift from the GUI.
func TestRestoreCLIRefusesInternalAndNonRemovable(t *testing.T) {
	cases := []disk.Info{
		{Identifier: "disk0", Internal: true, Removable: true},
		{Identifier: "disk4", Internal: false, Removable: false},
	}
	for _, d := range cases {
		if err := refuseUnlessCard(d); err == nil {
			t.Errorf("%+v: accepted a disk restore should refuse", d)
		}
	}
}
