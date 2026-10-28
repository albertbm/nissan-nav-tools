package disk

import (
	"bytes"
	"fmt"
	"io"
	"testing"
)

// unalignedRejecter stands in for a raw macOS device: anything not a multiple of
// sectorBytes, at a sectorBytes-aligned offset, is refused outright.
type unalignedRejecter struct{ data []byte }

func (d *unalignedRejecter) checkAligned(off int64, n int) error {
	if off%sectorBytes != 0 || n%sectorBytes != 0 {
		return fmt.Errorf("unaligned I/O at 0x%x, %d bytes", off, n)
	}
	return nil
}

func (d *unalignedRejecter) ReadAt(p []byte, off int64) (int, error) {
	if err := d.checkAligned(off, len(p)); err != nil {
		return 0, err
	}
	if off >= int64(len(d.data)) {
		return 0, io.EOF
	}
	n := copy(p, d.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (d *unalignedRejecter) WriteAt(p []byte, off int64) (int, error) {
	if err := d.checkAligned(off, len(p)); err != nil {
		return 0, err
	}
	return copy(d.data[off:], p), nil
}

func TestAlignedReadWriterAtReadsUnalignedRequests(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789"), sectorBytes)
	dev := &unalignedRejecter{data: append([]byte(nil), data...)}
	a := NewAlignedReadWriterAt(dev)

	got := make([]byte, 37)
	if _, err := a.ReadAt(got, 517); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if !bytes.Equal(got, data[517:517+37]) {
		t.Error("unaligned read returned the wrong bytes")
	}
}

func TestAlignedReadWriterAtMergesUnalignedWrites(t *testing.T) {
	data := make([]byte, sectorBytes*3)
	dev := &unalignedRejecter{data: data}
	a := NewAlignedReadWriterAt(dev)

	payload := bytes.Repeat([]byte{0xAB}, 40)
	if _, err := a.WriteAt(payload, 500); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if !bytes.Equal(dev.data[500:540], payload) {
		t.Error("write landed at the wrong bytes")
	}
	for i, b := range dev.data[:500] {
		if b != 0 {
			t.Fatalf("byte %d before the write was disturbed", i)
		}
	}
	for i, b := range dev.data[540:] {
		if b != 0 {
			t.Fatalf("byte %d after the write was disturbed", 540+i)
		}
	}
}

func TestAlignedReadWriterAtWriteThenReadSeesNewBytes(t *testing.T) {
	dev := &unalignedRejecter{data: make([]byte, sectorBytes*2)}
	a := NewAlignedReadWriterAt(dev)

	// prime the cache, then write through the same offset, as Reset's RMW edge does
	warm := make([]byte, 10)
	a.ReadAt(warm, 5)

	payload := []byte{1, 2, 3}
	if _, err := a.WriteAt(payload, 5); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	got := make([]byte, 3)
	if _, err := a.ReadAt(got, 5); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("read after write returned %v, want %v (stale cache?)", got, payload)
	}
}

func TestAlignedReadWriterAtVerifyCatchesAMismatch(t *testing.T) {
	dev := &unalignedRejecter{data: make([]byte, sectorBytes*2)}
	a := NewAlignedReadWriterAt(dev)

	if _, err := a.WriteAt([]byte{1, 2, 3}, 10); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if err := a.Verify(); err != nil {
		t.Fatalf("Verify on an untouched write: %v", err)
	}

	// something rewrites the device after the fact, as a remount might
	dev.data[10] = 0xFF
	if err := a.Verify(); err == nil {
		t.Error("Verify missed a byte changed after the write")
	}
}
