package disk

import "testing"

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
