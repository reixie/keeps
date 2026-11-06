package main

import (
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const focusSearch = "capture-search"
const browseBarHeight = float32(34)

type browseKey struct {
	section, query, sort string
	filters              captureFilters
	since                time.Time
}

type browseState struct {
	searching bool
	query     string
	key       browseKey
	items     []capture
	valid     bool
}

func browseName(item capture) string {
	if item.isLink() && item.Title != "" {
		return tagNameKey(item.Title)
	}
	return tagNameKey(item.Filename)
}

func matchesSearch(item capture, words []string, tags map[string]string) bool {
	if len(words) == 0 {
		return true
	}
	fields := []string{tagNameKey(item.Filename), tagNameKey(item.Title), tagNameKey(item.URL)}
	for _, id := range item.TagIDs {
		fields = append(fields, tags[id])
	}
	for _, word := range words {
		if !slices.ContainsFunc(fields, func(field string) bool { return strings.Contains(field, word) }) {
			return false
		}
	}
	return true
}

func (a *app) showSearch() {
	if a.store == nil || a.loadError != nil || a.libraryInputBlocked() {
		return
	}
	a.controls.dismiss(controlTags)
	a.captureMenu.dismiss()
	a.browse.searching = true
	a.focus = focusSearch
	a.window.Invalidate()
}

func (a *app) closeSearch() {
	a.browse.searching, a.browse.query = false, ""
	a.focus = "header-search"
	a.filtersChanged()
}

func (a *app) clearBrowse() {
	a.filters = captureFilters{}
	a.browse.query = ""
	if a.browse.searching {
		a.focus = focusSearch
	} else {
		a.focus = focusLibrary
	}
	a.filtersChanged()
}

func (a *app) hasBrowseConditions() bool {
	return a.filters != (captureFilters{}) || strings.TrimSpace(a.browse.query) != ""
}

func (a *app) browseConditions() string {
	var values []string
	if query := strings.TrimSpace(a.browse.query); query != "" {
		values = append(values, "“"+query+"”")
	}
	for _, option := range filterTypes {
		if a.filters.kind == option.value {
			values = append(values, option.label)
		}
	}
	for _, collection := range a.collections {
		if a.filters.collection == collection.ID {
			values = append(values, collection.Name)
		}
	}
	for _, tag := range a.tags {
		if a.filters.tag == tag.ID {
			values = append(values, "#"+tag.Name)
		}
	}
	for _, option := range filterDates {
		if a.filters.date == option.value {
			values = append(values, option.label)
		}
	}
	return strings.Join(values, " · ")
}

func (a *app) browseMinimum() float32 {
	switch a.preferences.Density {
	case "compact":
		return 150
	case "large":
		return 270
	default:
		return masonryMinimum
	}
}

func (a *app) showBrowseView(bounds ui.Rect) {
	if a.store == nil || a.loadError != nil || a.libraryInputBlocked() {
		return
	}
	a.controls.dismiss(controlTags)
	a.captureMenu.dismiss()
	category := func(title string, preference *string, options []filterOption) *mygo.MenuItem {
		menu := &mygo.MenuItem{Label: title}
		for _, option := range options {
			menu.Submenu = append(menu.Submenu, &mygo.MenuItem{
				Label: option.label, Type: mygo.MenuItemRadio, Checked: *preference == option.value,
				Click: func(*mygo.MenuItem, *mygo.Window) {
					if a.closed || a.switchingSpace {
						return
					}
					*preference = option.value
					if a.preferencesError == nil && !a.libraryOverride {
						savePreferences(a.preferences)
					}
					a.filtersChanged()
				},
			})
		}
		return menu
	}
	sortMenu := category("Sort", &a.preferences.Sort, []filterOption{{"", "Newest"}, {"oldest", "Oldest"}, {"name", "Name A–Z"}})
	sortMenu.Disabled = a.navigation.selected == sectionTrash
	densityMenu := category("Density", &a.preferences.Density, []filterOption{{"compact", "Compact"}, {"", "Standard"}, {"large", "Large"}})
	mygo.NewMenu([]*mygo.MenuItem{sortMenu, densityMenu}).PopupAt(a.window, int(bounds.X), int(bounds.Y+bounds.H))
}
