package anonymize

import (
	"bytes"
	"io"
	"testing"
)

type memImg struct{ data []byte }

func newMemImg(size int) *memImg { return &memImg{data: make([]byte, size)} }

func (m *memImg) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(m.data)) {
		return 0, io.EOF
	}
	n := copy(p, m.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (m *memImg) WriteAt(p []byte, off int64) (int, error) {
	if off+int64(len(p)) > int64(len(m.data)) {
		return 0, io.ErrShortWrite
	}
	return copy(m.data[off:], p), nil
}

func putLE32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func putLE16(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

func utf16le(s string) []byte {
	out := make([]byte, 0, len(s)*2)
	for _, r := range s {
		out = append(out, byte(r), byte(r>>8))
	}
	return out
}

func bitmapEntry(first uint32, dataSize uint64) []byte {
	e := make([]byte, 32)
	e[0] = 0x81
	putLE32(e[0x14:0x18], first)
	b := make([]byte, 8)
	putLE32(b[0:4], uint32(dataSize))
	copy(e[0x18:0x20], b)
	return e
}

func buildBitmap(clusterCount int, allocated []int) []byte {
	bits := make([]byte, (clusterCount+7)/8)
	for _, c := range allocated {
		bit := c - 2
		bits[bit/8] |= 1 << uint(bit%8)
	}
	return bits
}

func dirEntry(name string, isDir, noFat bool, first uint32, dataSize uint64) []byte {
	e := make([]byte, 96)
	e[0] = 0x85
	if isDir {
		putLE16(e[4:6], 0x10)
	}
	e[32] = 0xc0
	if noFat {
		e[33] = 2
	}
	putLE32(e[32+0x14:32+0x18], first)
	b := make([]byte, 8)
	putLE32(b[0:4], uint32(dataSize))
	copy(e[32+0x18:32+0x20], b)
	e[64] = 0xc1
	copy(e[64+2:96], utf16le(name))
	return e
}

func buildSynthCard(size int) *memImg {
	img := newMemImg(size)
	copy(img.data[512:], []byte("CLARION ID"))
	img.data[512+510] = 0x55
	img.data[512+511] = 0xAA

	const volLBA = 10
	e := img.data[512+0x1be : 512+0x1be+16]
	e[4] = 0x07
	putLE32(e[8:12], volLBA)
	putLE32(e[12:16], 40)

	base := volLBA * 512
	boot := img.data[base : base+512]
	copy(boot[3:11], []byte("EXFAT   "))
	putLE32(boot[0x50:0x54], 1)
	putLE32(boot[0x54:0x58], 1)
	putLE32(boot[0x58:0x5c], 2)
	const clusterCount = 38
	putLE32(boot[0x5c:0x60], clusterCount)
	putLE32(boot[0x60:0x64], 2)
	boot[0x6c] = 9
	boot[0x6d] = 0

	fat := img.data[base+512 : base+512+512]
	for _, c := range []int{2, 3, 4, 5} {
		putLE32(fat[c*4:c*4+4], 0xffffffff)
	}

	clusterOff := func(c int) int { return base + (2+(c-2))*512 }

	root := img.data[clusterOff(2) : clusterOff(2)+512]
	copy(root, dirEntry("PRIVATE", true, false, 3, 0))

	const bitmapCluster = 20
	allocated := []int{2, 3, 4, 5, 6, 7, bitmapCluster}
	bitmapBits := buildBitmap(clusterCount, allocated)
	copy(root[96:], bitmapEntry(bitmapCluster, uint64(len(bitmapBits))))
	copy(img.data[clusterOff(bitmapCluster):], bitmapBits)

	private := img.data[clusterOff(3) : clusterOff(3)+512]
	copy(private, dirEntry("TEL", true, false, 4, 0))

	tel := img.data[clusterOff(4) : clusterOff(4)+512]
	copy(tel, dirEntry("PB_OBJ", true, false, 5, 0))

	pbObj := img.data[clusterOff(5) : clusterOff(5)+512]
	copy(pbObj, dirEntry("0_TelBook.bin", false, true, 6, 100))

	fileData := img.data[clusterOff(6) : clusterOff(6)+512]
	for i := 0; i < 100; i++ {
		fileData[i] = 0xAA
	}
	fileData[200] = 0xBB

	return img
}

func TestResetZeroesPhonebook(t *testing.T) {
	img := buildSynthCard(10*512 + 40*512)
	rep, err := Reset(img, int64(len(img.data)))
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if len(rep.Items) != 1 {
		t.Fatalf("got %d items, want 1: %+v", len(rep.Items), rep.Items)
	}
	if rep.Items[0].Path != "/PRIVATE/TEL/PB_OBJ/0_TelBook.bin" {
		t.Errorf("path = %q", rep.Items[0].Path)
	}
	const base = 10*512 + (2+(6-2))*512
	cluster := img.data[base : base+512]
	if !bytes.Equal(cluster, make([]byte, 512)) {
		t.Errorf("phonebook cluster not fully zeroed")
	}
	if rep.FrontGapFound {
		t.Errorf("front gap reported found on an image too small to carry one")
	}
}

func TestResetZeroesVoiceGrammar(t *testing.T) {
	img := buildSynthCard(10*512 + 40*512)
	clusterOff := func(c int) int { return 10*512 + (2+(c-2))*512 }

	// /PRIVATE/TEL/PB_OBJ is cluster 5; add SPEECH/XX/TTG/PB01/GPB000.G2P and SPEECH/XX/TTG/AB01/GAB000.G2P as siblings of PRIVATE.
	// Root already holds PRIVATE at [0:96] and the bitmap entry at [96:128]; SPEECH goes right after.
	root := img.data[clusterOff(2) : clusterOff(2)+512]
	copy(root[128:], dirEntry("SPEECH", true, false, 8, 0))

	speech := img.data[clusterOff(8) : clusterOff(8)+512]
	copy(speech, dirEntry("XX", true, false, 9, 0))

	xx := img.data[clusterOff(9) : clusterOff(9)+512]
	copy(xx, dirEntry("TTG", true, false, 10, 0))

	ttg := img.data[clusterOff(10) : clusterOff(10)+512]
	copy(ttg, dirEntry("PB01", true, false, 11, 0))
	copy(ttg[96:], dirEntry("AB01", true, false, 12, 0))

	pb01 := img.data[clusterOff(11) : clusterOff(11)+512]
	copy(pb01, dirEntry("GPB000.G2P", false, true, 13, 20))
	grammar := img.data[clusterOff(13) : clusterOff(13)+512]
	for i := 0; i < 20; i++ {
		grammar[i] = 0xAA
	}

	ab01 := img.data[clusterOff(12) : clusterOff(12)+512]
	copy(ab01, dirEntry("GAB000.G2P", false, true, 14, 20))
	seed := img.data[clusterOff(14) : clusterOff(14)+512]
	for i := range seed {
		seed[i] = 0xCC
	}
	before := append([]byte(nil), seed...)

	// cluster 6 (the phonebook) is still referenced by the FAT chain as allocated in buildSynthCard; add 8-14 too.
	fat := img.data[10*512+512 : 10*512+512+512]
	for _, c := range []int{8, 9, 10, 11, 12, 13, 14} {
		putLE32(fat[c*4:c*4+4], 0xffffffff)
	}
	const bitmapCluster = 20
	allocated := []int{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, bitmapCluster}
	copy(img.data[clusterOff(bitmapCluster):], buildBitmap(38, allocated))

	rep, err := Reset(img, int64(len(img.data)))
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if !bytes.Equal(grammar[:20], make([]byte, 20)) {
		t.Errorf("voice grammar file not zeroed")
	}
	if !bytes.Equal(seed, before) {
		t.Errorf("AB01's grammar seed was touched, want it left alone")
	}
	var sawGrammar bool
	for _, it := range rep.Items {
		if it.Path == "/SPEECH/XX/TTG/PB01/GPB000.G2P" {
			sawGrammar = true
		}
		if it.Path == "/SPEECH/XX/TTG/AB01/GAB000.G2P" {
			t.Errorf("AB01 reported as cleared")
		}
	}
	if !sawGrammar {
		t.Errorf("grammar file not reported: %+v", rep.Items)
	}
}

func TestResetRefusesWithoutClarionSignature(t *testing.T) {
	img := newMemImg(4096)
	if _, err := Reset(img, int64(len(img.data))); err == nil {
		t.Errorf("Reset on a non-Clarion image: no error")
	}
}

func TestResetNeverTouchesUnmatchedFile(t *testing.T) {
	img := buildSynthCard(10*512 + 40*512)
	clusterOff := func(c int) int { return 10*512 + (2+(c-2))*512 }
	pbObj := img.data[clusterOff(5) : clusterOff(5)+512]
	copy(pbObj[96:], dirEntry("CHNLIST.DAT", false, true, 7, 50))
	other := img.data[clusterOff(7) : clusterOff(7)+512]
	for i := range other {
		other[i] = 0xCC
	}
	before := append([]byte(nil), other...)

	if _, err := Reset(img, int64(len(img.data))); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if !bytes.Equal(other, before) {
		t.Errorf("an unmatched file was modified")
	}
}

func TestResetZeroesFreeClustersButNotAllocatedOnes(t *testing.T) {
	img := buildSynthCard(10*512 + 40*512)
	clusterOff := func(c int) int { return 10*512 + (2+(c-2))*512 }

	const freeCluster = 10
	residue := img.data[clusterOff(freeCluster) : clusterOff(freeCluster)+512]
	copy(residue, []byte{0x85, 0x02, 'o', 'l', 'd'})

	rep, err := Reset(img, int64(len(img.data)))
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if !bytes.Equal(residue, make([]byte, 512)) {
		t.Errorf("free cluster %d still holds old data: %x", freeCluster, residue[:8])
	}
	if rep.FreeClusters == 0 || rep.FreeBytesZeroed == 0 {
		t.Errorf("report shows no free space cleared: %+v", rep)
	}

	phonebook := img.data[clusterOff(6) : clusterOff(6)+512]
	if !bytes.Equal(phonebook, make([]byte, 512)) {
		t.Errorf("allocated phonebook cluster not zeroed")
	}
}
