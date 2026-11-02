package main

import (
	"slices"
	"strings"
)

func (a *app) captureSection() bool {
	return a.navigation.selected == sectionLibrary || a.navigation.selected == sectionStarred || a.navigation.selected == sectionTrash || a.activeCollectionID() != ""
}

// This ordered sequence drives geometry, focus, selection, clipboard and menus.
// Filtering preserves the store's newest-first order.
func (a *app) visibleItems() []capture {
	var items []capture
	for _, item := range a.items {
		if itemInSection(item, a.navigation.selected) {
			items = append(items, item)
		}
	}
	if a.navigation.selected == sectionTrash {
		slices.SortStableFunc(items, func(a, b capture) int {
			if n := b.TrashedAt.Compare(*a.TrashedAt); n != 0 {
				return n
			}
			return strings.Compare(a.ID, b.ID)
		})
	}
	return items
}

func itemInSection(item capture, section string) bool {
	if id, ok := strings.CutPrefix(section, collectionPrefix); ok {
		return item.TrashedAt == nil && slices.Contains(item.CollectionIDs, id)
	}
	switch section {
	case sectionLibrary:
		return item.TrashedAt == nil
	case sectionStarred:
		return item.TrashedAt == nil && item.Starred
	case sectionTrash:
		return item.TrashedAt != nil
	}
	return false
}

func (a *app) sectionCount(section string) int {
	count := 0
	for _, item := range a.items {
		if itemInSection(item, section) {
			count++
		}
	}
	return count
}

func sameLayoutItems(a, b []capture) bool {
	return slices.EqualFunc(a, b, func(a, b capture) bool {
		return a.ID == b.ID && a.Kind == b.Kind && a.Width == b.Width && a.Height == b.Height
	})
}

// Prune hidden IDs and repair focus at the closest surviving neighbor in the
// former visible order (prefer the next card on a tie).
// The following layout pass clamps the scroll offset to the new content height.
func (a *app) reconcileVisible(items []capture) {
	visible := make(map[string]bool, len(items))
	for _, item := range items {
		visible[item.ID] = true
	}
	for id := range a.selection {
		if !visible[id] {
			delete(a.selection, id)
		}
	}
	a.selectionOrder = slices.DeleteFunc(a.selectionOrder, func(id string) bool { return !a.selection[id] })
	if id, card := strings.CutPrefix(a.focus, "capture:"); card && !visible[id] {
		index := slices.IndexFunc(a.layoutItems, func(item capture) bool { return item.ID == id })
		a.focus = focusLibrary
		if len(items) > 0 {
			next := items[0].ID
			if index >= 0 {
			search:
				for distance := 1; distance < len(a.layoutItems); distance++ {
					for _, i := range []int{index + distance, index - distance} {
						if i >= 0 && i < len(a.layoutItems) && visible[a.layoutItems[i].ID] {
							next = a.layoutItems[i].ID
							break search
						}
					}
				}
			}
			a.focus = "capture:" + next
			a.focusNeedsReveal = true
		}
	}
}

func (a *app) revealCard(index int, viewportHeight float32) {
	r := a.layout.Cards[index]
	if r.Y < a.libraryScroll.Y {
		a.libraryScroll.Y = r.Y
	}
	if r.Y+r.H > a.libraryScroll.Y+viewportHeight {
		a.libraryScroll.Y = max(r.Y, r.Y+r.H-viewportHeight)
	}
}
