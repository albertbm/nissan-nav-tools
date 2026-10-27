package main

import (
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func newAnonymizeTab(w fyne.Window) fyne.CanvasObject {
	logView, appendLog, clearLog := newLog()
	status := widget.NewLabel("Pick an image file to anonymize.")
	status.Wrapping = fyne.TextWrapWord

	var inPath string
	inLabel := widget.NewLabel("No image file chosen")
	inLabel.Wrapping = fyne.TextWrapWord

	outEntry := widget.NewEntry()
	outEntry.SetPlaceHolder("output path")

	chooseIn := widget.NewButton("Choose image...", func() {
		pickFile("", appendLog, func(path string) {
			inPath = path
			inLabel.SetText(inPath)
			outEntry.SetText(defaultOutputPath(inPath, ".anon"))
		})
	})

	chooseOut := widget.NewButton("Choose output...", func() {
		pickSaveFile(defaultOutputPath(inPath, ".anon"), appendLog, func(path string) {
			outEntry.SetText(path)
		})
	})

	hashCheck := newHashCheck()

	spinner := widget.NewProgressBarInfinite()
	spinner.Hide()
	hashBar := widget.NewProgressBar()
	hashBar.Hide()
	speedLabel := widget.NewLabel("")
	speedLabel.Hide()

	var runBtn *widget.Button
	runBtn = widget.NewButton("Anonymize", func() {
		if inPath == "" {
			status.SetText("Choose an image file first.")
			return
		}
		out := outEntry.Text
		if out == "" {
			out = defaultOutputPath(inPath, ".anon")
			outEntry.SetText(out)
		}

		runBtn.Disable()
		spinner.Show()
		clearLog()
		status.SetText("Anonymizing...")
		go func() {
			defer fyne.Do(func() { runBtn.Enable(); spinner.Hide(); hashBar.Hide(); speedLabel.Hide() })
			if err := anonymizeCopy(inPath, out, appendLog); err != nil {
				fyne.Do(func() { status.SetText("Failed: " + err.Error()) })
				return
			}
			var hashProgress func(int64)
			if hashCheck.Checked {
				size := int64(0)
				if fi, err := os.Stat(out); err == nil {
					size = fi.Size()
				}
				hashProgress = newSpeedTracker(size, hashBar, speedLabel).onProgress
			}
			if err := logHash(out, hashCheck.Checked, hashProgress, appendLog); err != nil {
				fyne.Do(func() { status.SetText("Failed: " + err.Error()) })
				return
			}
			fyne.Do(func() { status.SetText("Done: " + out) })
		}()
	})
	runBtn.Importance = widget.HighImportance

	return container.NewVBox(
		widget.NewLabelWithStyle("Input", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewBorder(nil, nil, nil, chooseIn, inLabel),
		container.NewBorder(nil, nil, nil, chooseOut, outEntry),
		hashCheck,
		runBtn,
		spinner,
		hashBar,
		speedLabel,
		status,
		logView,
	)
}
