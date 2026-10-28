package disk

import (
	"fmt"
	"io"
)

// Window is a byte range a RestrictedReadWriterAt will let WriteAt touch.
type Window struct {
	Offset int64
	Size   int64
}

func (w Window) contains(off, n int64) bool {
	return off >= w.Offset && off+n <= w.Offset+w.Size
}

type restrictedTarget interface {
	io.ReaderAt
	io.WriterAt
}

// RestrictedReadWriterAt refuses any write that doesn't fall entirely inside one of its
// windows; reads always pass through. It guards against a reset or patch bug reaching
// past the one card area it was supposed to touch.
type RestrictedReadWriterAt struct {
	inner   restrictedTarget
	allowed []Window
}

func NewRestrictedReadWriterAt(inner restrictedTarget, allowed []Window) *RestrictedReadWriterAt {
	return &RestrictedReadWriterAt{inner: inner, allowed: allowed}
}

func (r *RestrictedReadWriterAt) ReadAt(p []byte, off int64) (int, error) {
	return r.inner.ReadAt(p, off)
}

func (r *RestrictedReadWriterAt) WriteAt(p []byte, off int64) (int, error) {
	n := int64(len(p))
	for _, w := range r.allowed {
		if w.contains(off, n) {
			return r.inner.WriteAt(p, off)
		}
	}
	return 0, fmt.Errorf("write at 0x%x, %d bytes, falls outside every allowed window; refusing", off, n)
}
