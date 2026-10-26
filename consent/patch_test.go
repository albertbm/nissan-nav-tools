package consent

import (
	"bytes"
	"encoding/binary"
	"testing"

	"nissan-nav-tools/consent/lz4hc"
)

func TestInstructionEncoding(t *testing.T) {
	// ZE1's stock handler re-emitted unchanged, so these encodings are known good.
	const va = 0x41a0a5c0
	tests := []struct {
		name string
		got  uint32
		want uint32
	}{
		{"bl to GetContext", bl(va+0x00, 0x41b2f560), 0xeb0493e6},
		{"b to the tail", b(va+0x70, 0x41a0a680), 0xea000012},
		{"bne over the second dispatch", bne(va+0x54, va+0x68), 0x1a000003},
		{"movw of a composite action", movw(1, 4892), 0xe301131c},
		{"mov of a small variable id", movImm(1, 73), 0xe3a01049},
		{"mov with a rotated immediate", movImm(1, 628), 0xe3a01f9d},
		{"movw where mov cannot reach", movImm(1, 1687), 0xe3001697},
		{"ldr from the literal pool", ldrPC(va+0x04, 2, 0x41a0a690), 0xe59f20c4},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s: got 0x%08x, want 0x%08x", tt.name, tt.got, tt.want)
		}
	}
}

func TestWordSum(t *testing.T) {
	b := make([]byte, 12)
	binary.LittleEndian.PutUint32(b[0:], 0xffffffff)
	binary.LittleEndian.PutUint32(b[4:], 2)
	binary.LittleEndian.PutUint32(b[8:], 0x10000000)
	if got := wordSum(b); got != 0x10000001 {
		t.Errorf("wordSum = 0x%08x, want 0x10000001 (it has to wrap)", got)
	}
	if got := wordSum(b[:3]); got != 0 {
		t.Errorf("wordSum of a partial word = 0x%08x, want 0", got)
	}
}

// testBuild fits a container small enough for a test; the handler lands at plaintext offset 0x400.
func testBuild() build {
	stock := make([]uint32, 29)
	for i := range stock {
		stock[i] = 0xe1a00000 | uint32(i)
	}
	return build{
		name:    "G218ENNI.120",
		codeROM: 0x100,
		codeVA:  0x40000100,

		getContext: 0x40001000,
		setVar:     0x40001010,
		getState:   0x40001020,
		getState2:  0x40001030,
		evHandler:  0x40001040,
		execFn:     0x40001050,

		skip: handler{
			va:    0x40000400,
			tail:  0x40000500,
			pTrue: 0x40000600,

			varSkip:    73,
			varAgree:   257,
			varStartup: 628,

			action: 4892,
			ctrl:   25,
			notify: 1687,

			stock: stock,
		},
	}
}

// testPlain is 8 KiB of compressible filler with the stock handler in it.
func testPlain(bd build) []byte {
	plain := bytes.Repeat([]byte{0xe1, 0xa0, 0x00, 0x00, 0x12, 0x34, 0x00, 0x00}, 1024)
	putWords(plain[0x400:], bd.skip.stock)
	return plain
}

// testMod assembles a one-segment container around plain, with slack bytes of room beyond what the chunk needs.
func testMod(t *testing.T, bd *build, plain []byte, slack int) []byte {
	t.Helper()
	packed := lz4hc.Compress(plain)
	sectors := (len(packed) + slack + sectorSize - 1) / sectorSize
	segLen := (1 + sectors) * sectorSize
	mod := make([]byte, segOffset+segLen)
	bd.modLen = len(mod)

	copy(mod[offName:], bd.name)
	binary.LittleEndian.PutUint32(mod[offPayloadLen:], uint32(segLen))
	seg := mod[offSegTable:]
	binary.LittleEndian.PutUint32(seg[4:], uint32(1+sectors))
	binary.LittleEndian.PutUint32(seg[8:], uint32(segLen))

	copy(mod[segOffset:], "MTCP")
	binary.LittleEndian.PutUint32(mod[segOffset+offChunkCnt:], 1)
	e := mod[segOffset+offChunkEnts:]
	binary.LittleEndian.PutUint32(e[0:], uint32(sectors))
	binary.LittleEndian.PutUint32(e[4:], uint32(len(plain)))
	binary.LittleEndian.PutUint32(e[8:], uint32(len(packed)))
	copy(mod[segOffset+sectorSize:], packed)
	binary.LittleEndian.PutUint32(e[12:], wordSum(mod[segOffset+sectorSize:]))

	binary.LittleEndian.PutUint32(seg[12:], wordSum(mod[segOffset:]))
	binary.LittleEndian.PutUint32(mod[offSum4:], wordSum(mod[segOffset:]))
	return mod
}

func TestPatchMod(t *testing.T) {
	bd := testBuild()
	plain := testPlain(bd)
	mod := testMod(t, &bd, plain, sectorSize)
	header := bytes.Clone(mod[:segOffset])

	res, err := patchMod(mod, bd, func(string) {})
	if err != nil {
		t.Fatalf("patchMod: %v", err)
	}
	if res.Chunk != 0 {
		t.Errorf("patched chunk %d, want 0", res.Chunk)
	}
	if res.Packed > res.Room {
		t.Errorf("chunk is %d bytes over its sectors", res.Packed-res.Room)
	}
	if res.Compensation == 0 {
		t.Error("no compensating word was needed, which cannot be right here")
	}
	if !bytes.Equal(mod[:segOffset], header) {
		t.Error("the 512-byte header changed")
	}

	// Run patchMod's own checks again from the outside, against the container as written.
	cs, err := chunks(mod)
	if err != nil {
		t.Fatalf("chunks: %v", err)
	}
	segSum := binary.LittleEndian.Uint32(mod[offSegTable+12:])
	if got := loaderSum(mod, cs); got != segSum {
		t.Errorf("loader sum 0x%08x, header says 0x%08x", got, segSum)
	}
	if got := wordSum(mod[segOffset:]); got != binary.LittleEndian.Uint32(mod[offSum4:]) {
		t.Errorf("SUM4 does not match the payload")
	}

	back, err := lz4hc.Uncompress(mod[cs[0].at:cs[0].at+cs[0].packed], cs[0].plain)
	if err != nil {
		t.Fatalf("Uncompress: %v", err)
	}
	if err := checkWords(back[0x400:], bd.skipBody()); err != nil {
		t.Errorf("handler was not rewritten: %v", err)
	}
	want := bytes.Clone(plain)
	putWords(want[0x400:], bd.skipBody())
	if !bytes.Equal(back, want) {
		t.Error("the chunk changed outside the handler")
	}
}

func TestPatchModRefusesAnUnknownHandler(t *testing.T) {
	bd := testBuild()
	plain := testPlain(bd)
	plain[0x400+4*7] ^= 0x01 // one bit, in the middle of the body
	mod := testMod(t, &bd, plain, sectorSize)
	before := bytes.Clone(mod)

	if _, err := patchMod(mod, bd, func(string) {}); err == nil {
		t.Fatal("patched a handler it should not recognise")
	}
	if !bytes.Equal(mod, before) {
		t.Error("the container was modified before the check failed")
	}
}

func TestPatchModRefusesAChunkThatWouldNotFit(t *testing.T) {
	bd := testBuild()
	plain := testPlain(bd)
	mod := testMod(t, &bd, plain, 0)
	// One sector less than the chunk needs.
	e := mod[segOffset+offChunkEnts:]
	binary.LittleEndian.PutUint32(e[0:], binary.LittleEndian.Uint32(e[0:])-1)

	if _, err := patchMod(mod, bd, func(string) {}); err == nil {
		t.Fatal("accepted a chunk with nowhere to go")
	}
}

func TestFindSkipsUnalignedNames(t *testing.T) {
	img := make([]byte, 4<<20)
	name := []byte(builds[0].name)
	copy(img[0x3333:], name)           // a stray copy in the map data
	copy(img[0x200000+offName:], name) // the real container header

	got, err := Find(bytes.NewReader(img), int64(len(img)))
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Offset != 0x200000 {
		t.Errorf("found the nav image at 0x%x, want 0x200000", got.Offset)
	}
	if got.Build != builds[0].name {
		t.Errorf("build %q, want %q", got.Build, builds[0].name)
	}
}

func TestFindReportsNothingOnAnEmptyCard(t *testing.T) {
	img := make([]byte, 3<<20)
	if _, err := Find(bytes.NewReader(img), int64(len(img))); err == nil {
		t.Error("Find claimed to see a nav image in an empty card")
	}
}

func TestFindNamesAnUnknownBuild(t *testing.T) {
	img := make([]byte, 4<<20)
	at := 0x200000
	copy(img[at+offName:], "G214ELNI.999")
	copy(img[at+segOffset:], "MTCP")
	binary.LittleEndian.PutUint32(img[at+offPayloadLen:], 123)

	got, err := Find(bytes.NewReader(img), int64(len(img)))
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Offset != int64(at) {
		t.Errorf("found the nav image at 0x%x, want 0x%x", got.Offset, at)
	}
	if got.Build != "G214ELNI.999" {
		t.Errorf("build %q, want %q", got.Build, "G214ELNI.999")
	}

	if _, ok := findBuild(got.Build); ok {
		t.Fatal("test build name collides with a real entry in builds")
	}
	if _, err := ApplyConsentSkip(newRWAt(img), int64(len(img)), func(string) {}); err == nil {
		t.Error("ApplyConsentSkip patched a build it has no table entry for")
	}
}

// TestDiffPagesStaysInRange checks a write never lands outside the .mod it came from.
func TestDiffPagesStaysInRange(t *testing.T) {
	const base = 0x200000 + 123
	before := make([]byte, 3*pageSize+100)
	mod := bytes.Clone(before)
	mod[50] = 1
	mod[pageSize+10] = 1
	mod[len(mod)-1] = 1

	writes := diffPages(before, mod, base)
	if len(writes) != 3 {
		t.Fatalf("got %d page writes, want 3", len(writes))
	}
	lo, hi := int64(base), int64(base+len(mod))
	for _, w := range writes {
		if w.Offset < lo || w.Offset+int64(len(w.Data)) > hi {
			t.Errorf("write at 0x%x, %d bytes, falls outside 0x%x-0x%x", w.Offset, len(w.Data), lo, hi)
		}
	}
}

func newRWAt(b []byte) ReadWriterAt { return &memRWAt{b} }

type memRWAt struct{ b []byte }

func (m *memRWAt) ReadAt(p []byte, off int64) (int, error) {
	return bytes.NewReader(m.b).ReadAt(p, off)
}
func (m *memRWAt) WriteAt(p []byte, off int64) (int, error) {
	copy(m.b[off:], p)
	return len(p), nil
}
