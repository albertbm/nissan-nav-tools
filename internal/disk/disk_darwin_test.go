//go:build darwin

package disk

import (
	"io"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// Captured from `diskutil list -plist external physical | plutil -convert json -o - -`
// on a Mac with one external SSD (disk4) and no card reader attached.
const sampleListJSON = `{"AllDisksAndPartitions":[{"Partitions":[{"Size":209715200,"VolumeUUID":"0E239BC6-F960-3107-89CF-1C97F78BB46B","VolumeName":"EFI","Content":"EFI","DeviceIdentifier":"disk4s1","DiskUUID":"5D53BC6A-0DED-42C0-A133-74B2D7CCDCBF"},{"Size":511900434432,"Content":"Apple_APFS","DeviceIdentifier":"disk4s2","DiskUUID":"1B32BC89-280B-4C59-B9C1-F21BAEC6F045"}],"Size":512110190592,"Content":"GUID_partition_scheme","DeviceIdentifier":"disk4","OSInternal":false}],"VolumesFromDisks":[],"AllDisks":["disk4","disk4s1","disk4s2"],"WholeDisks":["disk4"]}`

// Captured from `diskutil info -plist disk4 | plutil -convert json -o - -` on the same disk.
const sampleInfoJSON = `{"DeviceIdentifier":"disk4","Internal":false,"RemovableMedia":false,"Ejectable":true,"WholeDisk":true,"MediaName":"SP PD60","Size":512110190592,"VirtualOrPhysical":"Physical"}`

const sampleEmptyListJSON = `{"AllDisksAndPartitions":[],"VolumesFromDisks":[],"AllDisks":[],"WholeDisks":[]}`

func TestParseWholeDisks(t *testing.T) {
	ids, err := parseWholeDisks([]byte(sampleListJSON))
	if err != nil {
		t.Fatalf("parseWholeDisks: %v", err)
	}
	if len(ids) != 1 || ids[0] != "disk4" {
		t.Errorf("got %v, want [disk4]", ids)
	}
}

func TestParseWholeDisksEmpty(t *testing.T) {
	ids, err := parseWholeDisks([]byte(sampleEmptyListJSON))
	if err != nil {
		t.Fatalf("parseWholeDisks: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("got %v, want none", ids)
	}
}

func TestParseInfo(t *testing.T) {
	info, err := parseInfo([]byte(sampleInfoJSON))
	if err != nil {
		t.Fatalf("parseInfo: %v", err)
	}
	want := Info{Identifier: "disk4", Name: "SP PD60", Size: 512110190592, Internal: false}
	if info != want {
		t.Errorf("got %+v, want %+v", info, want)
	}
	if info.Node() != "/dev/rdisk4" {
		t.Errorf("Node() = %q, want /dev/rdisk4", info.Node())
	}
}

func TestParseInfoReadsRemovableMedia(t *testing.T) {
	info, err := parseInfo([]byte(`{"DeviceIdentifier":"disk5","Internal":false,"RemovableMedia":true,"Size":64000000000}`))
	if err != nil {
		t.Fatalf("parseInfo: %v", err)
	}
	if !info.Removable {
		t.Error("RemovableMedia did not come through as true")
	}
}

func TestParseInfoRefusesAnInternalDisk(t *testing.T) {
	info, err := parseInfo([]byte(`{"DeviceIdentifier":"disk0","Internal":true,"Size":1}`))
	if err != nil {
		t.Fatalf("parseInfo: %v", err)
	}
	if !info.Internal {
		t.Error("Internal did not come through as true")
	}
}

func TestParseWholeDisksRejectsGarbage(t *testing.T) {
	if _, err := parseWholeDisks([]byte("not json")); err == nil {
		t.Error("parseWholeDisks accepted garbage")
	}
}

func TestRecvFDReceivesThePassedDescriptor(t *testing.T) {
	want := []byte("stands in for a card's raw device fd")
	tmp, err := os.CreateTemp(t.TempDir(), "fd-pass-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tmp.Write(want); err != nil {
		t.Fatal(err)
	}
	tmp.Close()

	passed, err := os.Open(tmp.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer passed.Close()

	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	defer unix.Close(fds[0])
	defer unix.Close(fds[1])

	rights := unix.UnixRights(int(passed.Fd()))
	if err := unix.Sendmsg(fds[1], []byte("x"), rights, nil, 0); err != nil {
		t.Fatalf("sendmsg: %v", err)
	}

	got, err := recvFD(fds[0], "received")
	if err != nil {
		t.Fatalf("recvFD: %v", err)
	}
	defer got.Close()

	data, err := io.ReadAll(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(want) {
		t.Errorf("got %q, want %q", data, want)
	}
}
