package main

import (
	"slices"

	"github.com/egoist/mygo/ui"
)

const (
	focusLibrary       = "library-panel"
	captureFocusPrefix = "capture:"
)

type captureSelection struct {
	members map[string]struct{}
	order   []string
}

func (s *captureSelection) has(id string) bool {
	_, ok := s.members[id]
	return ok
}

func (s *captureSelection) add(id string) {
	if s.members == nil {
		s.members = make(map[string]struct{})
	}
	s.members[id] = struct{}{}
	s.order = append(s.order, id)
}

func (s *captureSelection) clear() {
	clear(s.members)
	s.order = nil
}

func (s *captureSelection) click(id string, toggle bool) {
	if !toggle {
		s.clear()
	}
	if s.has(id) {
		delete(s.members, id)
		s.order = slices.DeleteFunc(s.order, func(old string) bool { return old == id })
		return
	}
	s.add(id)
}

func (s *captureSelection) retain(visible map[string]bool) {
	s.order = slices.DeleteFunc(s.order, func(id string) bool {
		if visible[id] {
			return false
		}
		delete(s.members, id)
		return true
	})
}

func (a *app) restoreCaptureFocus(previous string) {
	switch {
	case a.navigation.hidden && (previous == focusAddCapture || previous == focusNavigation):
		a.focus = focusToggle
	case previous == "":
		a.focus = focusLibrary
	default:
		a.focus = previous
	}
}

func (a *app) libraryInputBlocked() bool {
	return a.closed || a.picking || a.nativeDialogs > 0 || a.collectionDialog != nil || a.linkDialog != nil || a.previewOwnsFocus()
}

// Focused text widgets handle their own InputCommand. These handlers are
// attached only to the library's cards/panel and non-text shell controls.
func (a *app) shellInput(ev ui.InputEvent) bool { return a.libraryInput(ev, false) }

func (a *app) libraryInput(ev ui.InputEvent, libraryContext bool) bool {
	if a.libraryInputBlocked() {
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
			a.selection.clear()
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
		a.selection.clear()
		for _, item := range a.visibleItems() {
			a.selection.add(item.ID)
		}
		return true
	}
	return false
}

// Mixed and URL clipboard items use the order of explicit selections.
// All-media copies follow the visible library order.
func (s *captureSelection) orderedCopy(items []capture) []capture {
	var selected []capture
	hasLink := false
	for _, item := range items {
		if s.has(item.ID) {
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
	for _, id := range s.order {
		if item, ok := byID[id]; ok {
			ordered = append(ordered, item)
		}
	}
	return ordered
}
