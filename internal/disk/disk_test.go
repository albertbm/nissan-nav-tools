package disk

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestPadToSectorLeavesAlignedDataAlone(t *testing.T) {
	buf := make([]byte, sectorBytes*3)
	if got := padToSector(buf); len(got) != len(buf) {
		t.Errorf("padded an already-aligned chunk to %d bytes, want %d", len(got), len(buf))
	}
}

func TestPadToSectorRoundsUpTheShortChunk(t *testing.T) {
	buf := []byte("short")
	got := padToSector(buf)
	if len(got) != sectorBytes {
		t.Fatalf("got %d bytes, want %d", len(got), sectorBytes)
	}
	if string(got[:len(buf)]) != "short" {
		t.Error("padding clobbered the original data")
	}
	for _, b := range got[len(buf):] {
		if b != 0 {
			t.Error("padding was not zero-filled")
		}
	}
}

func TestWriteRangesRejectsAnUnalignedOffset(t *testing.T) {
	d := Info{Identifier: "disk99", Removable: true}
	err := WriteRanges(d, []Range{{Offset: 1, Data: make([]byte, sectorBytes)}})
	if err == nil {
		t.Fatal("expected an alignment error")
	}
}

func TestWriteRangesRejectsAnUnalignedLength(t *testing.T) {
	d := Info{Identifier: "disk99", Removable: true}
	err := WriteRanges(d, []Range{{Offset: 0, Data: make([]byte, sectorBytes-1)}})
	if err == nil {
		t.Fatal("expected an alignment error")
	}
}

func TestReadRangesRejectsAnUnalignedOffset(t *testing.T) {
	d := Info{Identifier: "disk99", Removable: true}
	_, err := ReadRanges(d, []Range{{Offset: 1, Data: make([]byte, sectorBytes)}})
	if err == nil {
		t.Fatal("expected an alignment error")
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

func TestHashStreamMatchesSHA256(t *testing.T) {
	data := bytes.Repeat([]byte("card"), 10000)
	want := sha256.Sum256(data)

	var progressed int64
	got, err := hashStream(bytes.NewReader(data), int64(len(data)), func(n int64) { progressed = n })
	if err != nil {
		t.Fatal(err)
	}
	if got != hex.EncodeToString(want[:]) {
		t.Errorf("got %s, want %s", got, hex.EncodeToString(want[:]))
	}
	if progressed != int64(len(data)) {
		t.Errorf("progress reported %d, want %d", progressed, len(data))
	}
}

func TestHashStreamStopsAtN(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 100)
	want := sha256.Sum256(data[:40])

	got, err := hashStream(bytes.NewReader(data), 40, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != hex.EncodeToString(want[:]) {
		t.Errorf("got %s, want the hash of the first 40 bytes only", got)
	}
}

func TestCheckStillThereAcceptsAnUnchangedDisk(t *testing.T) {
	picked := Info{Identifier: "disk4", Name: "NISSAN", Size: 16_039_018_496, Removable: true}
	now := []Info{picked}
	if err := checkStillThere(picked, now); err != nil {
		t.Errorf("rejected an unchanged disk: %v", err)
	}
}

func TestCheckStillThereRejectsAVanishedDisk(t *testing.T) {
	picked := Info{Identifier: "disk4", Name: "NISSAN", Size: 16_039_018_496, Removable: true}
	if err := checkStillThere(picked, nil); err == nil {
		t.Fatal("accepted a write with the disk no longer attached")
	}
}

// A file standing in for the card: WriteToHandle/HashHandle must verify through
// the handle that did the write, not through a fresh open, since on a real card a
// remount can mutate the disk right after that handle closes.
func TestHashHandleVerifiesBeforeAnyPostCloseMutation(t *testing.T) {
	data := bytes.Repeat([]byte("NISSANCARD"), sectorBytes) // 5120 bytes, an exact multiple of sectorBytes
	wantHash := hex.EncodeToString(sha256Sum(data))

	path := filepath.Join(t.TempDir(), "card.img")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteToHandle(f, bytes.NewReader(data), nil); err != nil {
		t.Fatal(err)
	}

	// verify through the same still-open handle, as restoreJob/verifyRestore now do
	gotHash, err := HashHandle(f, int64(len(data)), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	// the "remount" hook: something rewrites the card only after the handle closes
	if err := os.WriteFile(path, bytes.Repeat([]byte{0xff}, 16), 0o644); err != nil {
		t.Fatal(err)
	}

	if gotHash != wantHash {
		t.Fatalf("verify through the open handle got %s, want %s; it must not see what happens after close", gotHash, wantHash)
	}

	// prove the mutation really would have broken a verify that reopened the file instead
	reopened, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	afterHash, _ := hashStream(reopened, int64(len(data)), nil)
	if afterHash == wantHash {
		t.Fatal("the remount hook left the on-disk bytes unchanged; this test proves nothing")
	}
}

func sha256Sum(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}

func TestCheckStillThereRejectsADifferentCardOnTheSameIdentifier(t *testing.T) {
	picked := Info{Identifier: "disk4", Name: "NISSAN", Size: 16_039_018_496, Removable: true}
	cases := []Info{
		{Identifier: "disk4", Name: "SANDISK", Size: 16_039_018_496, Removable: true},
		{Identifier: "disk4", Name: "NISSAN", Size: 64_000_000_000, Removable: true},
		{Identifier: "disk4", Name: "NISSAN", Size: 16_039_018_496, Removable: false},
	}
	for _, now := range cases {
		if err := checkStillThere(picked, []Info{now}); err == nil {
			t.Errorf("accepted %+v in place of %+v", now, picked)
		}
	}
}
