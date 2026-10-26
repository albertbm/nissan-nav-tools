package disk

import (
	"bytes"
	"io"
	"math/rand"
	"testing"
)

type countingReaderAt struct {
	data  []byte
	reads int
}

func (r *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	r.reads++
	return bytes.NewReader(r.data).ReadAt(p, off)
}

func TestBufferedReaderAtMatchesPlainReader(t *testing.T) {
	data := make([]byte, 100*1024)
	rand.New(rand.NewSource(1)).Read(data)

	plain := bytes.NewReader(data)
	src := &countingReaderAt{data: data}
	buffered := &BufferedReaderAt{src: src, blockSize: 4096, cap: 4, cache: make(map[int64][]byte)}

	cases := []struct {
		off int64
		n   int
	}{
		{0, 10},
		{1, 4095},
		{4096, 512},
		{8000, 9000},
		{int64(len(data) - 20), 20},
	}
	for _, c := range cases {
		want := make([]byte, c.n)
		if _, err := plain.ReadAt(want, c.off); err != nil {
			t.Fatalf("plain ReadAt(off=%d, n=%d): %v", c.off, c.n, err)
		}
		got := make([]byte, c.n)
		if _, err := buffered.ReadAt(got, c.off); err != nil {
			t.Fatalf("buffered ReadAt(off=%d, n=%d): %v", c.off, c.n, err)
		}
		if !bytes.Equal(want, got) {
			t.Errorf("ReadAt(off=%d, n=%d): bytes differ from the plain reader", c.off, c.n)
		}
	}
}

func TestBufferedReaderAtCachesBlocks(t *testing.T) {
	data := make([]byte, 4096*3)
	src := &countingReaderAt{data: data}
	buffered := &BufferedReaderAt{src: src, blockSize: 4096, cap: 4, cache: make(map[int64][]byte)}

	buf := make([]byte, 10)
	for i := 0; i < 5; i++ {
		if _, err := buffered.ReadAt(buf, 100); err != nil {
			t.Fatal(err)
		}
	}
	if src.reads != 1 {
		t.Errorf("re-reading the same block cost %d underlying reads, want 1", src.reads)
	}
}

func TestBufferedReaderAtEOF(t *testing.T) {
	data := make([]byte, 100)
	buffered := &BufferedReaderAt{src: &countingReaderAt{data: data}, blockSize: 64, cap: 4, cache: make(map[int64][]byte)}

	buf := make([]byte, 20)
	n, err := buffered.ReadAt(buf, 90)
	if err != io.EOF {
		t.Errorf("got err %v, want io.EOF", err)
	}
	if n != 10 {
		t.Errorf("got n=%d, want 10", n)
	}
}
