package consent

import (
	"encoding/binary"
	"fmt"
)

const sectorSize = 512

// Offsets in the 512-byte .mod header.
const (
	offPayloadLen = 0x00 // container length, less this header
	offSum4       = 0x34 // word sum of everything after this header
	offName       = 0x16 // the map version, e.g. "G218ENNI.120"
	offSegTable   = 0x90 // per-segment entries, 0x10 bytes each
	segOffset     = 0x200
)

// Offsets in segment 0's header sector.
const (
	offMagic     = 0x00 // "MTCP"
	offChunkCnt  = 0x04
	offReserved  = 0x1c // three words the loader sums but nothing reads
	offChunkEnts = 0x20 // 0x20 bytes each: sectors, plain size, packed size, sum

	loaderSpan = 0x1e0 // how much of the header sector the loader checksums; the tail past it is invisible to it
)

// chunk is one LZ4 block in segment 0, 2 MiB of plaintext padded to whole sectors.
type chunk struct {
	at      int // byte offset in the .mod
	sectors int
	plain   int // uncompressed length
	packed  int // compressed length
	plainAt int // offset in the segment's concatenated plaintext
}

// wordSum is the container's checksum throughout: a plain 32-bit sum of little-endian words, not a CRC.
func wordSum(b []byte) uint32 {
	var s uint32
	for i := 0; i+4 <= len(b); i += 4 {
		s += binary.LittleEndian.Uint32(b[i:])
	}
	return s
}

func chunks(mod []byte) ([]chunk, error) {
	if string(mod[segOffset:segOffset+4]) != "MTCP" {
		return nil, fmt.Errorf("segment 0 is not an MTCP segment")
	}
	count := int(binary.LittleEndian.Uint32(mod[segOffset+offChunkCnt:]))
	if count < 1 || segOffset+offChunkEnts+count*0x20 > segOffset+loaderSpan {
		return nil, fmt.Errorf("MTCP chunk count %d out of range", count)
	}
	out := make([]chunk, count)
	at, plainAt := segOffset+sectorSize, 0
	for i := range out {
		e := mod[segOffset+offChunkEnts+i*0x20:]
		out[i] = chunk{
			at:      at,
			sectors: int(binary.LittleEndian.Uint32(e[0:])),
			plain:   int(binary.LittleEndian.Uint32(e[4:])),
			packed:  int(binary.LittleEndian.Uint32(e[8:])),
			plainAt: plainAt,
		}
		plainAt += out[i].plain
		at += out[i].sectors * sectorSize
	}
	if at > len(mod) {
		return nil, fmt.Errorf("MTCP chunks run %d bytes past the container", at-len(mod))
	}
	return out, nil
}

// loaderSum is loaderSpan bytes of the header sector plus the chunk sectors, what the NK2 loader checks against.
func loaderSum(mod []byte, cs []chunk) uint32 {
	s := wordSum(mod[segOffset : segOffset+loaderSpan])
	for _, c := range cs {
		s += wordSum(mod[c.at : c.at+c.sectors*sectorSize])
	}
	return s
}

func writeChunk(mod []byte, cs []chunk, i int, packed []byte) {
	c := cs[i]
	room := c.sectors * sectorSize
	copy(mod[c.at:c.at+room], packed)
	clear(mod[c.at+len(packed) : c.at+room])

	e := mod[segOffset+offChunkEnts+i*0x20:]
	binary.LittleEndian.PutUint32(e[8:], uint32(len(packed)))
	binary.LittleEndian.PutUint32(e[12:], wordSum(mod[c.at:c.at+room]))
	cs[i].packed = len(packed)
}
