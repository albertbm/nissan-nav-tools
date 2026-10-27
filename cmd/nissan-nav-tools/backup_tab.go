package main

import (
	"fmt"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"nissan-nav-tools/internal/disk"
	"nissan-nav-tools/internal/humansize"
)

func newBackupTab(w fyne.Window) fyne.CanvasObject {
	logView, appendLog, clearLog := newLog()
	status := widget.NewLabel("Pick a card to back up.")
	status.Wrapping = fyne.TextWrapWord

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

	var outPath string
	outLabel := widget.NewLabel("No destination chosen")
	outLabel.Wrapping = fyne.TextWrapWord
	chooseOut := widget.NewButton("Choose destination...", func() {
		name := "backup.img"
		if i := diskSelect.SelectedIndex(); i >= 0 {
			name = disks[i].Identifier + ".img"
		}
		start := filepath.Join(fallbackOutputDir(), name)
		pickSaveFile(start, appendLog, func(path string) {
			outPath = path
			outLabel.SetText(outPath)
		})
	})

	hashCheck := newHashCheck()
	trimCheck := widget.NewCheck("Trim unused space after the last partition", nil)
	trimCheck.SetChecked(true)

	progress := widget.NewProgressBar()
	progress.Hide()
	speedLabel := widget.NewLabel("")
	speedLabel.Hide()

	var runBtn *widget.Button
	runBtn = widget.NewButton("Back up", func() {
		clearLog()
		if diskSelect.SelectedIndex() < 0 {
			status.SetText("Pick a disk first.")
			return
		}
		if outPath == "" {
			status.SetText("Choose where the backup goes first.")
			return
		}
		d := disks[diskSelect.SelectedIndex()]

		runBtn.Disable()
		progress.SetValue(0)
		progress.Show()
		status.SetText("Reading the card...")
		go func() {
			defer fyne.Do(func() { runBtn.Enable(); progress.Hide(); speedLabel.Hide() })
			target := trimTarget(d, trimCheck.Checked, appendLog)
			total := d.Size
			if target > 0 {
				total = target
			}
			st := newSpeedTracker(total, progress, speedLabel)
			size, err := backupJob(d, outPath, target, st.onProgress, appendLog)
			if err != nil {
				fyne.Do(func() { status.SetText("Failed: " + err.Error()) })
				return
			}
			appendLog(fmt.Sprintf("%s written, %s", outPath, humansize.Format(size)))
			appendLog(st.finish(size))
			var hashProgress func(int64)
			if hashCheck.Checked {
				hashProgress = newSpeedTracker(size, progress, speedLabel).onProgress
			}
			if err := logHash(outPath, hashCheck.Checked, hashProgress, appendLog); err != nil {
				fyne.Do(func() { status.SetText("Failed: " + err.Error()) })
				return
			}
			fyne.Do(func() { status.SetText("Done: " + outPath) })
		}()
	})
	runBtn.Importance = widget.HighImportance

	return container.NewVBox(
		widget.NewLabelWithStyle("Card", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewBorder(nil, nil, nil, refreshBtn, diskSelect),
		container.NewBorder(nil, nil, nil, chooseOut, outLabel),
		trimCheck,
		hashCheck,
		runBtn,
		progress,
		speedLabel,
		status,
		logView,
	)
}
