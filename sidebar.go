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
	settingsIcon      = reiconGlyph("settings")
	tagsIcon          = reiconGlyph("tags")
	newSpaceIcon      = reiconGlyph("plus")
	sections          = []struct {
		id, label string
		icon      *ui.SVG
	}{
		{sectionLibrary, "Library", sidebarIcon("library")},
		{sectionStarred, "Starred", sidebarIcon("starred")},
		{sectionTags, "Tags", tagsIcon},
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
				if section.id == sectionTags {
					a.sidebarTags(c, t)
				} else {
					a.sidebarRow(c, t, base, section.id, section.label, section.icon, a.sectionCount(section.id), nil)
				}
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
			if len(a.collections) == 0 && a.store != nil && !a.loading && a.loadError == nil && !a.switchingSpace {
				a.emptyCollections(c, t)
			}
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
		a.sidebarFooter(c, t)
	})
}

func (a *app) emptyCollections(c *ui.Context, t themeValues) {
	ui.Column(c).Key("empty-collections").FillWidth().Height(104).Shrink(0).
		Center().Gap(9).Children(func() {
		ui.Text(c, "No collections yet").FontSize(12).TextColor(t.TextSecondary)
		button := ui.ButtonBase(c).Key("empty-new-collection").Height(28).Padding(0, 11).
			Radius(14).Gap(6).Role(ui.RoleButton).Label("New Collection").
			FocusBind(&a.focus, "empty-new-collection").Background(t.Hover)
		if button.Pressed() || button.Hovered() {
			button.Background(t.Selected)
		}
		button.Children(func() {
			ui.Icon(c, collectionPlusIcon).Size(14, 14).TextColor(t.TextPrimary)
			ui.Text(c, "New Collection").FontSize(13).TextColor(t.TextPrimary)
		}).OnClick(func() { a.openCollectionForm(nil, nil, true) })
	})
}

func (a *app) sidebarTags(c *ui.Context, t themeValues) {
	const focusTags = "tags"
	button := ui.ButtonBase(c).Key(sectionTags).FillWidth().Height(32).Shrink(0).
		Padding(6, 8).Gap(8).Radius(8).AlignItems(ui.Center).Justify(ui.Start).
		Role(ui.RoleButton).Label(fmt.Sprintf("Tags, %d", len(a.tags))).Tooltip("Tags").FocusBind(&a.focus, focusTags).
		Disabled(a.controls == nil || a.loading || a.loadError != nil || a.switchingSpace)
	ink := sidebarButtonInk(button, t, false)
	button.Children(func() {
		ui.Icon(c, tagsIcon).Size(16, 16).TextColor(ink)
		ui.Text(c, "Tags").Grow(1).TextColor(ink)
		ui.Text(c, fmt.Sprint(len(a.tags))).MinWidth(9).Shrink(0).TextAlign(ui.End).
			FontSize(12).FontWeight(400).FixedLineHeight(16).FontFeatures("tnum").TextColor(ink)
	})
	bounds := button.Bounds()
	button.OnClick(func() {
		a.window.Update(func() {
			a.focus = focusTags
			a.showTags(bounds)
		})
	})
}

func (a *app) sidebarFooter(c *ui.Context, t themeValues) {
	ui.Row(c).Key("sidebar-footer").Height(56).Shrink(0).Padding(12).Gap(4).
		AlignItems(ui.Center).Children(func() {
		a.footerButton(c, t, "settings", "Settings", settingsIcon, false, a.controls == nil).
			OnClick(func() { a.window.Update(a.showSettings) })
		spaces, active := a.preferences.Spaces, a.preferences.ActiveSpace
		if a.libraryOverride {
			spaces, active = defaultPreferences().Spaces, defaultSpaceID
		}
		ui.ScrollHorizontal(c).Key("spaces").Grow(1).MinWidth(0).Height(32).
			Label("Spaces").Children(func() {
			ui.Row(c).Gap(4).Children(func() {
				for _, space := range spaces {
					if space.Hidden {
						continue
					}
					label := space.Name
					if space.ID == active {
						label += ", active Space"
					}
					a.spaceButton(c, t, space, label, space.ID == active)
				}
			})
		})
		a.footerButton(c, t, "new-space", "New Space", newSpaceIcon, false,
			a.controls == nil || !a.canEditSpaces()).
			OnClick(func() {
				a.window.Update(func() {
					if a.canEditSpaces() {
						state := newSpaceState{Icons: spaceIcons[:], Icon: spaceIcons[len(a.preferences.Spaces)%len(spaceIcons)]}
						a.controls.present(controlNewSpace, ui.Rect{}, state)
					}
				})
			})
	})
}

func (a *app) footerButton(c *ui.Context, t themeValues, key, label string, icon *ui.SVG, selected, disabled bool) ui.Element {
	button, ink := a.sidebarFooterButton(c, t, key, label, selected, disabled)
	button.Children(func() { ui.Icon(c, icon).Size(16, 16).TextColor(ink) })
	return button
}

type newSpaceState struct {
	Icons []string `json:"icons"`
	Icon  string   `json:"icon"`
}

func (a *app) spaceButton(c *ui.Context, t themeValues, space librarySpace, label string, selected bool) {
	button, ink := a.sidebarFooterButton(c, t, "space:"+space.ID, label, selected, a.loading || a.switchingSpace || a.spaceHidePending())
	if space.Emoji != "" {
		button.Padding(4)
	}
	button.Tooltip(space.Name).Children(func() {
		if space.Emoji == "" {
			ui.Icon(c, collectionIconFor(space.Icon).icon).Size(16, 16).TextColor(ink)
		} else {
			ui.Text(c, space.Emoji).Font("Apple Color Emoji, system-ui").FontSize(16).
				FixedLineHeight(20).SingleLine().TextColor(ink)
		}
	})
	button.OnClick(func() { a.window.Update(func() { a.selectSpace(space.ID) }) }).
		ContextMenu(func(menu *ui.Menu) { a.spaceContextMenu(space.ID, menu) })
}

func (a *app) spaceContextMenu(id string, menu *ui.Menu) {
	if menu.Item("Open Space").Disabled(a.spaceLocked() || id == a.preferences.ActiveSpace).Chosen() {
		a.window.Update(func() { a.selectSpace(id) })
	}
	if menu.Item("Edit Space…").Disabled(!a.canEditSpaces() || a.controls == nil).Chosen() {
		a.window.Update(func() { a.showSpaceSettings(id) })
	}
	if menu.Item("Show in Finder").Disabled(a.spaceFolder(id) == "").Chosen() {
		a.window.Update(func() { a.revealSpace(id) })
	}
	if id != defaultSpaceID {
		menu.Separator()
		if menu.Item("Hide from Sidebar").Disabled(!a.canEditSpaces()).Chosen() {
			a.window.Update(func() { a.setSpaceHidden(id, true, true) })
		}
	}
}

func (a *app) sidebarFooterButton(c *ui.Context, t themeValues, key, label string, selected, disabled bool) (ui.Element, ui.Color) {
	button := ui.ButtonBase(c).Key(key).Size(32, 32).Shrink(0).Padding(8).Radius(100).
		Role(ui.RoleButton).Label(label).Tooltip(label).FocusBind(&a.focus, key).
		Selected(selected).Disabled(disabled)
	return button, sidebarButtonInk(button, t, selected)
}

func sidebarButtonInk(button ui.Element, t themeValues, selected bool) ui.Color {
	if selected || button.Pressed() {
		button.Background(t.Selected)
	} else if button.Hovered() {
		button.Background(t.Hover)
	}
	if selected || button.Hovered() {
		return t.TextPrimary
	}
	return t.TextSecondary
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
			a.controls.dismiss(controlTags)
			a.captureMenu.show(bounds)
		})
	})
}

func (a *app) captureMenuClosed(result int32) {
	generation := a.spaceGeneration
	a.window.Update(func() {
		if a.navigation.hidden || a.spaceGeneration != generation || a.libraryInputBlocked() {
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
		a.controls.dismiss(controlTags)
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
		{Role: mygo.RoleAppMenu, Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			mygo.Separator(),
			{Label: "Settings…", Accelerator: "Cmd+,", Disabled: a.controls == nil,
				Click: func(*mygo.MenuItem, *mygo.Window) { a.showSettings() }},
			mygo.Separator(),
			{Role: mygo.RoleServices},
			mygo.Separator(),
			{Role: mygo.RoleHide}, {Role: mygo.RoleHideOthers}, {Role: mygo.RoleUnhide},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Label: "File", Submenu: []*mygo.MenuItem{
			{Label: "Media…", Accelerator: "Cmd+O", Click: func(_ *mygo.MenuItem, _ *mygo.Window) { a.addCapture() }},
			{Label: "Link…", Click: func(_ *mygo.MenuItem, _ *mygo.Window) { a.addLink() }},
		}},
		{Role: mygo.RoleEditMenu},
		{Label: "View", Submenu: []*mygo.MenuItem{
			{Label: "Search Captures", Accelerator: "Cmd+F", Click: func(_ *mygo.MenuItem, window *mygo.Window) {
				if window == a.window {
					a.showSearch()
				}
			}},
			a.sidebarMenu, mygo.Separator(), {Role: mygo.RoleToggleFullScreen},
		}},
		{Role: mygo.RoleWindowMenu},
	}))
}
