package main

import (
	"slices"

	"github.com/egoist/mygo/ui"
)

// Keep both native menu builder passes on the same collection order, labels
// and explicit actions, even if a queued library update arrives in between.
type collectionMenuEntry struct {
	ID, Label string
	Checked   bool
	Assign    bool
}

func (a *app) freezeCollectionMenu(ids []string) []collectionMenuEntry {
	targets := make(map[string]bool, len(ids))
	for _, id := range ids {
		targets[id] = true
	}
	entries := make([]collectionMenuEntry, 0, len(a.collections))
	for _, entry := range a.collections {
		count := 0
		for _, item := range a.items {
			if targets[item.ID] && slices.Contains(item.CollectionIDs, entry.ID) {
				count++
			}
		}
		all := len(ids) > 0 && count == len(ids)
		label := entry.Name
		// Mygo's native MenuItem exposes a boolean check mark. Keep partial
		// membership explicit in the label rather than displaying it as all.
		if count > 0 && !all {
			label += " (Some Selected)"
		}
		entries = append(entries, collectionMenuEntry{ID: entry.ID, Label: label, Checked: all, Assign: !all})
	}
	return entries
}

func (a *app) captureCollectionsMenu(m *ui.Menu, ids []string, entries []collectionMenuEntry) {
	m.Submenu("Add to Collection", func(sub *ui.Menu) {
		for _, entry := range entries {
			if sub.Item(entry.Label).Checked(entry.Checked).Chosen() {
				a.consumeContext()
				a.enqueueCollections(collectionOperation{IDs: slices.Clone(ids), Changes: map[string]bool{entry.ID: entry.Assign}}, nil)
				return
			}
		}
		if len(entries) > 0 {
			sub.Separator()
		}
		if sub.Item("New Collection…").Chosen() {
			a.consumeContext()
			a.openCollectionForm(nil, ids, false)
		}
	})
}
