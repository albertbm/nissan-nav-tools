// Package progress tracks speed and time left.
package progress

import (
	"fmt"
	"io"
	"time"
)

// Blank for the first few seconds: too little data yet for an honest number.
const settleWindow = 3 * time.Second

// The displayed line changes no more than once per tick, so bursty callbacks can't make it flicker.
const tickInterval = time.Second

type Stats struct {
	Done     int64
	Total    int64
	Elapsed  time.Duration
	Speed    float64
	HasSpeed bool
	ETA      time.Duration
	HasETA   bool
}

type Tracker struct {
	total    int64
	start    time.Time
	lastTick time.Time
	cached   Stats
}

func New(total int64) *Tracker {
	return &Tracker{total: total, start: time.Now()}
}

func (t *Tracker) Update(done int64) Stats {
	return t.at(done, time.Now())
}

func (t *Tracker) at(done int64, now time.Time) Stats {
	if t.lastTick.IsZero() || now.Sub(t.lastTick) >= tickInterval {
		t.lastTick = now
		t.cached = t.compute(done, now)
	}
	return t.cached
}

// compute is the whole-run average: total bytes over total elapsed time.
func (t *Tracker) compute(done int64, now time.Time) Stats {
	elapsed := now.Sub(t.start)
	st := Stats{Done: done, Total: t.total, Elapsed: elapsed}
	if elapsed < settleWindow {
		return st
	}
	rate := float64(done) / elapsed.Seconds()
	if rate <= 0 {
		return st
	}
	st.Speed, st.HasSpeed = rate, true
	if t.total > 0 {
		remaining := float64(t.total - done)
		if remaining < 0 {
			remaining = 0
		}
		st.ETA, st.HasETA = time.Duration(remaining/rate*float64(time.Second)), true
	}
	return st
}

// MB/s (10^6), as card makers rate speed.
func FormatSpeed(bytesPerSec float64) string {
	return fmt.Sprintf("%.1f MB/s", bytesPerSec/1e6)
}

func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	secs := int64(d.Round(time.Second) / time.Second)
	if secs < 60 {
		return fmt.Sprintf("%d s", secs)
	}
	h, rem := secs/3600, secs%3600
	m, s := rem/60, rem%60
	if h > 0 {
		return fmt.Sprintf("%d h %d min %d s", h, m, s)
	}
	return fmt.Sprintf("%d min %d s", m, s)
}

// roundETA holds the countdown still: exact under a minute, to the nearest 10 s beyond it.
func roundETA(d time.Duration) time.Duration {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return d.Round(time.Second)
	}
	return d.Round(10 * time.Second)
}

func (s Stats) Line() string {
	pct := "-"
	if s.Total > 0 {
		pct = fmt.Sprintf("%d%%", 100*s.Done/s.Total)
	}
	if !s.HasSpeed {
		return pct
	}
	eta := "-"
	if s.HasETA {
		eta = FormatDuration(roundETA(s.ETA)) + " left"
	}
	return fmt.Sprintf("%s · %s · %s", pct, FormatSpeed(s.Speed), eta)
}

func Summary(elapsed time.Duration, totalBytes int64) string {
	var avg float64
	if elapsed > 0 {
		avg = float64(totalBytes) / elapsed.Seconds()
	}
	return fmt.Sprintf("Done in %s, average %s", FormatDuration(elapsed), FormatSpeed(avg))
}

type CountingReader struct {
	r          io.Reader
	n          int64
	onProgress func(int64)
}

func NewCountingReader(r io.Reader, onProgress func(int64)) *CountingReader {
	return &CountingReader{r: r, onProgress: onProgress}
}

func (c *CountingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.n += int64(n)
		if c.onProgress != nil {
			c.onProgress(c.n)
		}
	}
	return n, err
}

// Reads arrive out of order, so report the high-water mark.
type CountingReaderAt struct {
	r          io.ReaderAt
	high       int64
	onProgress func(int64)
}

func NewCountingReaderAt(r io.ReaderAt, onProgress func(int64)) *CountingReaderAt {
	return &CountingReaderAt{r: r, onProgress: onProgress}
}

func (c *CountingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.r.ReadAt(p, off)
	if end := off + int64(n); end > c.high {
		c.high = end
		if c.onProgress != nil {
			c.onProgress(c.high)
		}
	}
	return n, err
}
