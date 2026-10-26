//go:build windows

package disk

import (
	"encoding/binary"
	"testing"
)

func TestClassifyBusType(t *testing.T) {
	cases := []struct {
		name           string
		busType        byte
		removableMedia bool
		wantInternal   bool
		wantRemovable  bool
	}{
		{"USB card reader", busTypeUSB, false, false, true},
		{"SD slot", busTypeSD, false, false, true},
		{"MMC slot", busTypeMMC, false, false, true},
		{"internal SATA disk", 0x0B, false, true, false},
		{"internal disk flagged removable anyway", 0x0B, true, true, true},
	}
	for _, c := range cases {
		gotInternal, gotRemovable := classifyBusType(c.busType, c.removableMedia)
		if gotInternal != c.wantInternal || gotRemovable != c.wantRemovable {
			t.Errorf("%s: classifyBusType(%#x, %v) = (%v, %v), want (%v, %v)",
				c.name, c.busType, c.removableMedia, gotInternal, gotRemovable, c.wantInternal, c.wantRemovable)
		}
	}
}

func buildDescriptor(busType byte, removableMedia bool, vendor, product string) []byte {
	vendorOff := uint32(36)
	productOff := vendorOff + uint32(len(vendor)) + 1

	buf := make([]byte, productOff+uint32(len(product))+1)
	if removableMedia {
		buf[10] = 1
	}
	buf[28] = busType
	binary.LittleEndian.PutUint32(buf[12:16], vendorOff)
	binary.LittleEndian.PutUint32(buf[16:20], productOff)
	copy(buf[vendorOff:], vendor)
	copy(buf[productOff:], product)
	return buf
}

func TestParseStorageDeviceDescriptor(t *testing.T) {
	buf := buildDescriptor(busTypeUSB, true, "Generic", "SD Card Reader")
	busType, removable, name, err := parseStorageDeviceDescriptor(buf)
	if err != nil {
		t.Fatalf("parseStorageDeviceDescriptor: %v", err)
	}
	if busType != busTypeUSB {
		t.Errorf("busType = %#x, want %#x", busType, busTypeUSB)
	}
	if !removable {
		t.Error("removableMedia did not come through as true")
	}
	if name != "Generic SD Card Reader" {
		t.Errorf("name = %q, want %q", name, "Generic SD Card Reader")
	}
}

func TestParseStorageDeviceDescriptorRejectsAShortBuffer(t *testing.T) {
	if _, _, _, err := parseStorageDeviceDescriptor(make([]byte, 10)); err == nil {
		t.Error("expected an error for a too-short descriptor")
	}
}

func buildVolumeDiskExtents(disks ...uint32) []byte {
	const extentsOffset = 8
	const extentSize = 24
	buf := make([]byte, extentsOffset+extentSize*len(disks))
	binary.LittleEndian.PutUint32(buf[0:4], uint32(len(disks)))
	for i, d := range disks {
		start := extentsOffset + i*extentSize
		binary.LittleEndian.PutUint32(buf[start:start+4], d)
	}
	return buf
}

func TestParseVolumeDiskExtents(t *testing.T) {
	buf := buildVolumeDiskExtents(2)
	got, err := parseVolumeDiskExtents(buf)
	if err != nil {
		t.Fatalf("parseVolumeDiskExtents: %v", err)
	}
	if len(got) != 1 || got[0] != 2 {
		t.Errorf("got %v, want [2]", got)
	}
}

func TestParseVolumeDiskExtentsSpanningTwoDisks(t *testing.T) {
	buf := buildVolumeDiskExtents(0, 1)
	got, err := parseVolumeDiskExtents(buf)
	if err != nil {
		t.Fatalf("parseVolumeDiskExtents: %v", err)
	}
	if len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Errorf("got %v, want [0 1]", got)
	}
}

func TestParseVolumeDiskExtentsRejectsATruncatedBuffer(t *testing.T) {
	buf := buildVolumeDiskExtents(2, 3)
	if _, err := parseVolumeDiskExtents(buf[:10]); err == nil {
		t.Error("expected an error for a truncated extents list")
	}
}

func TestContainsDisk(t *testing.T) {
	disks := []uint32{0, 2, 5}
	if !containsDisk(disks, 2) {
		t.Error("containsDisk(disks, 2) = false, want true")
	}
	if containsDisk(disks, 3) {
		t.Error("containsDisk(disks, 3) = true, want false")
	}
}

func TestDiskNumberFromIdentifier(t *testing.T) {
	n, err := diskNumberFromIdentifier("PhysicalDrive3")
	if err != nil {
		t.Fatalf("diskNumberFromIdentifier: %v", err)
	}
	if n != 3 {
		t.Errorf("got %d, want 3", n)
	}
}

func TestDiskNumberFromIdentifierRejectsGarbage(t *testing.T) {
	if _, err := diskNumberFromIdentifier("not-a-drive"); err == nil {
		t.Error("expected an error for a malformed identifier")
	}
}
