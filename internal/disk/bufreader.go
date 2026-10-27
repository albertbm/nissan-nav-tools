package disk

import "io"

// BufBlockSize is the aligned block BufferedReaderAt reads and caches; a raw device on macOS refuses an unaligned read.
const BufBlockSize = 4 << 20

type BufferedReaderAt struct {
	src       io.ReaderAt
	blockSize int64
	cap       int
	cache     map[int64][]byte
	order     []int64
}

func NewBufferedReaderAt(src io.ReaderAt) *BufferedReaderAt {
	return &BufferedReaderAt{src: src, blockSize: BufBlockSize, cap: 4, cache: make(map[int64][]byte)}
}

func (b *BufferedReaderAt) block(start int64) ([]byte, error) {
	if blk, ok := b.cache[start]; ok {
		return blk, nil
	}
	buf := make([]byte, b.blockSize)
	n, err := b.src.ReadAt(buf, start)
	if err != nil && err != io.EOF {
		return nil, err
	}
	buf = buf[:n]
	b.cache[start] = buf
	b.order = append(b.order, start)
	if len(b.order) > b.cap {
		delete(b.cache, b.order[0])
		b.order = b.order[1:]
	}
	return buf, nil
}

func (b *BufferedReaderAt) ReadAt(p []byte, off int64) (int, error) {
	total := 0
	for total < len(p) {
		abs := off + int64(total)
		start := abs / b.blockSize * b.blockSize
		blk, err := b.block(start)
		if err != nil {
			return total, err
		}
		rel := int(abs - start)
		if rel < len(blk) {
			total += copy(p[total:], blk[rel:])
		}
		if len(blk) < int(b.blockSize) {
			if total < len(p) {
				return total, io.EOF
			}
			return total, nil
		}
	}
	return total, nil
}
