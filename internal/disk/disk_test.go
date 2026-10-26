package disk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestDDWriteScriptArgs(t *testing.T) {
	d := Info{Identifier: "disk5"}
	script := ddWriteScript("/tmp/card.img", d.Node(), "/tmp/progress")
	for _, want := range []string{"if='/tmp/card.img'", "of='/dev/rdisk5'", "bs=16m"} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q:\n%s", want, script)
		}
	}
}

func TestLastDDWrittenParsesSIGINFOOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "progress")
	log := "12+0 records in\n12+0 records out\n201326592 bytes transferred in 3.1 secs (64942449 bytes/sec)\n"
	if err := os.WriteFile(path, []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	n, ok := lastDDWritten(path)
	if !ok || n != 201326592 {
		t.Errorf("got %d, %v, want 201326592, true", n, ok)
	}
}

func TestLastDDWrittenNoDataYet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "progress")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := lastDDWritten(path); ok {
		t.Error("reported progress before dd printed any")
	}
}

func TestIsCard(t *testing.T) {
	cases := []struct {
		d    Info
		want bool
	}{
		{Info{Removable: true, Size: 16_039_018_496}, true},
		{Info{Removable: true, Size: 64_000_000_000}, true},
		{Info{Removable: false, Size: 16_039_018_496}, false},
		{Info{Removable: true, Size: 1_000_000_000_000}, false},
		{Info{Internal: true, Removable: true, Size: 16_039_018_496}, false},
		{Info{Removable: true}, false},
	}
	for _, c := range cases {
		if got := isCard(c.d); got != c.want {
			t.Errorf("isCard(%+v) = %v, want %v", c.d, got, c.want)
		}
	}
}
