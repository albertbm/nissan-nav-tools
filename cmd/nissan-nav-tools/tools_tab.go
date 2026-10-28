package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type toolEntry struct {
	name  string
	panel fyne.CanvasObject
}

// newToolsTab holds every one-off card tool behind a single picker. Adding
// a tool is one more entry here; each tool's own panel is unchanged.
func newToolsTab(w fyne.Window) fyne.CanvasObject {
	tools := []toolEntry{
		{"Patch", newPatchTab(w)},
		{"Factory reset", newResetTab(w)},
	}

	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.name
		if i != 0 {
			t.panel.Hide()
		}
	}

	picker := widget.NewSelect(names, func(chosen string) {
		for _, t := range tools {
			if t.name == chosen {
				t.panel.Show()
			} else {
				t.panel.Hide()
			}
		}
	})
	picker.SetSelected(names[0])

	body := container.NewVBox()
	for _, t := range tools {
		body.Add(t.panel)
	}

	return container.NewBorder(
		container.NewVBox(widget.NewLabelWithStyle("Tool", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), picker),
		nil, nil, nil,
		body,
	)
}
