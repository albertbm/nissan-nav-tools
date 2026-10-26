// Package disk lists removable storage and reads it raw.
package disk

import (
	"fmt"
	"io"
	"os"
)

// Info is one whole disk, external and physical.
type Info struct {
	Identifier string // e.g. "disk4"
	Name       string // the device's media name, blank if none was reported
	Size       int64
	Internal   bool
	Removable  bool // true for a card reader's slot; false for most external enclosures
}

const sectorBytes = 512

// A multiple of sectorBytes: only the final, short chunk of a device write ever needs padding.
const chunkSize = 4 << 20

// Head unit cards top out at 64 GB; this keeps external drives off the list.
const maxCardSize = 130_000_000_000

type Range struct {
	Offset int64
	Data   []byte
}

// Node is the raw device path to read d through, in the form the current platform uses.
func (d Info) Node() string { return platformNode(d) }

func isCard(d Info) bool {
	return !d.Internal && d.Removable && d.Size > 0 && d.Size <= maxCardSize
}

// refuseUnlessCard rejects any disk that is not external and removable.
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

// List drops any disk that doesn't look like a card, even if the platform's own listing let it through.
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

// OpenRaw opens id's raw device for I/O, refusing anything that isn't a card.
func OpenRaw(id Info, write bool) (*os.File, error) {
	if err := refuseUnlessCard(id); err != nil {
		return nil, err
	}
	return platformOpen(id, write)
}

// Unmount clears the buffer cache so nothing else writes under a raw read.
func Unmount(id string) error { return platformUnmount(id) }

func Eject(id string) error { return platformEject(id) }

func ReadHeader(id Info) ([]byte, error) {
	f, err := OpenRaw(id, false)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data := make([]byte, 1024)
	if _, err := io.ReadFull(f, data); err != nil {
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

func WriteRaw(id Info, srcPath string, onProgress func(written int64)) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := OpenRaw(id, true)
	if err != nil {
		return err
	}
	defer dst.Close()

	return copyChunks(dst, src, onProgress, true)
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
