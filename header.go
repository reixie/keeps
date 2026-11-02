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

var headerSortIcon = headerIcon("sort")
var headerGridIcon = headerIcon("grid")

func (a *app) sidebarToggleX(c *ui.Context) float32 {
	if a.navigation.hidden {
		return max(96, c.TitleBar().Left+12)
	}
	return sidebarWidth - 12 - 32
}

func (a *app) sectionHeading() (string, *ui.SVG) {
	if id := a.activeCollectionID(); id != "" {
		for _, entry := range a.collections {
			if entry.ID == id {
				return entry.Name, collectionIcon(entry.Icon)
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
		left = max(left, a.sidebarToggleX(c)+32+headerPadding-contentPadding-1)
	}
	ui.Row(c).Key("header").Height(headerHeight).Shrink(0).
		Padding(0, headerPadding, 0, left).AlignItems(ui.Center).Gap(16).
		DragWindow().Label("Section header").Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(10).Children(func() {
			// Library's existing SVG has the exact Figma geometry. All section
			// and collection icons share the sidebar's semantic tinting.
			ui.Icon(c, icon).Size(16, 16).Shrink(0).TextColor(t.TextPrimary)
			ui.Text(c, title).Grow(1).MinWidth(0).Font("SF Pro, system-ui").
				FontSize(13).FontWeight(510).FixedLineHeight(20).TextColor(t.TextPrimary).
				SingleLine().Label(title).Tooltip(title)
		})
		ui.Row(c).Shrink(0).Gap(4).Children(func() {
			for _, action := range []struct {
				label string
				icon  *ui.SVG
			}{{"Sort", headerSortIcon}, {"Grid", headerGridIcon}} {
				b := ui.ButtonBase(c).Key("header-"+action.label).Size(32, 32).Shrink(0).
					Padding(8).Radius(100).Role(ui.RoleButton).Label(action.label).
					Tooltip(action.label)
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
					ui.Icon(c, action.icon).Size(16, 16).TextColor(ink)
				}).OnClick(func() {})
			}
		})
		ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(1).Background(t.Border)
	})
}
