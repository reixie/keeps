package main

import (
	"slices"

	"github.com/egoist/mygo/ui"
)

// Mygo resolves native choices by position and label on a second build pass.
// Keep both passes on the same data even if library updates arrive in between.
type captureMenuSnapshot struct {
	triggerID    string
	ids          []string
	openLink     bool
	addToStarred bool
	restore      bool
	collectionID string
	collections  []collectionMenuEntry
}

type collectionMenuEntry struct {
	ID, Label string
	Checked   bool
}

func contextInvocation(ev ui.InputEvent) bool {
	// Mygo's macOS backend normalizes Control-click to button 1 before
	// HandleInput. Returning false lets Mygo present the system ContextMenu.
	return ev.Kind == ui.InputPointerDown && ev.Button == 1 ||
		ev.Kind == ui.InputKeyDown && (ev.Key == ui.KeyContextMenu && ev.Mods == 0 || ev.Key == ui.KeyF10 && ev.Mods == ui.Shift)
}

func (a *app) prepareCaptureContextMenu(id string) {
	if !a.selection.has(id) {
		a.selection.click(id, false)
	}
	a.focus = captureFocusPrefix + id
	a.pointerActivation = ""
	menu := &captureMenuSnapshot{
		triggerID:    id,
		restore:      a.navigation.selected == sectionTrash,
		collectionID: a.activeCollectionID(),
	}
	var selected []capture
	for _, item := range a.visibleItems() {
		if a.selection.has(item.ID) {
			selected = append(selected, item)
			menu.ids = append(menu.ids, item.ID)
			menu.addToStarred = menu.addToStarred || !item.Starred
		}
	}
	menu.openLink = len(selected) == 1 && selected[0].isLink()
	if !menu.restore {
		menu.collections = freezeCollectionMenu(a.collections, selected)
	}
	a.cardMenu = menu
}

func freezeCollectionMenu(collections []collection, selected []capture) []collectionMenuEntry {
	entries := make([]collectionMenuEntry, 0, len(collections))
	for _, entry := range collections {
		count := 0
		for _, item := range selected {
			if slices.Contains(item.CollectionIDs, entry.ID) {
				count++
			}
		}
		all := len(selected) > 0 && count == len(selected)
		label := entry.Name
		// Mygo's native MenuItem exposes a boolean check mark. Keep partial
		// membership explicit in the label rather than displaying it as all.
		if count > 0 && !all {
			label += " (Some Selected)"
		}
		entries = append(entries, collectionMenuEntry{ID: entry.ID, Label: label, Checked: all})
	}
	return entries
}

func (a *app) captureContextMenu(id string, m *ui.Menu) {
	menu := a.cardMenu
	if menu == nil || menu.triggerID != id || len(menu.ids) == 0 {
		return
	}
	if action := a.captureMenuAction(m, menu); action != nil {
		a.cardMenu = nil
		action()
	}
}

func (a *app) captureMenuAction(m *ui.Menu, menu *captureMenuSnapshot) func() {
	if menu.openLink {
		if m.Item("Open Link").Chosen() {
			return func() {
				for _, item := range a.items {
					if item.ID == menu.ids[0] && item.isLink() {
						a.openCapture(item)
						return
					}
				}
			}
		}
		m.Separator()
	}
	trash := trashOperation{IDs: menu.ids, Restore: menu.restore}
	if trash.Restore {
		if m.Item(trash.label()).Chosen() {
			return func() { a.enqueueTrash(trash) }
		}
		m.Separator()
		if m.Item("Delete Permanently…").Chosen() {
			return func() { a.confirmDeletePermanently(trash.IDs) }
		}
		return nil
	}
	starredLabel := "Remove from Starred"
	if menu.addToStarred {
		starredLabel = "Add to Starred"
	}
	if m.Item(starredLabel).Chosen() {
		return func() { a.enqueueStarred(starredOperation{IDs: menu.ids, Value: menu.addToStarred}) }
	}
	m.Separator()
	if action := a.captureCollectionAction(m, menu); action != nil {
		return action
	}
	if menu.collectionID != "" && m.Item("Remove from This Collection").Chosen() {
		return func() {
			a.enqueueCollections(collectionOperation{IDs: slices.Clone(menu.ids), Changes: map[string]bool{menu.collectionID: false}}, nil)
		}
	}
	m.Separator()
	if m.Item(trash.label()).Chosen() {
		return func() { a.enqueueTrash(trash) }
	}
	return nil
}

func (a *app) captureCollectionAction(m *ui.Menu, menu *captureMenuSnapshot) func() {
	var action func()
	m.Submenu("Add to Collection", func(sub *ui.Menu) {
		for _, entry := range menu.collections {
			if sub.Item(entry.Label).Checked(entry.Checked).Chosen() {
				action = func() {
					a.enqueueCollections(collectionOperation{IDs: slices.Clone(menu.ids), Changes: map[string]bool{entry.ID: !entry.Checked}}, nil)
				}
				return
			}
		}
		if len(menu.collections) > 0 {
			sub.Separator()
		}
		if sub.Item("New Collection…").Chosen() {
			action = func() { a.openCollectionForm(nil, menu.ids, false) }
		}
	})
	return action
}
