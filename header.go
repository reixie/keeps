package main

import (
	"embed"

	"github.com/egoist/mygo/ui"
)

const headerHeight = float32(48)
const headerPadding = float32(16)

//go:embed resources/header/*.svg
var headerAssets embed.FS

func headerIcon(name string) *ui.SVG {
	data, err := headerAssets.ReadFile("resources/header/" + name + ".svg")
	if err != nil {
		panic(err)
	}
	return ui.MustParseSVG(data)
}

var headerFilterIcon = reiconGlyph("filter")
var headerGridIcon = headerIcon("grid")
var searchIcon = reiconGlyph("search")

func (a *app) sidebarToggleX(c *ui.Context) float32 {
	if a.navigation.hidden {
		return max(96, c.TitleBar().Left+sidebarControlInset)
	}
	return sidebarWidth - sidebarControlInset - sidebarControlSize
}

func (a *app) sectionHeading() (string, *ui.SVG) {
	if id := a.activeCollectionID(); id != "" {
		for _, entry := range a.collections {
			if entry.ID == id {
				return entry.Name, collectionIconFor(entry.Icon).icon
			}
		}
	}
	for _, section := range sections {
		if section.id == a.navigation.selected {
			return section.label, section.icon
		}
	}
	return sections[0].label, sections[0].icon
}

func (a *app) header(c *ui.Context, t themeValues) {
	title, icon := a.sectionHeading()
	left := headerPadding
	if a.navigation.hidden {
		// The panel begins after the outer padding and its one-point border.
		// Leave a normal Header inset after the native/sidebar control group.
		left = max(left, a.sidebarToggleX(c)+sidebarControlSize+headerPadding-contentPadding-panelBorderWidth)
	}
	ui.Row(c).Key("header").Height(headerHeight).Shrink(0).
		Padding(0, headerPadding, 0, left).AlignItems(ui.Center).Gap(16).
		DragWindow().Label("Section header").Children(func() {
		if a.browse.searching {
			ui.Row(c).Grow(1).MinWidth(0).Height(30).Padding(0, 8).Gap(7).Radius(8).
				AlignItems(ui.Center).Background(t.BackgroundSecondary).Border(1, t.Border).Children(func() {
				ui.Icon(c, searchIcon).Size(15, 15).Shrink(0).TextColor(t.TextSecondary)
				input := ui.TextInputBase(c.Key(focusSearch), &a.browse.query).Grow(1).MinWidth(0).
					Label("Search captures").Placeholder("Search captures…").FocusBind(&a.focus, focusSearch).
					HandleInput(func(ev ui.InputEvent) bool {
						if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyEscape && ev.Mods == 0 {
							a.closeSearch()
							return true
						}
						return false
					})
				if input.Changed() {
					a.window.Update(a.filtersChanged)
				}
			})
		} else {
			ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Icon(c, icon).Size(16, 16).Shrink(0).TextColor(t.TextPrimary)
				ui.Text(c, title).Grow(1).MinWidth(0).Font("SF Pro, system-ui").
					FontSize(13).FontWeight(510).FixedLineHeight(20).TextColor(t.TextPrimary).
					SingleLine().Label(title).Tooltip(title)
			})
		}
		ui.Row(c).Shrink(0).Gap(2).Padding(0, 2).Radius(16).Background(t.BackgroundSecondary).Children(func() {
			disabled := a.loading || a.store == nil || a.loadError != nil || a.switchingSpace
			a.headerAction(c, t, "header-search", "Search (⌘F)", searchIcon, a.browse.searching, disabled).
				OnClick(func() {
					a.window.Update(func() {
						if a.browse.searching {
							a.closeSearch()
						} else {
							a.showSearch()
						}
					})
				})
			filter := a.headerAction(c, t, "header-filter", "Filter", headerFilterIcon, a.filters != (captureFilters{}), disabled)
			filterBounds := filter.Bounds()
			filter.OnClick(func() { a.window.Update(func() { a.showFilter(filterBounds) }) })
			view := a.headerAction(c, t, "header-view", "View", headerGridIcon, false, disabled)
			viewBounds := view.Bounds()
			view.OnClick(func() { a.window.Update(func() { a.showBrowseView(viewBounds) }) })
		})
		ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(1).Background(t.Border)
	})
}

func (a *app) headerAction(c *ui.Context, t themeValues, key, label string, icon *ui.SVG, active, disabled bool) ui.Element {
	button := ui.ButtonBase(c).Key(key).Size(30, 30).Shrink(0).Radius(15).
		Role(ui.RoleButton).Label(label).Tooltip(label).Selected(active).Disabled(disabled).
		FocusBind(&a.focus, key).HandleInput(a.shellInput)
	ink := sidebarButtonInk(button, t, active)
	button.Children(func() { ui.Icon(c, icon).Size(16, 16).TextColor(ink) })
	return button
}

func (a *app) browseButton(c *ui.Context, t themeValues, key, title string) ui.Element {
	button := ui.ButtonBase(c).Key(key).Height(26).Shrink(0).Padding(0, 9).Radius(8).
		Role(ui.RoleButton).Label(title).FocusBind(&a.focus, key)
	ink := sidebarButtonInk(button, t, false)
	button.Children(func() { ui.Text(c, title).FontSize(12).TextColor(ink) })
	return button
}
