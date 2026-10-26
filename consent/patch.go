// Package consent patches the nav image on a Clarion QY8 map card to skip
// the telematics consent screen. The .mod header must stay byte-identical,
// because the loader only boots the card's copy of NK2 when it matches the NAND's.
package consent

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"

	"nissan-nav-tools/consent/lz4hc"
)

const pageSize = 4096

// ReadWriterAt is what disk.OpenDeviceForReadWrite returns.
type ReadWriterAt interface {
	io.ReaderAt
	io.WriterAt
}

type Target struct {
	Offset int64
	Build  string
	Size   int
}

// Result is what the patch did. SHA256 covers the whole .mod.
type Result struct {
	Target       Target
	Chunk        int
	Stock        int // chunk's packed size as shipped
	Packed       int // and after recompression
	Room         int // what the chunk's sectors hold
	Compensation uint32
	Pages        int // 4 KiB pages written
	SHA256       string
}

// Find names the build it recognises, or, failing that, whatever build name the image itself claims.
func Find(f io.ReaderAt, size int64) (Target, error) {
	if t, err := findKnown(f, size); err == nil {
		return t, nil
	}
	return findBySignature(f, size)
}

func findKnown(f io.ReaderAt, size int64) (Target, error) {
	const step = 1 << 20
	buf := make([]byte, step+sectorSize)
	for off := int64(0); size <= 0 || off < size; off += step {
		n, err := f.ReadAt(buf, off)
		blk := buf[:max(n, 0)]
		for _, bd := range builds {
			name := []byte(bd.name)
			for i := 0; i < len(blk); {
				j := bytes.Index(blk[i:], name)
				if j < 0 {
					break
				}
				i += j
				if at := off + int64(i) - offName; at >= 0 && at%sectorSize == 0 {
					return Target{Offset: at, Build: bd.name, Size: bd.modLen}, nil
				}
				i++
			}
		}
		if err != nil {
			break
		}
	}
	return Target{}, fmt.Errorf("no nav image this patcher knows is on that target")
}

// findBySignature falls back to the MTCP marker, so an unrecognised build is named instead of reported as nothing.
func findBySignature(f io.ReaderAt, size int64) (Target, error) {
	const step = 1 << 20
	buf := make([]byte, step+sectorSize)
	for off := int64(0); size <= 0 || off < size; off += step {
		n, err := f.ReadAt(buf, off)
		blk := buf[:max(n, 0)]
		for i := 0; i < len(blk); {
			j := bytes.Index(blk[i:], []byte("MTCP"))
			if j < 0 {
				break
			}
			i += j
			at := off + int64(i) - segOffset
			i++
			if at < 0 || at%sectorSize != 0 {
				continue
			}
			hdr := make([]byte, offSegTable)
			if _, herr := f.ReadAt(hdr, at); herr != nil {
				continue
			}
			nameField := hdr[offName:]
			if nul := bytes.IndexByte(nameField, 0); nul >= 0 {
				nameField = nameField[:nul]
			}
			name := string(nameField)
			if name == "" {
				continue
			}
			plen := binary.LittleEndian.Uint32(hdr[offPayloadLen:])
			return Target{Offset: at, Build: name, Size: int(plen) + segOffset}, nil
		}
		if err != nil {
			break
		}
	}
	return Target{}, fmt.Errorf("no nav image this patcher knows is on that target")
}

// ApplyConsentSkip refuses to write unless the handler matches byte for
// byte, so a map version whose addresses moved is left alone, not corrupted.
func ApplyConsentSkip(f ReadWriterAt, size int64, log func(string)) (Result, error) {
	t, err := Find(f, size)
	if err != nil {
		return Result{}, err
	}
	bd, ok := findBuild(t.Build)
	if !ok {
		return Result{Target: t}, fmt.Errorf("no patch for app version %q on this card", t.Build)
	}
	log(fmt.Sprintf("Found %s at 0x%x, %d bytes", bd.name, t.Offset, bd.modLen))

	mod := make([]byte, bd.modLen)
	if _, err := f.ReadAt(mod, t.Offset); err != nil && err != io.EOF {
		return Result{}, fmt.Errorf("cannot read the nav image: %w", err)
	}
	before := bytes.Clone(mod)

	res, err := patchMod(mod, bd, log)
	if err != nil {
		return Result{}, err
	}
	res.Target = t

	start := t.Offset
	for abs := start / pageSize * pageSize; abs < start+int64(len(mod)); abs += pageSize {
		lo := max(abs, start) - start
		hi := min(abs+pageSize, start+int64(len(mod))) - start
		if bytes.Equal(mod[lo:hi], before[lo:hi]) {
			continue
		}
		if _, err := f.WriteAt(mod[lo:hi], start+lo); err != nil {
			return res, fmt.Errorf("cannot write at 0x%x: %w", start+lo, err)
		}
		res.Pages++
	}

	sum := sha256.Sum256(mod)
	res.SHA256 = hex.EncodeToString(sum[:])
	log(fmt.Sprintf("Wrote %d pages of 4 KiB", res.Pages))
	log("Nav image sha256: " + res.SHA256)
	return res, nil
}

func Supported(name string) bool {
	_, ok := findBuild(name)
	return ok
}

func findBuild(name string) (build, bool) {
	for _, bd := range builds {
		if bd.name == name {
			return bd, true
		}
	}
	return build{}, false
}

func patchMod(mod []byte, bd build, log func(string)) (Result, error) {
	if len(mod) != bd.modLen {
		return Result{}, fmt.Errorf("nav image is %d bytes, expected %d", len(mod), bd.modLen)
	}
	if got := int(binary.LittleEndian.Uint32(mod[offPayloadLen:])) + segOffset; got != bd.modLen {
		return Result{}, fmt.Errorf("container claims %d bytes, expected %d", got, bd.modLen)
	}
	header := bytes.Clone(mod[:segOffset])

	seg := mod[offSegTable:]
	segLen := int(binary.LittleEndian.Uint32(seg[8:]))
	segSum := binary.LittleEndian.Uint32(seg[12:])
	if segOffset+segLen > len(mod) {
		return Result{}, fmt.Errorf("segment 0 runs past the container")
	}
	if got := wordSum(mod[segOffset : segOffset+segLen]); got != segSum {
		return Result{}, fmt.Errorf("segment 0 sum is 0x%08x, header says 0x%08x", got, segSum)
	}

	cs, err := chunks(mod)
	if err != nil {
		return Result{}, err
	}

	p := bd.skip
	romOff := bd.codeROM + int(p.va-bd.codeVA)
	i := -1
	for n, c := range cs {
		if romOff >= c.plainAt && romOff < c.plainAt+c.plain {
			i = n
			break
		}
	}
	if i < 0 {
		return Result{}, fmt.Errorf("handler at 0x%x is not in any chunk", p.va)
	}
	c := cs[i]
	at := romOff - c.plainAt
	log(fmt.Sprintf("Handler at 0x%x is ROM 0x%x, chunk %d +0x%x", p.va, romOff, i, at))

	plain, err := lz4hc.Uncompress(mod[c.at:c.at+c.packed], c.plain)
	if err != nil {
		return Result{}, fmt.Errorf("chunk %d: %w", i, err)
	}
	if err := checkWords(plain[at:], p.stock); err != nil {
		return Result{}, fmt.Errorf("chunk %d +0x%x: %w", i, at, err)
	}
	putWords(plain[at:], bd.skipBody())

	packed := lz4hc.Compress(plain)
	back, err := lz4hc.Uncompress(packed, len(plain))
	if err != nil || !bytes.Equal(back, plain) {
		return Result{}, fmt.Errorf("recompressed chunk %d does not round-trip", i)
	}
	room := c.sectors * sectorSize
	log(fmt.Sprintf("Chunk %d: %d -> %d bytes, sectors hold %d, %d spare",
		i, c.packed, len(packed), room, room-len(packed)))
	if len(packed) > room {
		return Result{}, fmt.Errorf("patched chunk %d is %d bytes over its sectors", i, len(packed)-room)
	}

	res := Result{Chunk: i, Stock: c.packed, Packed: len(packed), Room: room}
	writeChunk(mod, cs, i, packed)

	// The loader's sum skips the tail padding, so the drift goes in this reserved word instead.
	pad := mod[segOffset+offReserved:]
	if binary.LittleEndian.Uint32(pad) != 0 {
		return res, fmt.Errorf("the reserved word in segment 0 is not zero")
	}
	drift := segSum - wordSum(mod[segOffset:segOffset+segLen])
	binary.LittleEndian.PutUint32(pad, drift)
	res.Compensation = drift
	log(fmt.Sprintf("Compensating word 0x%08x at .mod +0x%x", drift, segOffset+offReserved))

	if got := wordSum(mod[segOffset : segOffset+segLen]); got != segSum {
		return res, fmt.Errorf("segment 0 sum drifted to 0x%08x", got)
	}
	if got := loaderSum(mod, cs); got != segSum {
		return res, fmt.Errorf("the loader's sum drifted to 0x%08x", got)
	}
	if got, want := wordSum(mod[segOffset:]), binary.LittleEndian.Uint32(mod[offSum4:]); got != want {
		return res, fmt.Errorf("SUM4 is 0x%08x, header says 0x%08x", got, want)
	}
	if !bytes.Equal(mod[:segOffset], header) {
		return res, fmt.Errorf("the 512-byte header changed")
	}
	return res, nil
}

func checkWords(b []byte, want []uint32) error {
	if len(b) < len(want)*4 {
		return fmt.Errorf("chunk ends before the handler does")
	}
	for i, w := range want {
		if got := binary.LittleEndian.Uint32(b[i*4:]); got != w {
			return fmt.Errorf("instruction %d is 0x%08x, expected 0x%08x", i, got, w)
		}
	}
	return nil
}

func putWords(b []byte, words []uint32) {
	for i, w := range words {
		binary.LittleEndian.PutUint32(b[i*4:], w)
	}
}
