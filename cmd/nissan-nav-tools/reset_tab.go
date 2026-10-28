package main

import (
	"fmt"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/ncruces/zenity"

	"nissan-nav-tools/internal/disk"
	"nissan-nav-tools/internal/humansize"
)

func newResetTab(w fyne.Window) fyne.CanvasObject {
	logView, appendLog, clearLog := newLog()
	hint := widget.NewLabel("Removes the previous owner's data: phonebook, pairings, history, settings.")
	hint.Wrapping = fyne.TextWrapWord
	status := widget.NewLabel("Pick an image file to reset.")
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
			outEntry.SetText(defaultOutputPath(inPath, ".reset"))
		})
	})

	chooseOut := widget.NewButton("Choose output...", func() {
		pickSaveFile(defaultOutputPath(inPath, ".reset"), appendLog, func(path string) {
			outEntry.SetText(path)
		})
	})

	fileRow := container.NewBorder(nil, nil, nil, chooseIn, inLabel)
	outRow := container.NewBorder(nil, nil, nil, chooseOut, outEntry)

	var disks []disk.Info
	diskSelect := widget.NewSelect(nil, nil)
	diskSelect.PlaceHolder = "No card found yet"
	refreshBtn := widget.NewButton("Refresh", func() {
		found, err := disk.List()
		if err != nil {
			status.SetText("Could not list disks: " + err.Error())
			return
		}
		disks = found
		labels := make([]string, len(found))
		for i, d := range found {
			name := d.Name
			if name == "" {
				name = "(unnamed)"
			}
			labels[i] = fmt.Sprintf("%s  %s  %s", d.Identifier, name, humansize.Format(d.Size))
		}
		diskSelect.Options = labels
		diskSelect.ClearSelected()
		if len(labels) == 0 {
			for _, l := range disk.Report() {
				appendLog(l)
			}
			status.SetText("No external, removable disk found. Insert the card and press Refresh.")
		} else {
			status.SetText(fmt.Sprintf("%d disk(s) found.", len(labels)))
		}
	})
	diskRow := container.NewBorder(nil, nil, nil, refreshBtn, diskSelect)

	targetRadio := widget.NewRadioGroup([]string{targetCopyLabel, targetFileLabel, targetCardLabel}, nil)
	targetRadio.Horizontal = true
	targetRadio.SetSelected(targetCopyLabel)

	updateVisibility := func() {
		switch patchTargetFor(targetRadio.Selected) {
		case targetFile:
			fileRow.Show()
			outRow.Hide()
			diskRow.Hide()
		case targetCard:
			fileRow.Hide()
			outRow.Hide()
			diskRow.Show()
		default:
			fileRow.Show()
			outRow.Show()
			diskRow.Hide()
		}
	}
	targetRadio.OnChanged = func(string) { updateVisibility() }
	updateVisibility()

	hashCheck := newHashCheck()

	spinner := widget.NewProgressBarInfinite()
	spinner.Hide()
	hashBar := widget.NewProgressBar()
	hashBar.Hide()
	speedLabel := widget.NewLabel("")
	speedLabel.Hide()

	var runBtn *widget.Button
	runBtn = widget.NewButton("Reset", func() {
		switch patchTargetFor(targetRadio.Selected) {
		case targetFile:
			runFileReset(inPath, runBtn, spinner, status, clearLog, appendLog)
		case targetCard:
			if diskSelect.SelectedIndex() < 0 {
				status.SetText("Pick a card first.")
				return
			}
			runCardReset(disks[diskSelect.SelectedIndex()], runBtn, spinner, status, clearLog, appendLog)
		default:
			runCopyReset(inPath, outEntry, hashCheck, runBtn, spinner, hashBar, speedLabel, status, clearLog, appendLog)
		}
	})
	runBtn.Importance = widget.HighImportance

	return container.NewVBox(
		widget.NewLabelWithStyle("Target", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		targetRadio,
		widget.NewLabelWithStyle("Input", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		fileRow,
		diskRow,
		outRow,
		hint,
		hashCheck,
		runBtn,
		spinner,
		hashBar,
		speedLabel,
		status,
		logView,
	)
}

func runCopyReset(inPath string, outEntry *widget.Entry, hashCheck *widget.Check,
	runBtn *widget.Button, spinner *widget.ProgressBarInfinite, hashBar *widget.ProgressBar, speedLabel *widget.Label,
	status *widget.Label, clearLog func(), appendLog func(string)) {
	if inPath == "" {
		status.SetText("Choose an image file first.")
		return
	}
	out := outEntry.Text
	if out == "" {
		out = defaultOutputPath(inPath, ".reset")
		outEntry.SetText(out)
	}

	runBtn.Disable()
	spinner.Show()
	clearLog()
	status.SetText("Resetting...")
	go func() {
		defer fyne.Do(func() { runBtn.Enable(); spinner.Hide(); hashBar.Hide(); speedLabel.Hide() })
		if err := resetCopy(inPath, out, appendLog); err != nil {
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
}

func runFileReset(inPath string,
	runBtn *widget.Button, spinner *widget.ProgressBarInfinite, status *widget.Label, clearLog func(), appendLog func(string)) {
	if inPath == "" {
		status.SetText("Choose an image file first.")
		return
	}

	runBtn.Disable()
	spinner.Show()
	clearLog()
	status.SetText("Confirming...")
	go func() {
		defer fyne.Do(func() { runBtn.Enable(); spinner.Hide() })

		backupFirst, proceed := confirmInPlace(inPath, "reset")
		if !proceed {
			fyne.Do(func() { status.SetText("Cancelled; nothing written.") })
			return
		}
		if backupFirst {
			dest, err := zenity.SelectFileSave(zenity.Filename(defaultOutputPath(inPath, ".backup")), zenity.ConfirmOverwrite(), imgFilters)
			if err != nil {
				msg := "Cancelled; nothing written."
				if err != zenity.ErrCanceled {
					msg = "Failed: " + err.Error()
				}
				fyne.Do(func() { status.SetText(msg) })
				return
			}
			if err := backupCopy(inPath, dest, appendLog); err != nil {
				fyne.Do(func() { status.SetText("Failed: " + err.Error()) })
				return
			}
		}

		fyne.Do(func() { status.SetText("Resetting...") })
		rep, err := resetInPlaceFile(inPath, appendLog)
		if err != nil {
			fyne.Do(func() { status.SetText("Failed: " + err.Error()) })
			return
		}
		logResetReport(rep, appendLog)
		fyne.Do(func() { status.SetText("Done: " + inPath) })
	}()
}

// Shows the in-place warning before touching the card, then resets it through its raw device handle.
func runCardReset(d disk.Info,
	runBtn *widget.Button, spinner *widget.ProgressBarInfinite, status *widget.Label, clearLog func(), appendLog func(string)) {
	if err := refuseUnlessCard(d); err != nil {
		status.SetText(err.Error())
		return
	}

	backupFirst, proceed := confirmInPlace(d.Identifier, "reset")
	if !proceed {
		status.SetText("Cancelled; nothing written.")
		return
	}

	runBtn.Disable()
	spinner.Show()
	clearLog()
	go func() {
		defer fyne.Do(func() { runBtn.Enable(); spinner.Hide() })

		if backupFirst {
			dest, err := zenity.SelectFileSave(
				zenity.Filename(filepath.Join(fallbackOutputDir(), d.Identifier+".backup.img")),
				zenity.ConfirmOverwrite(), imgFilters)
			if err != nil {
				msg := "Cancelled; nothing written."
				if err != zenity.ErrCanceled {
					msg = "Failed: " + err.Error()
				}
				fyne.Do(func() { status.SetText(msg) })
				return
			}
			fyne.Do(func() { status.SetText("Backing up the card...") })
			if _, err := backupJob(d, dest, false, nil, nil, appendLog); err != nil {
				fyne.Do(func() { status.SetText("Failed: " + err.Error()) })
				return
			}
			appendLog("Backup written: " + dest)
		}

		fyne.Do(func() { status.SetText("Resetting the card...") })
		rep, err := resetCardInPlace(d, appendLog)
		if err != nil {
			fyne.Do(func() { status.SetText("Failed: " + err.Error()) })
			return
		}
		logResetReport(rep, appendLog)
		fyne.Do(func() { status.SetText("Done: " + d.Identifier) })
	}()
}
