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
	tags         []collectionMenuEntry
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
		for _, tag := range a.tags {
			count := 0
			for _, item := range selected {
				if slices.Contains(item.TagIDs, tag.ID) {
					count++
				}
			}
			menu.tags = append(menu.tags, membershipMenuEntry(tag.ID, tag.Name, count, len(selected)))
		}
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
		entries = append(entries, membershipMenuEntry(entry.ID, entry.Name, count, len(selected)))
	}
	return entries
}

func membershipMenuEntry(id, name string, count, total int) collectionMenuEntry {
	all := total > 0 && count == total
	if count > 0 && !all {
		name += " (Some Selected)"
	}
	return collectionMenuEntry{ID: id, Label: name, Checked: all}
}

func (a *app) captureContextMenu(id string, m *ui.Menu) {
	menu := a.cardMenu
	if a.switchingSpace || menu == nil || menu.triggerID != id || len(menu.ids) == 0 {
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
	var tagAction func()
	m.Submenu("Tags", func(sub *ui.Menu) {
		for _, entry := range menu.tags {
			if sub.Item(entry.Label).Checked(entry.Checked).Chosen() {
				tagAction = func() {
					a.changeTags("Could not update tags", func(store *libraryStore) (manifest, error) {
						return store.assignTag(menu.ids, entry.ID, !entry.Checked)
					})
				}
				return
			}
		}
		if len(menu.tags) > 0 {
			sub.Separator()
		}
		if sub.Item("New Tag…").Disabled(a.controls == nil).Chosen() {
			tagAction = func() {
				generation := a.spaceGeneration
				a.window.Update(func() {
					if a.spaceGeneration == generation {
						a.openTagEditor(libraryTag{}, menu.ids)
					}
				})
			}
		}
	})
	if tagAction != nil {
		return tagAction
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
