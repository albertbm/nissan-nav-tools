package main

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"

	"nissan-nav-tools/internal/progress"
)

type speedTracker struct {
	tr    *progress.Tracker
	start time.Time
	bar   *widget.ProgressBar
	label *widget.Label
	total int64
}

func newSpeedTracker(total int64, bar *widget.ProgressBar, label *widget.Label) *speedTracker {
	fyne.Do(func() {
		bar.SetValue(0)
		bar.Show()
		label.SetText("")
		label.Show()
	})
	return &speedTracker{tr: progress.New(total), start: time.Now(), bar: bar, label: label, total: total}
}

func (s *speedTracker) onProgress(done int64) {
	st := s.tr.Update(done)
	fyne.Do(func() {
		if s.total > 0 {
			s.bar.SetValue(float64(done) / float64(s.total))
		}
		s.label.SetText(st.Line())
	})
}

func (s *speedTracker) finish(totalBytes int64) string {
	fyne.Do(func() { s.label.Hide() })
	return progress.Summary(time.Since(s.start), totalBytes)
}
