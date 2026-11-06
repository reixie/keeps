package main

import (
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type captureFilters struct {
	kind, collection, tag, date string
}

func (f captureFilters) matches(item capture, since time.Time) bool {
	if f.collection != "" && !slices.Contains(item.CollectionIDs, f.collection) ||
		f.tag != "" && !slices.Contains(item.TagIDs, f.tag) ||
		!since.IsZero() && item.ImportedAt.Before(since) {
		return false
	}
	switch f.kind {
	case "image":
		return item.MediaType == "image" && item.Format != "gif"
	case "gif":
		return item.Format == "gif"
	case "video":
		return item.MediaType == "video"
	case "link":
		return item.isLink()
	default:
		return true
	}
}

func (f captureFilters) since(now time.Time) time.Time {
	y, m, d := now.Date()
	switch f.date {
	case "today":
		return time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	case "week":
		return time.Date(y, m, d-6, 0, 0, 0, 0, now.Location())
	case "month":
		return time.Date(y, m, d-29, 0, 0, 0, 0, now.Location())
	default:
		return time.Time{}
	}
}

type filterOption struct{ value, label string }

var (
	filterTypes = []filterOption{{"image", "Image"}, {"gif", "GIF"}, {"video", "Video"}, {"link", "Link"}}
	filterDates = []filterOption{{"today", "Today"}, {"week", "Last 7 Days"}, {"month", "Last 30 Days"}}
)

var (
	filterTypeIcon       = reiconMenuGlyph("type")
	filterCollectionIcon = reiconMenuGlyph("collections")
	filterTagIcon        = reiconMenuGlyph("tags")
	filterDateIcon       = reiconMenuGlyph("date")
)

func (a *app) showFilter(bounds ui.Rect) {
	if a.loading || a.store == nil || a.loadError != nil || a.libraryInputBlocked() {
		return
	}
	a.controls.dismiss(controlTags)
	a.captureMenu.dismiss()
	generation := a.spaceGeneration
	category := func(label string, icon *mygo.MenuIcon, field *string, options []filterOption) *mygo.MenuItem {
		options = append([]filterOption{{"", "Any"}}, options...)
		item := &mygo.MenuItem{Label: label, Icon: icon}
		for _, option := range options {
			item.Submenu = append(item.Submenu, &mygo.MenuItem{
				Label: option.label, Type: mygo.MenuItemRadio, Checked: *field == option.value,
				Click: func(*mygo.MenuItem, *mygo.Window) {
					if a.closed || a.switchingSpace || a.spaceGeneration != generation {
						return
					}
					*field = option.value
					a.filtersChanged()
				},
			})
		}
		return item
	}
	var collections, tags []filterOption
	for _, c := range a.collections {
		collections = append(collections, filterOption{c.ID, c.Name})
	}
	for _, tag := range a.tags {
		tags = append(tags, filterOption{tag.ID, tag.Name})
	}
	menu := mygo.NewMenu([]*mygo.MenuItem{
		category("Type", filterTypeIcon, &a.filters.kind, filterTypes),
		category("Collections", filterCollectionIcon, &a.filters.collection, collections),
		category("Tags", filterTagIcon, &a.filters.tag, tags),
		category("Date", filterDateIcon, &a.filters.date, filterDates),
		mygo.Separator(),
		{Label: "Clear Filters", Disabled: a.filters == (captureFilters{}), Click: func(*mygo.MenuItem, *mygo.Window) {
			if !a.closed && !a.switchingSpace && a.spaceGeneration == generation {
				a.filters = captureFilters{}
				a.filtersChanged()
			}
		}},
	})
	menu.PopupAt(a.window, int(bounds.X), int(bounds.Y+bounds.H))
}

func (a *app) filtersChanged() {
	if a.filters.collection != "" && !hasCollection(a.collections, a.filters.collection) {
		a.filters.collection = ""
	}
	if a.filters.tag != "" && !hasTag(a.tags, a.filters.tag) {
		a.filters.tag = ""
	}
	a.copyGeneration++
	a.grid.scroll.Y = 0
	a.reconcileVisible(a.visibleItems())
	a.window.Invalidate()
}
