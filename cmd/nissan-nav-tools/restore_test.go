package main

import (
	"fmt"
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

func TestRestoreSequenceOrder(t *testing.T) {
	var calls []string
	write := func() error { calls = append(calls, "write"); return nil }
	verify := func() error { calls = append(calls, "verify"); return nil }

	if err := restoreSequence(write, verify, true); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0] != "write" || calls[1] != "verify" {
		t.Fatalf("got %v, want [write verify]", calls)
	}
}

func TestRestoreSequenceSkipsVerify(t *testing.T) {
	var calls []string
	write := func() error { calls = append(calls, "write"); return nil }
	verify := func() error { calls = append(calls, "verify"); return nil }

	if err := restoreSequence(write, verify, false); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "write" {
		t.Fatalf("got %v, want [write]", calls)
	}
}

func TestRestoreSequenceReportsVerifyFailure(t *testing.T) {
	write := func() error { return nil }
	verify := func() error { return fmt.Errorf("sha256 mismatch") }

	if err := restoreSequence(write, verify, true); err == nil {
		t.Fatal("restoreSequence didn't report the verify failure")
	}
}

func TestRestoreSequenceStopsOnWriteFailure(t *testing.T) {
	var verified bool
	write := func() error { return fmt.Errorf("short write") }
	verify := func() error { verified = true; return nil }

	if err := restoreSequence(write, verify, true); err == nil {
		t.Fatal("restoreSequence didn't report the write failure")
	}
	if verified {
		t.Error("ran verify after a failed write")
	}
}
