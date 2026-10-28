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

func TestSpeedIsTheWholeRunAverage(t *testing.T) {
	tr := New(0)
	start := time.Now()
	var st Stats
	for i := int64(1); i <= 60; i++ { // 10 MB/s for a minute
		st = tr.at(i*10_000_000, start.Add(time.Duration(i)*time.Second))
	}
	for i := int64(1); i <= 40; i++ { // then 2 MB/s, as a card's cache fills
		st = tr.at(600_000_000+i*2_000_000, start.Add(time.Duration(60+i)*time.Second))
	}
	// 680,000,000 bytes over 100s: the honest average, not the recent rate.
	if got := st.Speed; got < 6_700_000 || got > 6_900_000 {
		t.Fatalf("speed = %v, want about 6,800,000 B/s (the whole-run average)", got)
	}
}

func TestUnknownTotal(t *testing.T) {
	tr := New(0)
	start := time.Now()
	tr.at(1_000_000, start.Add(1*time.Second))
	st := tr.at(2_000_000, start.Add(4*time.Second))
	if st.HasETA {
		t.Fatalf("an unknown (zero) total must never produce an ETA")
	}
	st = New(-1).at(2_000_000, start.Add(4*time.Second))
	if st.HasETA {
		t.Fatalf("a negative total must never produce an ETA")
	}
}

func TestFirstThreeSecondsHaveNoSpeedOrETA(t *testing.T) {
	tr := New(1_000_000_000)
	start := time.Now()
	st := tr.at(10_000_000, start.Add(1*time.Second))
	if st.HasSpeed || st.HasETA {
		t.Fatalf("under the settle window, speed and ETA must both stay blank")
	}
	st = tr.at(20_000_000, start.Add(2*time.Second))
	if st.HasSpeed || st.HasETA {
		t.Fatalf("still under 3s, speed and ETA must stay blank")
	}
	st = tr.at(30_000_000, start.Add(4*time.Second))
	if !st.HasSpeed {
		t.Fatalf("past the 3s settle window, a speed should be available")
	}
}

func TestBurstyCallbacksDontMoveTheSpeedWithinATick(t *testing.T) {
	tr := New(1_000_000_000)
	start := time.Now()
	tr.at(40_000_000, start.Add(4*time.Second))
	first := tr.at(40_000_000, start.Add(4*time.Second))

	// several callbacks landing inside the same wall-clock second
	for _, d := range []time.Duration{100, 300, 600, 900} {
		got := tr.at(90_000_000, start.Add(4*time.Second+d*time.Millisecond))
		if got != first {
			t.Fatalf("a callback inside the same tick moved the stats: got %+v, want %+v", got, first)
		}
	}

	next := tr.at(90_000_000, start.Add(5*time.Second))
	if next == first {
		t.Fatalf("a callback a full tick later should have moved the stats")
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

func TestLineFormat(t *testing.T) {
	s := Stats{Done: 42, Total: 100, Speed: 36_800_000, HasSpeed: true, ETA: 190 * time.Second, HasETA: true}
	want := "42% · 36.8 MB/s · 3 min 10 s left"
	if got := s.Line(); got != want {
		t.Errorf("Line() = %q, want %q", got, want)
	}
}

func TestLineRoundsETAToTenSecondsOverAMinute(t *testing.T) {
	s := Stats{Done: 1, Total: 100, Speed: 1_000_000, HasSpeed: true, ETA: 64 * time.Second, HasETA: true}
	if got, want := s.Line(), "1% · 1.0 MB/s · 1 min 0 s left"; got != want {
		t.Errorf("Line() = %q, want %q", got, want)
	}
}

func TestLineKeepsExactSecondsUnderAMinute(t *testing.T) {
	s := Stats{Done: 1, Total: 100, Speed: 1_000_000, HasSpeed: true, ETA: 47 * time.Second, HasETA: true}
	if got, want := s.Line(), "1% · 1.0 MB/s · 47 s left"; got != want {
		t.Errorf("Line() = %q, want %q", got, want)
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
