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

func confirmRestore(card, image string) (backupFirst, proceed bool) {
	err := zenity.Question(
		fmt.Sprintf("This erases everything on %s and writes %s to it.\n\nThere is no undo. Only continue if you have a backup.", card, image),
		zenity.Title("Restore"),
		zenity.WarningIcon,
		zenity.OKLabel("Yes, restore"),
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

func newRestoreTab(w fyne.Window) fyne.CanvasObject {
	logView, appendLog, clearLog := newLog()
	status := widget.NewLabel("Pick an image file to restore.")
	status.Wrapping = fyne.TextWrapWord

	var inPath string
	var imgSize int64
	var disks []disk.Info
	inLabel := widget.NewLabel("No image file chosen")
	inLabel.Wrapping = fyne.TextWrapWord
	versionLabel := widget.NewLabel("App version: not checked yet")
	versionLabel.Wrapping = fyne.TextWrapWord
	sizesLabel := widget.NewLabel("Image size: unknown   Card size: unknown")
	sizesLabel.Wrapping = fyne.TextWrapWord

	var diskSelect *widget.Select
	updateSizes := func() {
		imgText := "unknown"
		if imgSize > 0 {
			imgText = humansize.Format(imgSize)
		}
		cardText := "unknown"
		if diskSelect != nil && diskSelect.SelectedIndex() >= 0 {
			cardText = humansize.Format(disks[diskSelect.SelectedIndex()].Size)
		}
		sizesLabel.SetText(fmt.Sprintf("Image size: %s   Card size: %s", imgText, cardText))
	}

	chooseIn := widget.NewButton("Choose image...", func() {
		pickFile("", appendLog, func(path string) {
			inPath = path
			inLabel.SetText(inPath)
			if fi, err := os.Stat(path); err == nil {
				imgSize = fi.Size()
			}
			updateSizes()
			versionLabel.SetText("App version: checking...")
			go func() {
				av := detectAppVersion(inPath)
				fyne.Do(func() {
					if av.err != nil {
						versionLabel.SetText("App version: unknown (" + av.err.Error() + ")")
					} else {
						versionLabel.SetText("App version: " + av.name)
					}
				})
			}()
		})
	})

	diskSelect = widget.NewSelect(nil, func(string) { updateSizes() })
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
		updateSizes()
		if len(labels) == 0 {
			for _, l := range disk.Report() {
				appendLog(l)
			}
			status.SetText("No card found. Insert one and press Refresh.")
		} else {
			status.SetText(fmt.Sprintf("%d disk(s) found.", len(labels)))
		}
	})

	verifyCheck := widget.NewCheck("Verify after writing", nil)
	verifyCheck.SetChecked(true)

	progress := widget.NewProgressBar()
	progress.Hide()
	speedLabel := widget.NewLabel("")
	speedLabel.Hide()

	var runBtn *widget.Button
	runBtn = widget.NewButton("Restore", func() {
		clearLog()
		if inPath == "" {
			status.SetText("Choose an image file first.")
			return
		}
		if diskSelect.SelectedIndex() < 0 {
			status.SetText("Pick a card first.")
			return
		}
		d := disks[diskSelect.SelectedIndex()]
		if err := refuseUnlessCard(d); err != nil {
			status.SetText(err.Error())
			return
		}
		if err := refuseIfTooSmall(d, imgSize); err != nil {
			status.SetText(err.Error())
			return
		}

		runBtn.Disable()
		progress.SetValue(0)
		status.SetText("Confirming...")
		go func() {
			defer fyne.Do(func() { runBtn.Enable(); progress.Hide(); speedLabel.Hide() })

			backupFirst, proceed := confirmRestore(d.Identifier, inPath)
			if !proceed {
				fyne.Do(func() { status.SetText("Cancelled. Nothing written.") })
				return
			}
			if backupFirst {
				dest, err := zenity.SelectFileSave(
					zenity.Filename(filepath.Join(fallbackOutputDir(), d.Identifier+".backup.img")),
					zenity.ConfirmOverwrite(), imgFilters)
				if err != nil {
					msg := "Cancelled. Nothing written."
					if err != zenity.ErrCanceled {
						msg = "Failed: " + err.Error()
					}
					fyne.Do(func() { status.SetText(msg) })
					return
				}
				fyne.Do(func() { status.SetText("Backing up first...") })
				if _, err := backupJob(d, dest, false, nil, nil, appendLog); err != nil {
					fyne.Do(func() { status.SetText("Failed: " + err.Error()) })
					return
				}
			}

			fyne.Do(func() { status.SetText("Restoring...") })
			var card disk.RawDevice
			err := restoreSequence(
				func() error {
					writeTracker := newSpeedTracker(imgSize, progress, speedLabel)
					var err error
					card, err = restoreJob(d, inPath, writeTracker.onProgress, appendLog)
					if err != nil {
						return err
					}
					fyne.Do(func() { progress.SetValue(1) })
					appendLog(writeTracker.finish(imgSize))
					return nil
				},
				func() error {
					fyne.Do(func() { status.SetText("Verifying...") })
					verifyTracker := newSpeedTracker(imgSize, progress, speedLabel)
					if err := verifyRestore(card, inPath, verifyTracker.onProgress, appendLog); err != nil {
						return err
					}
					appendLog(verifyTracker.finish(imgSize))
					return nil
				},
				verifyCheck.Checked,
			)
			if card != nil {
				card.Close()
			}
			if err != nil {
				fyne.Do(func() { status.SetText("Failed: " + err.Error()) })
				return
			}
			fyne.Do(func() { status.SetText("Done: " + d.Identifier) })
		}()
	})
	runBtn.Importance = widget.HighImportance

	return container.NewVBox(
		widget.NewLabelWithStyle("Image", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewBorder(nil, nil, nil, chooseIn, inLabel),
		versionLabel,
		widget.NewLabelWithStyle("Card", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewBorder(nil, nil, nil, refreshBtn, diskSelect),
		sizesLabel,
		verifyCheck,
		runBtn,
		progress,
		speedLabel,
		status,
		logView,
	)
}
