package main

import (
	"fmt"
	"slices"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (a *app) openCapture(item capture) {
	if a.switchingSpace || a.linkDialog != nil || a.collectionDialog != nil || a.purging {
		return
	}
	if item.isLink() {
		if !slices.ContainsFunc(a.visibleItems(), func(current capture) bool { return current.ID == item.ID }) {
			return
		}
		if err := mygo.Shell.OpenExternal(item.URL); err != nil {
			a.report("Could not open link", err.Error(), "")
		}
		return
	}
	if a.media != nil {
		a.showPreview(item)
	}
}

const (
	linkCaptionGap       = 8
	linkCaptionRowHeight = 20
	linkCaptionHeight    = linkCaptionGap + linkCaptionRowHeight
)

func (a *app) libraryView(c *ui.Context, t themeValues) {
	if a.media != nil {
		a.media.desired = nil
	}
	items := a.visibleItems()
	a.reconcileVisible(items)
	width, height := c.Size()
	width -= 2 * contentPadding
	if !a.navigation.hidden {
		width -= sidebarWidth
	}
	width -= 2 * panelBorderWidth
	top := headerHeight
	if a.hasBrowseConditions() {
		top += browseBarHeight
	}
	var selected *capture
	if len(a.selection.order) == 1 {
		for i := range items {
			if a.selection.has(items[i].ID) {
				selected = &items[i]
				break
			}
		}
	}
	viewportHeight := height - 2*contentPadding - top - 2*panelBorderWidth
	if len(items) > 0 {
		viewportHeight -= browseBarHeight
	}
	viewportHeight = max(0, viewportHeight)
	panel := ui.Column(c).Fill().Background(t.BackgroundPrimary).Radius(panelRadius).Border(panelBorderWidth, t.Border).Label("Main content").Clip().
		FocusBind(&a.focus, focusLibrary).HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind == ui.InputPointerDown && ev.Button == 0 {
			a.focus = focusLibrary
		}
		return a.libraryInput(ev, true)
	})
	// Only this element consumes native files. Mygo routes native data to this
	// legacy file target as a fallback, never to two app callbacks.
	over := panel.FileDragOver()
	if paths := panel.DroppedFiles(); len(paths) > 0 {
		a.dropFiles(paths)
	}
	panel.Children(func() {
		defer func() {
			if over {
				ui.Box(c).Absolute().Left(8).Right(8).Top(top).Bottom(8).Radius(8).
					Background(t.Selected.Alpha(0.8)).Children(func() {
					ui.Text(c, "Drop to add").Absolute().Left(16).Top(16).Padding(8, 12).
						Radius(8).Background(t.BackgroundPrimary).TextColor(t.TextPrimary).FontSize(13)
				})
			}
		}()
		a.header(c, t)
		if a.hasBrowseConditions() {
			ui.Row(c).Key("browse-conditions").Height(browseBarHeight).Shrink(0).Padding(0, headerPadding).
				Gap(8).AlignItems(ui.Center).Background(t.BackgroundSecondary).Children(func() {
				conditions := a.browseConditions()
				ui.Text(c, conditions).SingleLine().Grow(1).MinWidth(0).TextColor(t.TextSecondary).Tooltip(conditions)
				a.browseButton(c, t, "clear-browse", "Clear All").OnClick(func() { a.window.Update(a.clearBrowse) })
			})
		}
		if !a.captureSection() {
			return
		}
		if len(items) == 0 {
			a.grid.reset()
			ui.Column(c).Grow(1).Center().Gap(8).Children(func() {
				title, detail := "No captures yet", ""
				if a.navigation.selected == sectionStarred {
					title = "No starred captures"
				}
				if a.navigation.selected == sectionTrash {
					title, detail = "Trash is empty.", ""
				}
				if a.hasBrowseConditions() {
					title = "No matching captures"
				}
				if a.loading {
					title, detail = "Loading library…", ""
				}
				if a.loadError != nil {
					title, detail = "Library could not be loaded", libraryLoadRecovery+"\n\n"+a.loadError.Error()
				}
				ui.Text(c, title).FontSize(15).TextColor(t.TextPrimary).MaxWidth(max(100, width-64)).TextAlign(ui.Center)
				if detail != "" {
					ui.Text(c, detail).FontSize(12).FixedLineHeight(17).TextColor(t.TextTertiary).MaxWidth(max(100, width-64)).TextAlign(ui.Center)
				}
				if !a.loading && !a.switchingSpace && a.loadError == nil && a.store != nil {
					if a.hasBrowseConditions() {
						a.browseButton(c, t, "clear-empty-browse", "Clear Search and Filters").OnClick(func() { a.window.Update(a.clearBrowse) })
					} else if a.navigation.selected == sectionLibrary || a.activeCollectionID() != "" {
						a.browseButton(c, t, "empty-import", "Import Media…").OnClick(func() { a.window.Update(a.addCapture) })
					}
				}
			})
			return
		}
		a.grid.updateLayout(items, width, viewportHeight, a.browseMinimum(), a.focus)
		if a.media != nil {
			a.media.desired = playbackCandidates(items, a.grid.layout, a.grid.scroll.Y, viewportHeight, a.media.stillGIFs)
		}
		ui.Scroll(c).Key("library-scroll").Grow(1).MinHeight(0).TrackScroll(&a.grid.scroll).Label("Capture library").Children(func() {
			ui.Box(c).Width(width).Height(a.grid.layout.Height).Shrink(0).Children(func() {
				for i, item := range items {
					r := a.grid.layout.Cards[i]
					// Retain at most one focused offscreen card so reflow/scrolling
					// cannot strand keyboard commands; it requests no thumbnail.
					if !visibleCard(r, a.grid.scroll.Y, viewportHeight) && a.focus != captureFocusPrefix+item.ID {
						continue
					}
					a.captureCard(c, t, item, i, items, r, viewportHeight)
				}
			})
		})
		if selected != nil {
			a.tagBar(c, t, *selected)
		} else {
			ui.Row(c).Key("capture-count").Height(browseBarHeight).Shrink(0).Padding(0, headerPadding).
				AlignItems(ui.Center).Background(t.BackgroundSecondary).Children(func() {
				label := fmt.Sprintf("%d %s", len(items), captureNoun(len(items)))
				if count := len(a.selection.order); count > 1 {
					label = fmt.Sprintf("%d selected", count)
				}
				ui.Text(c, label).FontSize(12).TextColor(t.TextSecondary)
			})
		}
	})
}

func (a *app) captureCard(c *ui.Context, t themeValues, item capture, index int, items []capture, r cardRect, viewportHeight float32) {
	label := fmt.Sprintf("%s, %s, %d by %d. Preview original", item.Filename, item.MediaType, item.Width, item.Height)
	tooltip := item.Filename
	imageHeight := r.H
	if item.isLink() {
		label = item.Title + ", " + item.URL + ". Open link; Space previews saved capture"
		tooltip = item.Title + "\n" + item.URL
		imageHeight -= linkCaptionHeight
	}
	card := ui.ButtonBase(c).Key(item.ID).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).
		Padding(0).Radius(8).Selected(a.selection.has(item.ID)).Background(t.BackgroundSecondary).Role(ui.RoleButton).Label(label).
		Tooltip(tooltip).FocusBind(&a.focus, captureFocusPrefix+item.ID)
	if item.isLink() {
		card.Background(ui.Transparent)
	}
	if a.selection.has(item.ID) {
		// A paint-only inset keeps the existing masonry and image geometry.
		card.Background(t.Selected)
	}
	card.HandleInput(func(ev ui.InputEvent) bool {
		if a.switchingSpace || a.collectionDialog != nil || a.linkDialog != nil || a.previewOwnsFocus() {
			return false
		}
		if contextInvocation(ev) {
			a.prepareCaptureContextMenu(item.ID)
			return false
		}
		if a.libraryInput(ev, true) {
			return true
		}
		if ev.Kind == ui.InputPointerDown && ev.Button == 0 {
			a.selection.click(item.ID, ev.Mods&ui.Cmd != 0)
			a.pointerActivation = item.ID
		} else if ev.Kind == ui.InputKeyDown {
			a.pointerActivation = ""
			next := index
			switch ev.Key {
			case ui.KeyTab:
				if ev.Mods&ui.Shift != 0 {
					next--
				} else {
					next++
				}
				if next < 0 || next >= len(items) {
					return false
				}
			case ui.KeyLeft, ui.KeyUp:
				next--
			case ui.KeyRight, ui.KeyDown:
				next++
			case ui.KeyHome:
				next = 0
			case ui.KeyEnd:
				next = len(items) - 1
			default:
				return false
			}
			next = max(0, min(next, len(items)-1))
			a.focus = captureFocusPrefix + items[next].ID
			a.grid.revealCard(next, viewportHeight)
			return true
		}
		return false
	})
	card.ContextMenu(func(m *ui.Menu) { a.captureContextMenu(item.ID, m) })
	card.OnClick(func() {
		// Pointer presses select/focus; keyboard and native AX Press activate.
		if a.pointerActivation != item.ID {
			a.openCapture(item)
		}
		a.pointerActivation = ""
	})
	if card.DoubleClicked() {
		a.openCapture(item)
	}
	card.Children(func() {
		if item.isLink() {
			ui.Box(c).Absolute().Left(0).Top(0).Size(r.W, imageHeight).Radius(8).Background(t.BackgroundSecondary)
		}
		var playing *ui.Bitmap
		if a.media != nil {
			if frame := a.media.frames[item.ID]; frame != nil && !frame.failed {
				playing = frame.bitmap
			}
		}
		if a.thumbnails != nil && visibleCard(r, a.grid.scroll.Y, viewportHeight) {
			entry := a.thumbnails.get(item)
			if entry != nil && entry.bitmap != nil && playing == nil {
				ui.Image(c, entry.bitmap).Absolute().Left(0).Top(0).Size(r.W, imageHeight).Fit(ui.Contain).Radius(8)
			}
			if item.isLink() {
				ui.Row(c).Absolute().Left(0).Top(imageHeight+linkCaptionGap).Size(r.W, linkCaptionRowHeight).AlignItems(ui.Center).Gap(6).Children(func() {
					if entry != nil && entry.favicon != nil {
						ui.Image(c, entry.favicon).Size(16, 16).Shrink(0).Fit(ui.Contain)
					} else {
						ui.Icon(c, linkGlobeIcon).Size(16, 16).Shrink(0).TextColor(t.TextSecondary)
					}
					ui.Text(c, item.Title).Grow(1).MinWidth(0).FontSize(13).FontWeight(500).FixedLineHeight(linkCaptionRowHeight).TextColor(t.TextPrimary).SingleLine()
				})
			}
			if entry != nil && entry.err != nil {
				ui.Text(c, "Preview unavailable").FontSize(12).FixedLineHeight(17).TextColor(t.TextSecondary)
			}
		}
		if playing != nil {
			ui.Image(c, playing).Absolute().Left(0).Top(0).Size(r.W, imageHeight).Fit(ui.Contain).Radius(8)
		}
		// A separate last-painted image overlay keeps the stroke above still
		// thumbnails and live frames without consuming any layout or hit area.
		ui.Box(c).Absolute().Left(0).Top(0).Size(r.W, imageHeight).Radius(8).Border(1, t.ImageStroke)
		if a.selection.has(item.ID) {
			ui.Box(c).Absolute().Left(0).Top(0).Size(r.W, r.H).Radius(8).
				Border(3, c.Theme().Accent).Label("Selected capture")
			// A one-point on-accent keyline stays inside the existing stripe.
			// The two tones keep selection visible against varied media while
			// retaining the native accent, card geometry and hit target.
			ui.Box(c).Absolute().Left(1).Top(1).Size(r.W-2, r.H-2).Radius(7).
				Border(1, c.Theme().AccentText)
		}
		if item.MediaType == "video" {
			ui.Text(c, "▶").Absolute().Left(8).Bottom(8).Padding(3, 6).Radius(5).
				Background(ui.RGBA(0, 0, 0, 0.55)).TextColor(ui.Hex("#FFFFFF")).FontSize(11)
		}
	})
}
