package main

import (
	"slices"

	"github.com/egoist/mygo/ui"
)

const focusLibrary = "library-panel"

type captureSelection map[string]bool

func (s captureSelection) click(id string, toggle bool) {
	if toggle {
		if s[id] {
			delete(s, id)
		} else {
			s[id] = true
		}
		return
	}
	clear(s)
	s[id] = true
}

// Focused text widgets handle their own InputCommand. These handlers are
// attached only to the library's cards/panel and non-text shell controls.
func (a *app) shellInput(ev ui.InputEvent) bool { return a.libraryInput(ev, false) }

func (a *app) libraryInput(ev ui.InputEvent, libraryContext bool) bool {
	if a.clipboardBlocked() {
		return false
	}
	command := ""
	if ev.Kind == ui.InputCommand {
		command = ev.Text
	}
	if ev.Kind == ui.InputKeyDown {
		if ev.Mods == ui.Cmd {
			switch ev.Key {
			case ui.KeyC:
				command = "copy"
			case ui.KeyV:
				command = "paste"
			case ui.KeyA:
				command = "selectAll"
			}
		}
		if ev.Mods == 0 && ev.Key == ui.KeyEscape {
			clear(a.selection)
			a.selectionOrder = nil
			return true
		}
		if libraryContext && ev.Mods == 0 && (ev.Key == ui.KeySpace || ev.Key == ui.KeyEnter) {
			if item, ok := a.previewTarget(); ok {
				if ev.Key == ui.KeySpace {
					a.showPreview(item)
				} else {
					a.openCapture(item)
				}
			}
			return true
		}
	}
	switch command {
	case "copy":
		a.copySelected()
		return true
	case "paste":
		a.pasteMedia()
		return true
	case "selectAll":
		if !a.captureSection() || !libraryContext {
			return false
		}
		clear(a.selection)
		a.selectionOrder = nil
		for _, item := range a.visibleItems() {
			a.selection[item.ID] = true
			a.selectionOrder = append(a.selectionOrder, item.ID)
		}
		return true
	}
	return false
}

// Keep the order of explicit selections for URL clipboard items. All-media
// copies retain their existing visible-library order.
func (a *app) selectCapture(id string, toggle bool) {
	if !toggle {
		a.selectionOrder = nil
	}
	a.selection.click(id, toggle)
	if a.selection[id] && !slices.Contains(a.selectionOrder, id) {
		a.selectionOrder = append(a.selectionOrder, id)
	}
	if !a.selection[id] {
		a.selectionOrder = slices.DeleteFunc(a.selectionOrder, func(old string) bool { return old == id })
	}
}
func (a *app) orderedCopySelection() []capture {
	var selected []capture
	hasLink := false
	for _, item := range a.visibleItems() {
		if a.selection[item.ID] {
			selected = append(selected, item)
			hasLink = hasLink || item.isLink()
		}
	}
	if !hasLink {
		return selected
	}
	byID := make(map[string]capture, len(selected))
	for _, item := range selected {
		byID[item.ID] = item
	}
	ordered := make([]capture, 0, len(selected))
	for _, id := range a.selectionOrder {
		if item, ok := byID[id]; ok {
			ordered = append(ordered, item)
			delete(byID, id)
		}
	}
	for _, item := range selected {
		if _, ok := byID[item.ID]; ok {
			ordered = append(ordered, item)
		}
	}
	return ordered
}
