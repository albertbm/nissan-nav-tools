// Package disk lists removable storage and reads it raw.
package disk

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

type Info struct {
	Identifier string
	Name       string
	Size       int64
	Internal   bool
	Removable  bool
}

const sectorBytes = 512
const chunkSize = 4 << 20

// Head unit cards top out at 64 GB; this keeps external drives off the list.
const maxCardSize = 130_000_000_000

type Range struct {
	Offset int64
	Data   []byte
}

func (d Info) Node() string { return platformNode(d) }

func isCard(d Info) bool {
	return !d.Internal && d.Removable && d.Size > 0 && d.Size <= maxCardSize
}

func refuseUnlessCard(id Info) error {
	if id.Internal {
		return fmt.Errorf("refusing %s: it is an internal disk", id.Identifier)
	}
	if !id.Removable {
		return fmt.Errorf("refusing %s: it is not removable media", id.Identifier)
	}
	return nil
}

func padToSector(buf []byte) []byte {
	r := len(buf) % sectorBytes
	if r == 0 {
		return buf
	}
	padded := make([]byte, len(buf)+sectorBytes-r)
	copy(padded, buf)
	return padded
}

func copyChunks(dst io.Writer, src io.Reader, onProgress func(int64), padWrites bool) error {
	buf := make([]byte, chunkSize)
	var total int64
	for {
		n, err := io.ReadFull(src, buf)
		if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
			return fmt.Errorf("reading: %w", err)
		}
		if n > 0 {
			chunk := buf[:n]
			if padWrites {
				chunk = padToSector(chunk)
			}
			if _, werr := dst.Write(chunk); werr != nil {
				return fmt.Errorf("writing: %w", werr)
			}
			total += int64(n)
			if onProgress != nil {
				onProgress(total)
			}
		}
		if err == io.ErrUnexpectedEOF || err == io.EOF {
			return nil
		}
	}
}

func List() ([]Info, error) {
	disks, err := platformList()
	if err != nil {
		return nil, err
	}
	var out []Info
	for _, d := range disks {
		if isCard(d) {
			out = append(out, d)
		}
	}
	return out, nil
}

func Report() []string {
	disks, err := platformList()
	var out []string
	if err != nil {
		out = append(out, "listing failed: "+err.Error())
	}
	for _, d := range disks {
		out = append(out, fmt.Sprintf("%s %q %d bytes internal=%v removable=%v listed=%v",
			d.Identifier, d.Name, d.Size, d.Internal, d.Removable, isCard(d)))
	}
	return append(out, platformNotes()...)
}

// RawDevice is an open raw disk handle; Close also drops a Windows write handle's volume locks, tied to Close not GC.
type RawDevice interface {
	io.Reader
	io.ReaderAt
	io.Writer
	io.WriterAt
	io.Closer
}

func OpenRaw(id Info, write bool) (RawDevice, error) {
	if err := refuseUnlessCard(id); err != nil {
		return nil, err
	}
	if write {
		if err := confirmStillThere(id); err != nil {
			return nil, err
		}
	}
	return platformOpen(id, write)
}

func confirmStillThere(id Info) error {
	disks, err := platformList()
	if err != nil {
		return fmt.Errorf("re-checking %s before writing: %w", id.Identifier, err)
	}
	return checkStillThere(id, disks)
}

func checkStillThere(id Info, disks []Info) error {
	for _, d := range disks {
		if d.Identifier != id.Identifier {
			continue
		}
		if d.Size != id.Size || d.Name != id.Name || d.Removable != id.Removable {
			return fmt.Errorf("%s has changed since it was picked (now %q, %d bytes, removable=%v); refusing to write", id.Identifier, d.Name, d.Size, d.Removable)
		}
		return nil
	}
	return fmt.Errorf("%s is no longer attached; refusing to write", id.Identifier)
}

func Unmount(id string) error { return platformUnmount(id) }

func ReadHeader(id Info) ([]byte, error) {
	f, err := OpenRaw(id, false)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ReadHeaderFromHandle(f)
}

// ReadHeaderFromHandle reads the leading partition-table bytes through an already-open handle.
func ReadHeaderFromHandle(f RawDevice) ([]byte, error) {
	data := make([]byte, 1024)
	if _, err := f.ReadAt(data, 0); err != nil {
		return nil, fmt.Errorf("reading the partition table: %w", err)
	}
	return data, nil
}

func ReadRaw(id Info, outPath string, maxBytes int64, onProgress func(written int64)) error {
	src, err := OpenRaw(id, false)
	if err != nil {
		return err
	}
	defer src.Close()
	return ReadRawFromHandle(src, outPath, maxBytes, onProgress)
}

// ReadRawFromHandle copies maxBytes (or everything, if 0) from an already-open handle to outPath.
func ReadRawFromHandle(src RawDevice, outPath string, maxBytes int64, onProgress func(written int64)) error {
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()

	var r io.Reader = src
	if maxBytes > 0 {
		r = io.LimitReader(src, maxBytes)
	}
	return copyChunks(out, r, onProgress, false)
}

func hashStream(r io.Reader, n int64, onProgress func(written int64)) (string, error) {
	h := sha256.New()
	if err := copyChunks(h, io.LimitReader(r, n), onProgress, false); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// WriteToHandle writes src to an already-open handle; the caller owns opening and closing it.
func WriteToHandle(dst RawDevice, src io.Reader, onProgress func(written int64)) error {
	return copyChunks(dst, src, onProgress, true)
}

// HashHandle reads the first n bytes back through dst's ReaderAt and hashes them.
// Reading through the same handle that did the write, before it's closed, means a
// remount's writes to the new partition table (or a page cache getting invalidated
// only on close) never reach this read-back.
func HashHandle(dst RawDevice, n int64, onProgress func(written int64)) (string, error) {
	return hashStream(io.NewSectionReader(dst, 0, n), n, onProgress)
}

func checkRangesAligned(ranges []Range) error {
	for _, r := range ranges {
		if r.Offset%sectorBytes != 0 || int64(len(r.Data))%sectorBytes != 0 {
			return fmt.Errorf("range at 0x%x, %d bytes, is not sector-aligned", r.Offset, len(r.Data))
		}
	}
	return nil
}

func WriteRanges(id Info, ranges []Range) error {
	if len(ranges) == 0 {
		return nil
	}
	if err := checkRangesAligned(ranges); err != nil {
		return err
	}

	f, err := OpenRaw(id, true)
	if err != nil {
		return err
	}
	defer f.Close()
	return WriteRangesToHandle(f, ranges)
}

// WriteRangesToHandle writes ranges through an already-open handle.
func WriteRangesToHandle(f RawDevice, ranges []Range) error {
	if err := checkRangesAligned(ranges); err != nil {
		return err
	}
	for _, r := range ranges {
		if _, err := f.WriteAt(r.Data, r.Offset); err != nil {
			return fmt.Errorf("writing the card at 0x%x: %w", r.Offset, err)
		}
	}
	return nil
}

func ReadRanges(id Info, ranges []Range) ([][]byte, error) {
	if len(ranges) == 0 {
		return nil, nil
	}
	if err := checkRangesAligned(ranges); err != nil {
		return nil, err
	}

	f, err := OpenRaw(id, false)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ReadRangesFromHandle(f, ranges)
}

// ReadRangesFromHandle reads ranges back through an already-open handle.
func ReadRangesFromHandle(f RawDevice, ranges []Range) ([][]byte, error) {
	if err := checkRangesAligned(ranges); err != nil {
		return nil, err
	}
	result := make([][]byte, len(ranges))
	for i, r := range ranges {
		buf := make([]byte, len(r.Data))
		if _, err := f.ReadAt(buf, r.Offset); err != nil {
			return nil, fmt.Errorf("reading the card back at 0x%x: %w", r.Offset, err)
		}
		result[i] = buf
	}
	return result, nil
}
