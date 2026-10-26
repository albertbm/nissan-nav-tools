package trim

import (
	"bytes"
	"testing"
)

func putLE32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

// mbrSector builds a 512-byte MBR-style sector, optionally with the Clarion signature, and one partition entry.
func mbrSector(clarion bool, lba, count uint32) []byte {
	s := make([]byte, 512)
	if clarion {
		copy(s, []byte("CLARION ID"))
	}
	e := s[0x1be : 0x1be+16]
	e[4] = 0x07 // any non-zero type
	putLE32(e[8:12], lba)
	putLE32(e[12:16], count)
	s[510], s[511] = 0x55, 0xaa
	return s
}

func image(sector0, sector1 []byte) *bytes.Reader {
	buf := make([]byte, 1024)
	copy(buf, sector0)
	copy(buf[512:], sector1)
	return bytes.NewReader(buf)
}

func TestSize_Sector1Clarion(t *testing.T) {
	sector0 := make([]byte, 512) // no signature, must be ignored
	sector1 := mbrSector(true, 2048, 31254528)
	got, err := Size(image(sector0, sector1))
	if err != nil {
		t.Fatal(err)
	}
	if got != StockSize {
		t.Errorf("got %d, want %d", got, StockSize)
	}
}

func TestSize_Sector0Only(t *testing.T) {
	sector0 := mbrSector(false, 2048, 31254528)
	sector1 := make([]byte, 512)
	got, err := Size(image(sector0, sector1))
	if err != nil {
		t.Fatal(err)
	}
	if got != StockSize {
		t.Errorf("got %d, want %d", got, StockSize)
	}
}

func TestSize_BriefExample(t *testing.T) {
	const end = 16_013_853_184
	sector0 := make([]byte, 512)
	sector1 := mbrSector(true, 0, uint32(end/sectorSize))
	got, err := Size(image(sector0, sector1))
	if err != nil {
		t.Fatal(err)
	}
	if got != StockSize {
		t.Errorf("got %d, want %d (stock pad)", got, StockSize)
	}
}

func TestSize_BeyondStockRoundsUpToMiB(t *testing.T) {
	sector0 := make([]byte, 512)
	sector1 := mbrSector(true, 0, 0)
	const end = 17_000_000_123 // bytes, not a multiple of 512 or 1 MiB
	lba := uint32(0)
	count := uint32(end / sectorSize) // truncated, so end-of-partition in bytes is <= end
	putLE32(sector1[0x1be+8:0x1be+12], lba)
	putLE32(sector1[0x1be+12:0x1be+16], count)

	got, err := Size(image(sector0, sector1))
	if err != nil {
		t.Fatal(err)
	}
	wantEnd := int64(count) * sectorSize
	wantRounded := roundUp(wantEnd, mib)
	if got != wantRounded {
		t.Errorf("got %d, want %d", got, wantRounded)
	}
	if got < wantEnd {
		t.Errorf("trimmed to %d, below the partition's own end %d", got, wantEnd)
	}
	if got%mib != 0 {
		t.Errorf("%d is not 1 MiB aligned", got)
	}
}

func TestSize_Unparsable(t *testing.T) {
	sector0 := make([]byte, 512)
	sector1 := make([]byte, 512)
	if _, err := Size(image(sector0, sector1)); err == nil {
		t.Error("want an error for an unparsable table, got nil")
	}
}

func TestSize_ClarionSignatureButNoPartitions(t *testing.T) {
	sector0 := make([]byte, 512)
	sector1 := make([]byte, 512)
	copy(sector1, []byte("CLARION ID"))
	sector1[510], sector1[511] = 0x55, 0xaa // signature but every entry is zero
	if _, err := Size(image(sector0, sector1)); err == nil {
		t.Error("want an error when sector 1 has the signature but no partitions, got nil")
	}
}
