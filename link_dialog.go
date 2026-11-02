package main

import (
	"context"

	"github.com/egoist/mygo/ui"
)

type linkDialogState struct {
	Open, Preparing, Saving         bool
	URL, Validation, Error          string
	Destination, ReturnFocus, Focus string
	cancel                          context.CancelFunc
	prepared                        *preparedLink
}

func (a *app) addLink() {
	if a.collectionDialog != nil || a.linkDialog != nil || a.picking || a.loading || a.store == nil || a.loadError != nil {
		return
	}
	a.linkDialog = &linkDialogState{Open: true, Destination: a.activeCollectionID(), ReturnFocus: a.focus, Focus: "link-url"}
	if a.invalidate != nil {
		a.invalidate()
	}
}
func (a *app) closeLinkDialog(d *linkDialogState) {
	if a.linkDialog != d || d.Saving {
		return
	}
	if d.cancel != nil {
		d.cancel()
	}
	a.linkDialog = nil
	a.focus = d.ReturnFocus
	if a.focus == "" {
		a.focus = focusLibrary
	}
	if a.navigation.hidden && (a.focus == focusAddCapture || a.focus == focusNavigation) {
		a.focus = focusToggle
	}
	a.reconcileVisible(a.visibleItems())
}
func (a *app) submitLink(d *linkDialogState) {
	if a.linkDialog != d || d.Preparing || d.Saving {
		return
	}
	normalized, err := normalizeLink(d.URL)
	d.Validation = ""
	d.Error = ""
	if err != nil {
		d.Validation = err.Error()
		d.Focus = "link-url"
		return
	}
	d.Preparing = true
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	store := a.store
	go func() {
		prepared, err := store.prepareLink(ctx, normalized)
		a.update(func() {
			if a.closed || a.linkDialog != d || ctx.Err() != nil {
				if prepared != nil {
					prepared.cleanup()
				}
				return
			}
			d.Preparing = false
			if err != nil {
				cancel()
				d.Focus = "link-url"
				d.Error = "Could not prepare the link. Check available disk space and library permissions, then try Save Link again. " + err.Error()
				a.announce(d.Error)
				return
			}
			d.Saving = true
			d.prepared = prepared
			a.enqueueImport(func() (importInput, error) {
				return importInput{link: prepared, destination: d.Destination, cleanup: prepared.cleanup, completed: func(saveErr error) {
					d.Saving = false
					cancel()
					if saveErr != nil {
						d.Focus = "link-url"
						d.Error = "Could not save the link. Check available disk space and library permissions, then try Save Link again. " + saveErr.Error()
						a.announce(d.Error)
						return
					}
					a.closeLinkDialog(d)
				}}, nil
			})
		})
	}()
}
func (a *app) linkModal(c *ui.Context, t themeValues) {
	d := a.linkDialog
	if d == nil {
		return
	}
	w, h := c.Size()
	base := c.Theme()
	theme := *base
	theme.Background = t.BackgroundSecondary
	theme.Surface = t.BackgroundPrimary
	theme.Border = t.Border
	theme.Text = t.TextPrimary
	c.SetTheme(&theme)
	panel := ui.DialogBase(c, &d.Open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, .32))
		panel.Width(400).MaxWidth(max(200, w-40)).MaxHeight(max(140, h-40)).Padding(20).Gap(12).Radius(12).Background(t.BackgroundSecondary).Border(1, t.Border).Shadow(0, 10, 30, 0, ui.RGBA(0, 0, 0, .25))
		ui.Text(c, "Add Link").FontSize(15).FontWeight(600).TextColor(t.TextPrimary)
		ui.Field(c, "URL", func() {
			ui.TextInput(c.Key("link-url"), &d.URL).Label("URL").Placeholder("https://example.com").FillWidth().FocusBind(&d.Focus, "link-url").AutoFocus().Disabled(d.Preparing || d.Saving).
				OnChange(func() { d.Validation = ""; d.Error = "" }).OnSubmit(func() { a.submitLink(d) })
		}).Error(d.Validation)
		if d.Preparing || d.Saving {
			text := "Preparing capture…"
			if d.Saving {
				text = "Saving link…"
			}
			ui.Text(c, text).FontSize(12).TextColor(t.TextSecondary).Role(ui.RoleStatus)
		}
		if d.Error != "" {
			ui.Text(c, d.Error).FontSize(12).TextColor(c.Theme().Danger).Role(ui.RoleStatus)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			ui.Button(c, "Cancel").Disabled(d.Saving).OnClick(func() { a.closeLinkDialog(d) })
			ui.PrimaryButton(c, "Save Link").Disabled(d.Preparing || d.Saving).OnClick(func() { a.submitLink(d) })
		})
		panel.OnShortcut(0, ui.KeyEnter, func() { a.submitLink(d) })
	})
	if panel.Dismissed() {
		d.Open = true
		if !d.Saving {
			a.closeLinkDialog(d)
		}
	}
	c.SetTheme(base)
}

func linkSavedStatus(result importResult, p *preparedLink) string {
	text := "Link saved"
	if result.Duplicates > 0 {
		text = "Link already saved"
	}
	if result.InTrash > 0 {
		text = "Link already in Trash; restore it there"
	}
	return text
}

var linkGlobeIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.2"><circle cx="8" cy="8" r="6.3"/><ellipse cx="8" cy="8" rx="3" ry="6.3"/><path d="M1.7 8h12.6M2.5 4.8h11M2.5 11.2h11"/></svg>`))
