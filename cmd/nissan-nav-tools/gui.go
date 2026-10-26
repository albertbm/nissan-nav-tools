package main

import (
	_ "embed"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/ncruces/zenity"
)

// imgFilters is the native dialogs' file filter: card images, or anything.
var imgFilters = zenity.FileFilters{
	{Name: "Card image", Patterns: []string{"*.img"}, CaseFold: true},
	{Name: "All files", Patterns: []string{"*"}},
}

// pickFile runs the dialog off the UI thread and calls back on it. A cancel is quiet.
func pickFile(startDir string, log func(string), onChosen func(path string)) {
	go func() {
		path, err := zenity.SelectFile(zenity.Filename(startDir), imgFilters)
		if err != nil {
			if err != zenity.ErrCanceled {
				fyne.Do(func() { log("Could not open the file picker: " + err.Error()) })
			}
			return
		}
		fyne.Do(func() { onChosen(path) })
	}()
}

// pickSaveFile is pickFile for saving, and remembers the chosen folder as the next default.
func pickSaveFile(startPath string, log func(string), onChosen func(path string)) {
	go func() {
		path, err := zenity.SelectFileSave(zenity.Filename(startPath), zenity.ConfirmOverwrite(), imgFilters)
		if err != nil {
			if err != zenity.ErrCanceled {
				fyne.Do(func() { log("Could not open the save dialog: " + err.Error()) })
			}
			return
		}
		fyne.CurrentApp().Preferences().SetString(prefLastOutputDir, dirOf(path))
		fyne.Do(func() { onChosen(path) })
	}()
}

//go:embed Icon.png
var iconPNG []byte

func runGUI() {
	a := app.NewWithID("io.albertbm.nissan-nav-tools")
	a.SetIcon(fyne.NewStaticResource("Icon.png", iconPNG))
	w := a.NewWindow("Nissan Nav Tools")

	w.SetContent(container.NewAppTabs(
		container.NewTabItem("Back up", newBackupTab(w)),
		container.NewTabItem("Restore", newRestoreTab(w)),
		container.NewTabItem("Patch image", newPatchTab(w)),
		container.NewTabItem("Anonymize", newAnonymizeTab(w)),
	))
	w.Resize(fyne.NewSize(640, 600))
	w.ShowAndRun()
}

// logEntry drops typed input so it reads as a selectable box, not a disabled one.
type logEntry struct {
	widget.Entry
}

func newLogEntry() *logEntry {
	e := &logEntry{}
	e.MultiLine = true
	e.Wrapping = fyne.TextWrapWord
	e.ExtendBaseWidget(e)
	return e
}

func (e *logEntry) TypedRune(_ rune)          {}
func (e *logEntry) TypedKey(_ *fyne.KeyEvent) {}

func newLog() (fyne.CanvasObject, func(string), func()) {
	box := newLogEntry()
	scroll := container.NewVScroll(box)
	scroll.SetMinSize(fyne.NewSize(560, 220))
	appendLine := func(s string) {
		fyne.Do(func() {
			if box.Text != "" {
				box.SetText(box.Text + "\n" + s)
			} else {
				box.SetText(s)
			}
		})
	}
	clear := func() { fyne.Do(func() { box.SetText("") }) }
	return scroll, appendLine, clear
}

// newHashCheck is opt-in: hashing a full card image is slow.
func newHashCheck() *widget.Check {
	return widget.NewCheck("Compute SHA-256", nil)
}

const prefLastOutputDir = "lastOutputDir"

func dirWritable(dir string) bool {
	if dir == "" {
		return false
	}
	f, err := os.CreateTemp(dir, ".nnt-write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

// fallbackOutputDir is the last output folder, else ~/Documents, never a drive root.
func fallbackOutputDir() string {
	if last := fyne.CurrentApp().Preferences().String(prefLastOutputDir); dirWritable(last) {
		return last
	}
	if home, err := os.UserHomeDir(); err == nil {
		if docs := filepath.Join(home, "Documents"); dirWritable(docs) {
			return docs
		}
	}
	return "."
}

// defaultOutputDir is next to in, or fallbackOutputDir() if that's not writable.
func defaultOutputDir(in string) string {
	if in != "" {
		if d := dirOf(in); dirWritable(d) {
			return d
		}
	}
	return fallbackOutputDir()
}

func defaultOutputPath(in, suffix string) string {
	return filepath.Join(defaultOutputDir(in), filepath.Base(defaultOutput(in, suffix)))
}

func dirOf(path string) string {
	if i := strings.LastIndexAny(path, "/\\"); i >= 0 {
		return path[:i]
	}
	return "."
}
