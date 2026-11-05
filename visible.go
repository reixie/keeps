package main

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

type captureGrid struct {
	scroll      ui.ScrollState
	layout      masonryLayout
	layoutWidth float32
	layoutItems []capture
	revealFocus bool
}

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

// Prune hidden IDs and repair focus at the closest surviving neighbor in the
// former visible order (prefer the next card on a tie).
func (a *app) reconcileVisible(items []capture) {
	visible := make(map[string]bool, len(items))
	for _, item := range items {
		visible[item.ID] = true
	}
	a.selection.retain(visible)
	if id, card := strings.CutPrefix(a.focus, captureFocusPrefix); card && !visible[id] {
		a.focus = focusLibrary
		if len(items) > 0 {
			next := a.grid.neighbor(id, visible)
			if next == "" {
				next = items[0].ID
			}
			a.focus = captureFocusPrefix + next
			a.grid.revealFocus = true
		}
	}
}

func (g *captureGrid) neighbor(id string, visible map[string]bool) string {
	index := slices.IndexFunc(g.layoutItems, func(item capture) bool { return item.ID == id })
	if index < 0 {
		return ""
	}
	for distance := 1; distance < len(g.layoutItems); distance++ {
		for _, i := range []int{index + distance, index - distance} {
			if i >= 0 && i < len(g.layoutItems) && visible[g.layoutItems[i].ID] {
				return g.layoutItems[i].ID
			}
		}
	}
	return ""
}

func (g *captureGrid) reset() {
	g.scroll.Y = 0
	g.revealFocus = false
	g.layout, g.layoutItems = masonryLayout{}, nil
}

func (g *captureGrid) updateLayout(items []capture, width, viewportHeight float32, focus string) {
	if g.layoutWidth != width || !slices.EqualFunc(g.layoutItems, items, func(a, b capture) bool {
		return a.ID == b.ID && a.Kind == b.Kind && a.Width == b.Width && a.Height == b.Height
	}) {
		g.layout = layoutMasonry(items, width)
		g.layoutWidth, g.layoutItems = width, slices.Clone(items)
	}
	g.scroll.Y = min(g.scroll.Y, max(0, g.layout.Height-viewportHeight))
	if g.revealFocus {
		if index := slices.IndexFunc(items, func(item capture) bool { return focus == captureFocusPrefix+item.ID }); index >= 0 {
			g.revealCard(index, viewportHeight)
		}
		g.revealFocus = false
	}
}

func (g *captureGrid) revealCard(index int, viewportHeight float32) {
	r := g.layout.Cards[index]
	if r.Y < g.scroll.Y {
		g.scroll.Y = r.Y
	}
	if r.Y+r.H > g.scroll.Y+viewportHeight {
		g.scroll.Y = max(r.Y, r.Y+r.H-viewportHeight)
	}
}
