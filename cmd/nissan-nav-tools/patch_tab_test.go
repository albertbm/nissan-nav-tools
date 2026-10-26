package main

import (
	"testing"

	"nissan-nav-tools/internal/disk"
)

func TestPatchTargetFor(t *testing.T) {
	tests := []struct {
		label string
		want  patchTarget
	}{
		{targetCopyLabel, targetCopy},
		{targetFileLabel, targetFile},
		{targetCardLabel, targetCard},
		{"", targetCopy},
		{"anything else", targetCopy},
	}
	for _, tt := range tests {
		if got := patchTargetFor(tt.label); got != tt.want {
			t.Errorf("patchTargetFor(%q) = %v, want %v", tt.label, got, tt.want)
		}
	}
}

func TestRefuseUnlessCardRejectsInternal(t *testing.T) {
	d := disk.Info{Identifier: "disk0", Internal: true, Removable: true}
	if err := refuseUnlessCard(d); err == nil {
		t.Error("accepted an internal disk")
	}
}

func TestRefuseUnlessCardRejectsNonRemovable(t *testing.T) {
	d := disk.Info{Identifier: "disk4", Internal: false, Removable: false}
	if err := refuseUnlessCard(d); err == nil {
		t.Error("accepted media that is not removable")
	}
}

func TestRefuseUnlessCardAcceptsACardReader(t *testing.T) {
	d := disk.Info{Identifier: "disk5", Internal: false, Removable: true}
	if err := refuseUnlessCard(d); err != nil {
		t.Errorf("rejected an external, removable disk: %v", err)
	}
}
