package main

import (
	"embed"
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	sectionLibrary  = "library"
	sectionStarred  = "starred"
	sectionTags     = "tags"
	sectionTrash    = "trash"
	focusNavigation = "navigation"
	focusToggle     = "sidebar-toggle"
	focusAddCapture = "add-capture"
)

// IDs are independent of labels and survive hiding the sidebar.
type navigationState struct {
	selected string
	hidden   bool
}

func newApp() *app {
	return &app{navigation: navigationState{selected: sectionLibrary}, focus: focusLibrary}
}

//go:embed resources/sidebar/*.svg
var sidebarAssets embed.FS

func sidebarIcon(name string) *ui.SVG {
	data, err := sidebarAssets.ReadFile("resources/sidebar/" + name + ".svg")
	if err != nil {
		panic(err)
	}
	return ui.MustParseSVG(data)
}

var (
	sidebarToggleIcon = sidebarIcon("sidebar-left")
	addCaptureIcon    = sidebarIcon("plus")
	settingsIcon      = sidebarIcon("settings")
	sections          = []struct {
		id, label string
		icon      *ui.SVG
	}{
		{sectionLibrary, "Library", sidebarIcon("library")},
		{sectionStarred, "Starred", sidebarIcon("starred")},
		{sectionTags, "Tags", sidebarIcon("tags")},
		{sectionTrash, "Trash", sidebarIcon("trash")},
	}
)

func (a *app) sidebar(c *ui.Context, t themeValues) {
	ui.Column(c).Key("sidebar").Width(sidebarWidth).Shrink(0).
		FontWeight(510).FixedLineHeight(20).Label("Sidebar").Children(func() {
		ui.Box(c).Height(dragHeight).Shrink(0)
		base := c.Theme()
		// Sidebar owns selection, keys, type-to-select, focus and accessibility.
		// Only its metrics and paints differ from the toolkit's source list.
		ui.Sidebar(c, &a.navigation.selected, func() {
			for _, section := range sections {
				a.sidebarRow(c, t, base, section.id, section.label, section.icon, a.sectionCount(section.id), nil)
			}
			ui.Row(c).Margin(6, 0, 0, 0).Height(32).Shrink(0).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, "Collections").Grow(1).MinWidth(0).Padding(8).FontSize(12).
					FontWeight(400).FixedLineHeight(16).TextColor(t.TextSecondary)
				plus := ui.ButtonBase(c).Key("new-collection").Size(24, 24).Padding(6).Radius(75).
					Label("New Collection").Tooltip("New Collection…").FocusBind(&a.focus, "new-collection").
					Disabled(a.loading || a.loadError != nil || a.store == nil)
				ink := t.TextSecondary
				if !plus.IsDisabled() {
					if plus.Hovered() {
						ink = t.TextPrimary
						plus.Background(t.Hover)
					}
					if plus.Pressed() {
						plus.Background(t.Selected)
					}
				}
				plus.Children(func() { ui.Icon(c, collectionPlusIcon).Size(12, 12).TextColor(ink) }).
					OnClick(func() { a.openCollectionForm(nil, nil, true) })
			})
			for _, entry := range a.collections {
				id := collectionSection(entry.ID)
				a.sidebarRow(c, t, base, id, entry.Name, collectionIconFor(entry.Icon).icon, a.sectionCount(id), &entry)
			}
		}).Grow(1).MinHeight(0).Shrink(1).Padding(8, 12).Gap(4).
			Background(ui.Transparent).Label("Navigation").FocusBind(&a.focus, focusNavigation).
			HandleInput(func(ev ui.InputEvent) bool {
				if contextInvocation(ev) {
					// A row's own handler takes pointer invocations. This root
					// handles the keyboard, or blank sidebar space (no menu).
					a.contextSidebarCollection = collection{}
					if ev.Kind == ui.InputKeyDown {
						for _, entry := range a.collections {
							if entry.ID == a.activeCollectionID() {
								a.contextSidebarCollection = entry
							}
						}
					}
				}
				return a.shellInput(ev)
			}).ContextMenu(func(m *ui.Menu) {
			a.collectionRowMenu(a.contextSidebarCollection.ID, m)
		})
		ui.Box(c).Height(56).Shrink(0).Padding(12).Children(func() {
			b := ui.ButtonBase(c).Key("settings").Size(32, 32).Padding(8).Radius(100).
				Role(ui.RoleButton).Label("Settings").Tooltip("Settings")
			ink := t.TextSecondary
			if b.Hovered() {
				ink = t.TextPrimary
			}
			if b.Pressed() {
				b.Background(t.Selected)
			} else if b.Hovered() {
				b.Background(t.Hover)
			}
			b.Children(func() {
				ui.Icon(c, settingsIcon).Size(16, 16).TextColor(ink)
			}).OnClick(func() {})
		})
	})
}

func (a *app) sidebarRow(c *ui.Context, t themeValues, base *ui.Theme, id, label string, icon *ui.SVG, count int, entry *collection) {
	chosen := a.navigation.selected == id
	ink, fill := t.TextSecondary, ui.Transparent
	if chosen {
		ink, fill = t.TextPrimary, t.Selected
	}
	itemTheme := *base
	itemTheme.Accent, itemTheme.AccentText = ink, ink
	c.SetTheme(&itemTheme)
	tooltip := label
	if id == sectionTrash {
		tooltip = "Trash · Automatically deleted after 30 days"
	}
	item := ui.SidebarItem(c, id, nil, label).Height(32).Padding(6, 8, 6, 32).Gap(8).Radius(8).
		Background(fill).TextColor(ink).MinWidth(0).
		Label(fmt.Sprintf("%s, %d %s", label, count, captureNoun(count))).Tooltip(tooltip)
	hovered := item.Hovered()
	if hovered {
		ink = t.TextPrimary
		item.TextColor(ink)
	}
	item.Children(func() {
		ui.Icon(c, icon).Absolute().Left(8).Top(8).Size(16, 16).TextColor(ink)
		countInk := t.TextSecondary
		if chosen || hovered {
			countInk = t.TextPrimary
		}
		ui.Text(c, fmt.Sprint(count)).MinWidth(9).Shrink(0).TextAlign(ui.End).
			FontSize(12).FontWeight(400).FixedLineHeight(16).FontFeatures("tnum").TextColor(countInk).SingleLine()
	})
	if entry != nil {
		frozen := *entry
		item.HandleInput(func(ev ui.InputEvent) bool {
			if contextInvocation(ev) {
				a.contextSidebarCollection = frozen
			}
			return false
		}).ContextMenu(func(m *ui.Menu) { a.collectionRowMenu(frozen.ID, m) })
	}
	last := ui.Local(item, "revealed", func() string { return "" })
	if chosen && *last != id {
		item.ScrollIntoView()
		*last = id
	} else if !chosen {
		*last = ""
	}
	c.SetTheme(base)
}

func (a *app) sidebarAddCapture(c *ui.Context, t themeValues, x float32) {
	b := ui.ButtonBase(c).Key("add-capture").Absolute().Left(x).Top(sidebarControlTop).
		Size(sidebarControlSize, sidebarControlSize).Padding(8).Radius(100).Role(ui.RoleButton).
		Label("Add capture").Tooltip("Add capture").
		FocusBind(&a.focus, focusAddCapture).
		Disabled(a.captureMenu == nil || a.loading || a.picking || a.loadError != nil).HandleInput(a.shellInput)
	ink := t.TextSecondary
	if b.Hovered() {
		ink = t.TextPrimary
	}
	if !b.IsDisabled() {
		if b.Pressed() {
			b.Background(t.Selected)
		} else if b.Hovered() {
			b.Background(t.Hover)
		}
	}
	b.Children(func() {
		ui.Icon(c, addCaptureIcon).Size(16, 16).TextColor(ink)
	})
	bounds := b.Bounds()
	b.OnClick(func() {
		a.window.Update(func() {
			if a.navigation.hidden || a.loading || a.loadError != nil || a.libraryInputBlocked() {
				return
			}
			a.focus = focusAddCapture
			a.captureMenu.show(bounds)
		})
	})
}

func (a *app) captureMenuClosed(result int32) {
	a.window.Update(func() {
		if a.navigation.hidden || a.libraryInputBlocked() {
			return
		}
		a.focus = focusAddCapture
		switch result {
		case captureMenuLink:
			a.addLink()
		case captureMenuMedia:
			a.addCapture()
		}
	})
}

func captureNoun(n int) string {
	if n == 1 {
		return "capture"
	}
	return "captures"
}

func (a *app) sidebarToggle(c *ui.Context, t themeValues, x float32) {
	label := a.sidebarActionLabel()
	b := ui.ButtonBase(c).Key("sidebar-toggle").Absolute().Left(x).Top(sidebarControlTop).
		Size(sidebarControlSize, sidebarControlSize).Padding(8).Radius(100).Role(ui.RoleButton).
		Label(label).Tooltip(label+" (⌃⌘S)").FocusBind(&a.focus, focusToggle).HandleInput(a.shellInput)
	ink := t.TextSecondary
	if b.Hovered() {
		ink = t.TextPrimary
	}
	if b.Pressed() {
		b.Background(t.Selected)
	} else if b.Hovered() {
		b.Background(t.Hover)
	}
	b.Children(func() {
		ui.Icon(c, sidebarToggleIcon).Size(16, 16).TextColor(ink)
	})
	b.OnClick(func() { a.setSidebarHidden(!a.navigation.hidden) })
}

func (a *app) sidebarActionLabel() string {
	if a.navigation.hidden {
		return "Show Sidebar"
	}
	return "Hide Sidebar"
}

func (a *app) setSidebarHidden(hidden bool) {
	a.navigation.hidden = hidden
	a.focus = focusNavigation
	if hidden {
		a.captureMenu.dismiss()
		a.focus = focusToggle
	}
	if a.sidebarMenu != nil {
		a.sidebarMenu.SetLabel(a.sidebarActionLabel())
	}
	if a.window != nil {
		a.window.Invalidate()
	}
}

func (a *app) installMenu() {
	a.sidebarMenu = &mygo.MenuItem{
		ID: "sidebar", Label: a.sidebarActionLabel(), Accelerator: "Ctrl+Cmd+S",
		Click: func(_ *mygo.MenuItem, _ *mygo.Window) { a.setSidebarHidden(!a.navigation.hidden) },
	}
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Label: "File", Submenu: []*mygo.MenuItem{
			{Label: "Media…", Accelerator: "Cmd+O", Click: func(_ *mygo.MenuItem, _ *mygo.Window) { a.addCapture() }},
			{Label: "Link…", Click: func(_ *mygo.MenuItem, _ *mygo.Window) { a.addLink() }},
		}},
		{Role: mygo.RoleEditMenu},
		{Label: "View", Submenu: []*mygo.MenuItem{
			a.sidebarMenu, mygo.Separator(), {Role: mygo.RoleToggleFullScreen},
		}},
		{Role: mygo.RoleWindowMenu},
	}))
}
