package progress

import (
	"testing"
	"time"
)

func TestSteadyRate(t *testing.T) {
	tr := New(100_000_000)
	start := time.Now()
	var st Stats
	for i := int64(1); i <= 10; i++ {
		st = tr.at(i*1_000_000, start.Add(time.Duration(i)*time.Second))
	}
	if !st.HasSpeed {
		t.Fatalf("expected a speed after 10 steady seconds")
	}
	if got := st.Speed; got < 900_000 || got > 1_100_000 {
		t.Fatalf("speed = %v, want close to 1,000,000 B/s", got)
	}
	if !st.HasETA {
		t.Fatalf("expected an ETA with a known total")
	}
	if got := st.ETA.Round(time.Second); got < 88*time.Second || got > 92*time.Second {
		t.Fatalf("eta = %v, want close to 90s", got)
	}
}

func TestBurstThenStall(t *testing.T) {
	tr := New(0)
	start := time.Now()
	st := tr.at(50_000_000, start.Add(3*time.Second))
	if !st.HasSpeed {
		t.Fatalf("expected a speed after the burst")
	}
	burstSpeed := st.Speed
	st = tr.at(50_000_000, start.Add(30*time.Second))
	if st.Speed != burstSpeed {
		t.Fatalf("a stall with no new bytes changed the rate: %v -> %v", burstSpeed, st.Speed)
	}
}

func TestUnknownTotal(t *testing.T) {
	tr := New(0)
	start := time.Now()
	tr.at(1_000_000, start.Add(1*time.Second))
	st := tr.at(2_000_000, start.Add(2*time.Second))
	if st.HasETA {
		t.Fatalf("an unknown (zero) total must never produce an ETA")
	}
	st = New(-1).at(2_000_000, start.Add(2*time.Second))
	if st.HasETA {
		t.Fatalf("a negative total must never produce an ETA")
	}
}

func TestFirstSecondHasNoETA(t *testing.T) {
	tr := New(1_000_000_000)
	start := time.Now()
	st := tr.at(10_000_000, start.Add(500*time.Millisecond))
	if st.HasSpeed || st.HasETA {
		t.Fatalf("under the settle window, speed and ETA must both stay blank")
	}
	st = tr.at(20_000_000, start.Add(1900*time.Millisecond))
	if st.HasSpeed || st.HasETA {
		t.Fatalf("just under 2s, speed and ETA must still be blank")
	}
	st = tr.at(30_000_000, start.Add(2100*time.Millisecond))
	if !st.HasSpeed {
		t.Fatalf("past the 2s settle window, a speed should be available")
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0 s"},
		{45 * time.Second, "45 s"},
		{59500 * time.Millisecond, "1 min 0 s"},
		{190 * time.Second, "3 min 10 s"},
		{3*time.Hour + 5*time.Minute + 2*time.Second, "3 h 5 min 2 s"},
	}
	for _, c := range cases {
		if got := FormatDuration(c.d); got != c.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestFormatSpeed(t *testing.T) {
	if got, want := FormatSpeed(38_200_000), "38.2 MB/s"; got != want {
		t.Errorf("FormatSpeed() = %q, want %q", got, want)
	}
}

func TestSummary(t *testing.T) {
	got := Summary(402*time.Second, 16_000_000_000)
	want := "Done in 6 min 42 s, average 39.8 MB/s"
	if got != want {
		t.Errorf("Summary() = %q, want %q", got, want)
	}
}

func TestCountingReaderAtHighWaterMark(t *testing.T) {
	var seen []int64
	cr := NewCountingReaderAt(zeroReaderAt{}, func(n int64) { seen = append(seen, n) })
	buf := make([]byte, 10)
	cr.ReadAt(buf, 0)
	cr.ReadAt(buf, 20) // a forward jump, as a signature scan makes
	cr.ReadAt(buf, 5)  // a re-read inside the already-seen range
	want := []int64{10, 30}
	if len(seen) != len(want) {
		t.Fatalf("progress calls = %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("progress calls = %v, want %v", seen, want)
		}
	}
}

type zeroReaderAt struct{}

func (zeroReaderAt) ReadAt(p []byte, off int64) (int, error) { return len(p), nil }
