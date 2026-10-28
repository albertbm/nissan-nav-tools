package disk

import "testing"

type memTarget struct{ data []byte }

func (m *memTarget) ReadAt(p []byte, off int64) (int, error) {
	return copy(p, m.data[off:]), nil
}

func (m *memTarget) WriteAt(p []byte, off int64) (int, error) {
	return copy(m.data[off:], p), nil
}

func TestRestrictedReadWriterAtAllowsAWriteInsideAWindow(t *testing.T) {
	inner := &memTarget{data: make([]byte, 1024)}
	r := NewRestrictedReadWriterAt(inner, []Window{{Offset: 100, Size: 200}})

	if _, err := r.WriteAt([]byte{1, 2, 3}, 150); err != nil {
		t.Fatalf("WriteAt inside the window: %v", err)
	}
	if inner.data[150] != 1 {
		t.Error("write inside the window never reached the inner target")
	}
}

func TestRestrictedReadWriterAtRefusesOutsideEveryWindow(t *testing.T) {
	inner := &memTarget{data: make([]byte, 1024)}
	r := NewRestrictedReadWriterAt(inner, []Window{{Offset: 100, Size: 200}})

	cases := []struct {
		name string
		off  int64
		n    int
	}{
		{"before the window", 0, 10},
		{"straddling the start", 95, 10},
		{"straddling the end", 290, 20},
		{"well past the end", 500, 10},
	}
	for _, c := range cases {
		if _, err := r.WriteAt(make([]byte, c.n), c.off); err == nil {
			t.Errorf("%s: write was not refused", c.name)
		}
	}
	for _, b := range inner.data {
		if b != 0 {
			t.Fatal("a refused write still reached the inner target")
		}
	}
}

func TestRestrictedReadWriterAtAllowsAnyConfiguredWindow(t *testing.T) {
	inner := &memTarget{data: make([]byte, 1024)}
	r := NewRestrictedReadWriterAt(inner, []Window{{Offset: 0, Size: 50}, {Offset: 600, Size: 50}})

	if _, err := r.WriteAt([]byte{9}, 10); err != nil {
		t.Errorf("write in the first window refused: %v", err)
	}
	if _, err := r.WriteAt([]byte{9}, 610); err != nil {
		t.Errorf("write in the second window refused: %v", err)
	}
}
