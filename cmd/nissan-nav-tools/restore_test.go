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
	eject := func() error { calls = append(calls, "eject"); return nil }

	if err := restoreSequence(write, verify, true, eject); err != nil {
		t.Fatal(err)
	}
	want := []string{"write", "verify", "eject"}
	if len(calls) != len(want) {
		t.Fatalf("got %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("got %v, want %v", calls, want)
		}
	}
}

func TestRestoreSequenceSkipsVerify(t *testing.T) {
	var calls []string
	write := func() error { calls = append(calls, "write"); return nil }
	verify := func() error { calls = append(calls, "verify"); return nil }
	eject := func() error { calls = append(calls, "eject"); return nil }

	if err := restoreSequence(write, verify, false, eject); err != nil {
		t.Fatal(err)
	}
	want := []string{"write", "eject"}
	if len(calls) != len(want) || calls[0] != want[0] || calls[1] != want[1] {
		t.Fatalf("got %v, want %v", calls, want)
	}
}

func TestRestoreSequenceStopsOnVerifyFailure(t *testing.T) {
	var ejected bool
	write := func() error { return nil }
	verify := func() error { return fmt.Errorf("sha256 mismatch") }
	eject := func() error { ejected = true; return nil }

	if err := restoreSequence(write, verify, true, eject); err == nil {
		t.Fatal("restoreSequence didn't report the verify failure")
	}
	if ejected {
		t.Error("ejected after a failed verify; the card should stay put")
	}
}

func TestRestoreSequenceStopsOnWriteFailure(t *testing.T) {
	var verified, ejected bool
	write := func() error { return fmt.Errorf("short write") }
	verify := func() error { verified = true; return nil }
	eject := func() error { ejected = true; return nil }

	if err := restoreSequence(write, verify, true, eject); err == nil {
		t.Fatal("restoreSequence didn't report the write failure")
	}
	if verified || ejected {
		t.Error("ran verify or eject after a failed write")
	}
}
