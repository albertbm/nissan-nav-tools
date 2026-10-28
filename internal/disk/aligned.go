package disk

import (
	"crypto/sha256"
	"fmt"
	"io"
)

type alignedTarget interface {
	io.ReaderAt
	io.WriterAt
}

type writtenRange struct {
	offset int64
	size   int64
	sum    [32]byte
}

// AlignedReadWriterAt lets code call ReadAt/WriteAt at any offset and length against a
// raw device that only accepts sectorBytes-aligned I/O: reads go through a BufferedReaderAt,
// and a write that doesn't already land on a sector boundary is merged into one that does
// by reading its edges first. Every write actually sent to the device is recorded, so
// Verify can read each one back and confirm it took, without trusting the cache.
type AlignedReadWriterAt struct {
	dev     alignedTarget
	r       *BufferedReaderAt
	written []writtenRange
}

func NewAlignedReadWriterAt(dev alignedTarget) *AlignedReadWriterAt {
	return &AlignedReadWriterAt{dev: dev, r: NewBufferedReaderAt(dev)}
}

func (a *AlignedReadWriterAt) ReadAt(p []byte, off int64) (int, error) {
	return a.r.ReadAt(p, off)
}

func alignDown(off int64) int64 { return off - off%sectorBytes }

func alignUp(off int64) int64 {
	if r := off % sectorBytes; r != 0 {
		return off + (sectorBytes - r)
	}
	return off
}

func (a *AlignedReadWriterAt) WriteAt(p []byte, off int64) (int, error) {
	lo := alignDown(off)
	hi := alignUp(off + int64(len(p)))

	buf := p
	if lo != off || hi != off+int64(len(p)) {
		buf = make([]byte, hi-lo)
		if _, err := a.r.ReadAt(buf, lo); err != nil && err != io.EOF {
			return 0, fmt.Errorf("reading the sector to merge an unaligned write at 0x%x: %w", off, err)
		}
		copy(buf[off-lo:], p)
	}
	if _, err := a.dev.WriteAt(buf, lo); err != nil {
		return 0, err
	}
	a.r.invalidate()
	a.written = append(a.written, writtenRange{offset: lo, size: int64(len(buf)), sum: sha256.Sum256(buf)})
	return len(p), nil
}

// Verify re-reads every range WriteAt has sent to the device, through the device directly
// rather than the cache, and confirms each one still hashes the same.
func (a *AlignedReadWriterAt) Verify() error {
	var buf []byte
	for _, wr := range a.written {
		if int64(len(buf)) < wr.size {
			buf = make([]byte, wr.size)
		}
		if _, err := a.dev.ReadAt(buf[:wr.size], wr.offset); err != nil {
			return fmt.Errorf("reading back 0x%x: %w", wr.offset, err)
		}
		if sha256.Sum256(buf[:wr.size]) != wr.sum {
			return fmt.Errorf("read-back mismatch at 0x%x", wr.offset)
		}
	}
	return nil
}
