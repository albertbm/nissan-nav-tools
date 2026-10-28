package anonymize

import "testing"

func TestBoundsReportsTheExfatPartition(t *testing.T) {
	img := buildSynthCard(10*512 + 40*512)
	windows, err := Bounds(img, int64(len(img.data)))
	if err != nil {
		t.Fatalf("Bounds: %v", err)
	}
	if len(windows) != 1 {
		t.Fatalf("got %d window(s), want 1 (no front gap on an image this small): %+v", len(windows), windows)
	}
	const volLBA = 10
	if windows[0].Offset != volLBA*512 || windows[0].Size != 40*512 {
		t.Errorf("window = %+v, want offset %d size %d", windows[0], volLBA*512, 40*512)
	}
}

func TestBoundsIncludesTheFrontGapWhenPresent(t *testing.T) {
	img := buildSynthCard(10*512 + 40*512)
	grown := newMemImg(int(frontGapOffset + frontGapSize))
	copy(grown.data, img.data)

	windows, err := Bounds(grown, int64(len(grown.data)))
	if err != nil {
		t.Fatalf("Bounds: %v", err)
	}
	var sawGap bool
	for _, w := range windows {
		if w.Offset == frontGapOffset && w.Size == frontGapSize {
			sawGap = true
		}
	}
	if !sawGap {
		t.Errorf("front gap window missing: %+v", windows)
	}
}

func TestBoundsRefusesWithoutClarionSignature(t *testing.T) {
	img := newMemImg(4096)
	if _, err := Bounds(img, int64(len(img.data))); err == nil {
		t.Error("Bounds on a non-Clarion image: no error")
	}
}
