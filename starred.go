package main

import "github.com/egoist/mygo/ui"

type starredOperation struct {
	IDs   []string
	Value bool
}

func (op starredOperation) label() string {
	if op.Value {
		return "Add to Starred"
	}
	return "Remove from Starred"
}

func contextInvocation(ev ui.InputEvent) bool {
	// Mygo's macOS backend normalizes Control-click to button 1 before
	// HandleInput. Returning false lets Mygo present the system ContextMenu.
	return ev.Kind == ui.InputPointerDown && ev.Button == 1 ||
		ev.Kind == ui.InputKeyDown && (ev.Key == ui.KeyContextMenu && ev.Mods == 0 || ev.Key == ui.KeyF10 && ev.Mods == ui.Shift)
}

func (a *app) prepareStarredMenu(id string) {
	if !a.selection[id] {
		a.selectCapture(id, false)
	}
	a.focus = "capture:" + id
	a.pointerActivation = ""
	op := starredOperation{}
	for _, item := range a.visibleItems() {
		if a.selection[item.ID] {
			op.IDs = append(op.IDs, item.ID)
			op.Value = op.Value || !item.Starred
		}
	}
	a.contextCard, a.contextAction = id, op
	a.contextTrash = trashOperation{IDs: append([]string(nil), op.IDs...), Restore: a.navigation.selected == sectionTrash}
	a.contextCollection = a.activeCollectionID()
	a.contextCollections = nil
	if !a.contextTrash.Restore {
		a.contextCollections = a.freezeCollectionMenu(op.IDs)
	}
}

func (a *app) starredMenu(id string, m *ui.Menu) {
	if a.contextCard != id || len(a.contextAction.IDs) == 0 {
		return
	}
	// Both builder runs use the SAME label and operation, even if a queued
	// import/metadata result arrives while the native menu is tracking.
	op := a.contextAction
	trash := a.contextTrash
	collectionID := a.contextCollection
	collections := a.contextCollections
	if len(op.IDs) == 1 {
		for _, item := range a.items {
			if item.ID == op.IDs[0] && item.isLink() {
				if m.Item("Open Link").Chosen() {
					a.consumeContext()
					a.openCapture(item)
					return
				}
				m.Separator()
				break
			}
		}
	}
	if trash.Restore {
		if m.Item(trash.label()).Chosen() {
			a.consumeContext()
			a.enqueueTrash(trash)
			return
		}
		m.Separator()
		if m.Item("Delete Permanently…").Chosen() {
			a.consumeContext()
			a.confirmDeletePermanently(trash.IDs)
		}
		return
	}
	if m.Item(op.label()).Chosen() {
		a.consumeContext()
		a.enqueueStarred(op)
		return
	}
	m.Separator()
	a.captureCollectionsMenu(m, op.IDs, collections)
	if a.contextCard == "" {
		return
	}
	if collectionID != "" {
		if m.Item("Remove from This Collection").Chosen() {
			a.consumeContext()
			a.enqueueCollections(collectionOperation{IDs: append([]string(nil), op.IDs...), Changes: map[string]bool{collectionID: false}}, nil)
			return
		}
	}
	m.Separator()
	if m.Item(trash.label()).Chosen() {
		a.consumeContext()
		a.enqueueTrash(trash)
	}
}

func (a *app) consumeContext() {
	a.contextCard, a.contextAction, a.contextTrash = "", starredOperation{}, trashOperation{}
	a.contextCollection = ""
	a.contextCollections = nil
}

func (a *app) enqueueStarred(op starredOperation) {
	if a.store == nil || a.loadError != nil || len(op.IDs) == 0 {
		return
	}
	op.IDs = append([]string(nil), op.IDs...)
	a.enqueueImport(func() (importInput, error) { return importInput{starred: &op}, nil })
}
