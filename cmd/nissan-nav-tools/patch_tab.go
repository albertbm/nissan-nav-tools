package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// newPatchTab refuses app versions with no patch.
func newPatchTab(w fyne.Window) fyne.CanvasObject {
	logView, appendLog, clearLog := newLog()
	status := widget.NewLabel("Pick an image file to patch.")
	status.Wrapping = fyne.TextWrapWord
	versionLabel := widget.NewLabel("App version: not checked yet")
	versionLabel.Wrapping = fyne.TextWrapWord

	var inPath string
	var detected appVersion
	inLabel := widget.NewLabel("No image file chosen")
	inLabel.Wrapping = fyne.TextWrapWord

	outEntry := widget.NewEntry()
	outEntry.SetPlaceHolder("output path")

	checks := make([]*widget.Check, len(patchRows))
	rowsBox := container.NewVBox()
	for i, r := range patchRows {
		i, r := i, r
		checks[i] = widget.NewCheck(r.title, nil)
		rowsBox.Add(checks[i])
	}

	chooseIn := widget.NewButton("Choose input...", func() {
		pickFile("", appendLog, func(path string) {
			inPath = path
			inLabel.SetText(inPath)
			if outEntry.Text == "" {
				outEntry.SetText(defaultOutputPath(inPath, ".patched"))
			}
			versionLabel.SetText("App version: checking...")
			go func() {
				detected = detectAppVersion(inPath)
				fyne.Do(func() {
					switch {
					case detected.err != nil:
						versionLabel.SetText("App version: could not identify a nav image (" + detected.err.Error() + ")")
					case detected.supported:
						versionLabel.SetText("App version: " + detected.name + " (patch available)")
					default:
						versionLabel.SetText("App version: " + detected.name + " (no patch for this version)")
					}
				})
			}()
		})
	})

	chooseOut := widget.NewButton("Choose output...", func() {
		pickSaveFile(defaultOutputPath(inPath, ".patched"), appendLog, func(path string) {
			outEntry.SetText(path)
		})
	})

	spinner := widget.NewProgressBarInfinite()
	spinner.Hide()

	var runBtn *widget.Button
	runBtn = widget.NewButton("Run", func() {
		if inPath == "" {
			status.SetText("Choose an input image first.")
			return
		}
		out := outEntry.Text
		if out == "" {
			out = defaultOutputPath(inPath, ".patched")
			outEntry.SetText(out)
		}
		var selected []int
		for i, c := range checks {
			if c.Checked {
				selected = append(selected, i)
			}
		}
		if len(selected) == 0 {
			status.SetText("Tick at least one patch.")
			return
		}
		if detected.name != "" && !detected.supported {
			status.SetText(fmt.Sprintf("No patch for app version %q on this card; nothing written.", detected.name))
			return
		}

		runBtn.Disable()
		spinner.Show()
		clearLog()
		status.SetText("Patching...")
		go func() {
			defer fyne.Do(func() { runBtn.Enable(); spinner.Hide() })
			if err := patchJob(inPath, out, selected, appendLog); err != nil {
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
		versionLabel,
		container.NewBorder(nil, nil, nil, chooseOut, outEntry),
		widget.NewLabelWithStyle("Patches", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		rowsBox,
		runBtn,
		spinner,
		status,
		logView,
	)
}
