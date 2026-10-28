// Package anonymize zeros the phonebook, pairing, voice-dial grammar, route history, CARWINGS cache, and front-gap settings on a Clarion QY8 card image.
package anonymize

import (
	"bytes"
	"fmt"
	"io"
)

type ReadWriterAt interface {
	io.ReaderAt
	io.WriterAt
}

type Item struct {
	Path   string
	Offset int64
	Size   int64
	What   string
}

// Report is what Reset did; no field carries a name, number, MAC or coordinate.
type Report struct {
	Items []Item

	FrontGapFound   bool
	FrontGapOffset  int64
	FrontGapSize    int64
	FrontGapRecords int

	CWSPartitions int

	FreeClusters    int64
	FreeBytesZeroed int64
}

func (r Report) BytesZeroed() int64 {
	var n int64
	for _, it := range r.Items {
		n += it.Size
	}
	if r.FrontGapFound {
		n += r.FrontGapSize
	}
	n += r.FreeBytesZeroed
	return n
}

// Reset zeros every personal-data item it finds on f, refusing images without the Clarion signature in sector 1.
func Reset(f ReadWriterAt, size int64) (Report, error) {
	var rep Report

	sector1 := make([]byte, 512)
	if _, err := f.ReadAt(sector1, 512); err != nil {
		return rep, fmt.Errorf("reading sector 1: %w", err)
	}
	if sector1[510] != 0x55 || sector1[511] != 0xAA || !bytes.HasPrefix(sector1, []byte("CLARION ID")) {
		return rep, fmt.Errorf("no Clarion signature in sector 1, refusing to touch this image")
	}

	parts, err := readPartitionTable(sector1)
	if err != nil {
		return rep, err
	}

	for _, p := range parts {
		vol, err := openExfat(f, p.lba)
		if err != nil {
			continue
		}
		rep.CWSPartitions++
		items, err := vol.findTargets()
		if err != nil {
			return rep, fmt.Errorf("walking exFAT partition at LBA %d: %w", p.lba, err)
		}
		for _, it := range items {
			if err := vol.zero(f, it); err != nil {
				return rep, fmt.Errorf("zeroing %s: %w", it.path, err)
			}
			rep.Items = append(rep.Items, Item{
				Path:   it.path,
				Offset: it.offset,
				Size:   it.size,
				What:   it.what,
			})
		}
		clusters, bytesZeroed, err := vol.zeroFreeClusters(f)
		if err != nil {
			return rep, fmt.Errorf("zeroing free space on the exFAT partition at LBA %d: %w", p.lba, err)
		}
		rep.FreeClusters += clusters
		rep.FreeBytesZeroed += bytesZeroed
	}

	if err := resetFrontGap(f, size, &rep); err != nil {
		return rep, err
	}

	return rep, nil
}

// Window is a byte range Reset may write: a CWS exFAT partition's full extent, or the front gap.
type Window struct {
	Offset int64
	Size   int64
}

// Bounds reports every window Reset is allowed to write. A caller writing straight to a
// card, rather than through Reset's own ReadWriterAt, can refuse anything outside them.
func Bounds(f io.ReaderAt, size int64) ([]Window, error) {
	sector1 := make([]byte, 512)
	if _, err := f.ReadAt(sector1, 512); err != nil {
		return nil, fmt.Errorf("reading sector 1: %w", err)
	}
	if sector1[510] != 0x55 || sector1[511] != 0xAA || !bytes.HasPrefix(sector1, []byte("CLARION ID")) {
		return nil, fmt.Errorf("no Clarion signature in sector 1, refusing to touch this image")
	}
	parts, err := readPartitionTable(sector1)
	if err != nil {
		return nil, err
	}

	var out []Window
	for _, p := range parts {
		boot := make([]byte, 512)
		if _, err := f.ReadAt(boot, p.lba*512); err != nil {
			continue
		}
		if string(boot[3:11]) != "EXFAT   " {
			continue
		}
		out = append(out, Window{Offset: p.lba * 512, Size: p.count * 512})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no exFAT partition found")
	}
	if size >= frontGapOffset+frontGapSize {
		out = append(out, Window{Offset: frontGapOffset, Size: frontGapSize})
	}
	return out, nil
}

type partEntry struct {
	ptype byte
	lba   int64
	count int64
}

// readPartitionTable reads sector 1, not the MBR at sector 0; the real table lives there on these cards.
func readPartitionTable(sector1 []byte) ([]partEntry, error) {
	var out []partEntry
	for i := 0; i < 4; i++ {
		e := sector1[0x1be+i*16 : 0x1be+i*16+16]
		ptype := e[4]
		lba := int64(le32(e[8:12]))
		count := int64(le32(e[12:16]))
		if ptype == 0 || count == 0 {
			continue
		}
		out = append(out, partEntry{ptype, lba, count})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("sector 1 has no partitions")
	}
	return out, nil
}

func le16(b []byte) uint16 { return uint16(b[0]) | uint16(b[1])<<8 }
func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}
func le64(b []byte) uint64 {
	return uint64(le32(b[0:4])) | uint64(le32(b[4:8]))<<32
}
