// Package progress turns a running byte count into a smoothed speed and an ETA.
package progress

import (
	"fmt"
	"io"
	"time"
)

const settleWindow = 2 * time.Second
const emaAlpha = 0.35

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
	total   int64
	start   time.Time
	last    time.Time
	lastN   int64
	rate    float64
	hasRate bool
}

func New(total int64) *Tracker {
	now := time.Now()
	return &Tracker{total: total, start: now, last: now}
}

func (t *Tracker) Update(done int64) Stats {
	return t.at(done, time.Now())
}

func (t *Tracker) at(done int64, now time.Time) Stats {
	if dt := now.Sub(t.last).Seconds(); dt > 0 && done > t.lastN {
		sample := float64(done-t.lastN) / dt
		if !t.hasRate {
			t.rate, t.hasRate = sample, true
		} else {
			t.rate = emaAlpha*sample + (1-emaAlpha)*t.rate
		}
		t.last, t.lastN = now, done
	}

	elapsed := now.Sub(t.start)
	st := Stats{Done: done, Total: t.total, Elapsed: elapsed}
	if !t.hasRate || elapsed < settleWindow {
		return st
	}
	st.Speed, st.HasSpeed = t.rate, true
	if t.total > 0 && t.rate > 0 {
		remaining := float64(t.total - done)
		if remaining < 0 {
			remaining = 0
		}
		st.ETA, st.HasETA = time.Duration(remaining/t.rate*float64(time.Second)), true
	}
	return st
}

// FormatSpeed renders bytes/sec as MB/s (10^6), matching how card makers rate speed.
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
		eta = FormatDuration(s.ETA) + " left"
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

// CountingReaderAt wraps a ReaderAt read out of order, reporting the high-water mark rather than a raw byte count.
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
