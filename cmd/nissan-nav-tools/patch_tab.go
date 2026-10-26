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

func newScratchFile() (string, error) {
	f, err := os.CreateTemp("", "nnt-card-*.img")
	if err != nil {
		return "", err
	}
	path := f.Name()
	f.Close()
	return path, nil
}

const (
	targetCopyLabel = "Copy"
	targetFileLabel = "This image file"
	targetCardLabel = "SD card in a reader"
)

type patchTarget int

const (
	targetCopy patchTarget = iota
	targetFile
	targetCard
)

func patchTargetFor(label string) patchTarget {
	switch label {
	case targetFileLabel:
		return targetFile
	case targetCardLabel:
		return targetCard
	default:
		return targetCopy
	}
}

func confirmInPlace(what string) (backupFirst, proceed bool) {
	err := zenity.Question(
		fmt.Sprintf("This writes straight to %s. There is no undo.\n\nOnly continue if you have a backup.", what),
		zenity.Title("Patch in place"),
		zenity.WarningIcon,
		zenity.OKLabel("Yes, patch"),
		zenity.ExtraButton("Back up first"),
		zenity.CancelLabel("Cancel"),
		zenity.DefaultCancel())
	switch err {
	case nil:
		return false, true
	case zenity.ErrExtraButton:
		return true, true
	default:
		return false, false
	}
}

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
			versionLabel.Show()
		case targetCard:
			fileRow.Hide()
			outRow.Hide()
			diskRow.Show()
			versionLabel.Hide()
		default:
			fileRow.Show()
			outRow.Show()
			diskRow.Hide()
			versionLabel.Show()
		}
	}
	targetRadio.OnChanged = func(string) { updateVisibility() }
	updateVisibility()

	spinner := widget.NewProgressBarInfinite()
	spinner.Hide()

	var runBtn *widget.Button
	runBtn = widget.NewButton("Run", func() {
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
		switch patchTargetFor(targetRadio.Selected) {
		case targetFile:
			runFilePatch(inPath, detected, selected, runBtn, spinner, status, clearLog, appendLog)
		case targetCard:
			if diskSelect.SelectedIndex() < 0 {
				status.SetText("Pick a card first.")
				return
			}
			runCardPatch(disks[diskSelect.SelectedIndex()], selected, runBtn, spinner, status, clearLog, appendLog)
		default:
			runCopyPatch(inPath, outEntry, detected, selected, runBtn, spinner, status, clearLog, appendLog)
		}
	})
	runBtn.Importance = widget.HighImportance

	return container.NewVBox(
		widget.NewLabelWithStyle("Target", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		targetRadio,
		widget.NewLabelWithStyle("Input", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		fileRow,
		diskRow,
		versionLabel,
		outRow,
		widget.NewLabelWithStyle("Patches", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		rowsBox,
		runBtn,
		spinner,
		status,
		logView,
	)
}

func runCopyPatch(inPath string, outEntry *widget.Entry, detected appVersion, selected []int,
	runBtn *widget.Button, spinner *widget.ProgressBarInfinite, status *widget.Label, clearLog func(), appendLog func(string)) {
	if inPath == "" {
		status.SetText("Choose an input image first.")
		return
	}
	out := outEntry.Text
	if out == "" {
		out = defaultOutputPath(inPath, ".patched")
		outEntry.SetText(out)
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
}

func runFilePatch(inPath string, detected appVersion, selected []int,
	runBtn *widget.Button, spinner *widget.ProgressBarInfinite, status *widget.Label, clearLog func(), appendLog func(string)) {
	if inPath == "" {
		status.SetText("Choose an image file first.")
		return
	}
	if detected.name != "" && !detected.supported {
		status.SetText(fmt.Sprintf("No patch for app version %q on this card; nothing written.", detected.name))
		return
	}

	runBtn.Disable()
	spinner.Show()
	clearLog()
	status.SetText("Confirming...")
	go func() {
		defer fyne.Do(func() { runBtn.Enable(); spinner.Hide() })

		backupFirst, proceed := confirmInPlace(inPath)
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

		fyne.Do(func() { status.SetText("Patching...") })
		if err := patchInPlaceFile(inPath, selected, appendLog); err != nil {
			fyne.Do(func() { status.SetText("Failed: " + err.Error()) })
			return
		}
		fyne.Do(func() { status.SetText("Done: " + inPath) })
	}()
}

// Shows the in-place warning before touching the card, then reads only the raw area before partition 1, never a full copy.
func runCardPatch(d disk.Info, selected []int,
	runBtn *widget.Button, spinner *widget.ProgressBarInfinite, status *widget.Label, clearLog func(), appendLog func(string)) {
	if err := refuseUnlessCard(d); err != nil {
		status.SetText(err.Error())
		return
	}

	backupFirst, proceed := confirmInPlace(d.Identifier)
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
			if _, err := backupJob(d, dest, 0, nil, appendLog); err != nil {
				fyne.Do(func() { status.SetText("Failed: " + err.Error()) })
				return
			}
			appendLog("Backup written: " + dest)
		}

		fyne.Do(func() { status.SetText("Unmounting the card...") })
		if err := disk.Unmount(d.Identifier); err != nil {
			fyne.Do(func() { status.SetText("Failed: " + err.Error()) })
			return
		}

		fyne.Do(func() { status.SetText("Reading the card...") })
		ranges, err := planCardPatchDevice(d, selected, appendLog)
		if err != nil {
			fyne.Do(func() { status.SetText("Failed: " + err.Error()) })
			return
		}

		fyne.Do(func() { status.SetText("Patching...") })
		if err := applyCardPatch(d, ranges, appendLog); err != nil {
			fyne.Do(func() { status.SetText("Failed: " + err.Error()) })
			return
		}
		fyne.Do(func() { status.SetText("Done: " + d.Identifier) })
	}()
}
