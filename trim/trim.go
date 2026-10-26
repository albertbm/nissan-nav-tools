// Package trim finds the trimmed size for a card image: the end of its
// last partition, padded to match a stock card or rounded up to 1 MiB.
package trim

import (
	"bytes"
	"fmt"
	"io"
)

const (
	sectorSize = 512
	mib        = 1 << 20

	// StockSize is a stock 16 GB card's exact length; a trim never goes below it.
	StockSize = 16_039_018_496
)

type partEntry struct {
	lba   int64
	count int64
}

// Size is the end of r's last partition, padded up to StockSize or rounded
// up to 1 MiB. It reads only the first two sectors.
func Size(r io.ReaderAt) (int64, error) {
	sector0 := make([]byte, sectorSize)
	if _, err := r.ReadAt(sector0, 0); err != nil {
		return 0, fmt.Errorf("reading sector 0: %w", err)
	}
	sector1 := make([]byte, sectorSize)
	if _, err := r.ReadAt(sector1, sectorSize); err != nil {
		return 0, fmt.Errorf("reading sector 1: %w", err)
	}

	table, tableName := sector0, "sector 0"
	if bytes.HasPrefix(sector1, []byte("CLARION ID")) {
		table, tableName = sector1, "sector 1"
	}

	parts, err := readPartitionTable(table)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", tableName, err)
	}

	var end int64
	for _, p := range parts {
		if e := (p.lba + p.count) * sectorSize; e > end {
			end = e
		}
	}
	if end == 0 {
		return 0, fmt.Errorf("%s: every partition has a zero end", tableName)
	}

	if end <= StockSize {
		return StockSize, nil
	}
	return roundUp(end, mib), nil
}

// readPartitionTable reads the MBR entry layout at 0x1be, shared by a Clarion sector 1 and an ordinary sector 0.
func readPartitionTable(sector []byte) ([]partEntry, error) {
	if len(sector) < 512 || sector[510] != 0x55 || sector[511] != 0xaa {
		return nil, fmt.Errorf("no boot signature")
	}
	var out []partEntry
	for i := 0; i < 4; i++ {
		e := sector[0x1be+i*16 : 0x1be+i*16+16]
		ptype := e[4]
		lba := int64(le32(e[8:12]))
		count := int64(le32(e[12:16]))
		if ptype == 0 || count == 0 {
			continue
		}
		out = append(out, partEntry{lba, count})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no partitions")
	}
	return out, nil
}

func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func roundUp(n, unit int64) int64 {
	if n%unit == 0 {
		return n
	}
	return (n/unit + 1) * unit
}
