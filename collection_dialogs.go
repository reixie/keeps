package main

import (
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type collectionForm struct {
	ID, Name, Icon       string
	Edit, Icons          bool
	NameError, SaveError string
}

type collectionDialogState struct {
	Open, Saving              bool
	ReturnFocus, Focus, Error string
	Form                      *collectionForm
	IDs                       []string
	SelectCreated             bool
}

func (a *app) openCollectionForm(existing *collection, ids []string, selectCreated bool) {
	if a.collectionDialog != nil || a.linkDialog != nil || a.loading || a.loadError != nil || a.store == nil {
		return
	}
	f := &collectionForm{Icon: "folder"}
	if existing != nil {
		f.ID, f.Name, f.Icon, f.Edit = existing.ID, existing.Name, existing.Icon, true
	}
	a.collectionDialog = &collectionDialogState{Open: true, ReturnFocus: a.focus, Focus: "collection-name", Form: f, IDs: slices.Clone(ids), SelectCreated: selectCreated}
}

func (a *app) closeCollectionDialog(d *collectionDialogState) {
	if d.Saving || a.collectionDialog != d {
		return
	}
	a.collectionDialog = nil
	a.focus = d.ReturnFocus
	if a.focus == "" {
		a.focus = focusLibrary
	}
	if a.navigation.hidden && (a.focus == focusNavigation || a.focus == "new-collection" || a.focus == focusAddCapture) {
		a.focus = focusToggle
	}
	a.reconcileVisible(a.visibleItems())
}

func (a *app) submitCollectionForm(d *collectionDialogState) {
	if d.Saving || d.Form == nil || a.collectionDialog != d {
		return
	}
	f := d.Form
	f.NameError = collectionNameError(f.Name, f.ID, a.collections)
	f.SaveError = ""
	if f.NameError != "" {
		d.Focus = "collection-name"
		return
	}
	if f.ID == "" {
		id, err := newCollectionID()
		if err != nil {
			f.SaveError = err.Error()
			return
		}
		f.ID = id
	}
	entry := collection{ID: f.ID, Name: strings.TrimSpace(f.Name), Icon: f.Icon}
	op := collectionOperation{IDs: slices.Clone(d.IDs)}
	if f.Edit {
		op.Edit = &entry
	} else {
		op.Create = []collection{entry}
		if len(d.IDs) > 0 {
			op.Changes = map[string]bool{entry.ID: true}
		}
	}
	d.Saving = true
	a.enqueueCollections(op, func(err error) {
		d.Saving = false
		if err != nil {
			if message := collectionNameError(f.Name, f.ID, a.collections); message != "" {
				f.NameError = message
			} else {
				f.SaveError = err.Error()
			}
			// Name conflicts discovered under the lock belong beside Name too.
			if strings.Contains(err.Error(), "name") {
				f.NameError, f.SaveError = err.Error(), ""
			}
			a.announce(err.Error())
			return
		}
		a.closeCollectionDialog(d)
		if d.SelectCreated && !f.Edit {
			a.navigation.selected = collectionSection(entry.ID)
			a.libraryScroll.Y = 0
			a.focus = focusNavigation
			if a.navigation.hidden {
				a.focus = focusLibrary
			}
		}
	})
}

func (a *app) collectionsModal(c *ui.Context, t themeValues) {
	d := a.collectionDialog
	if d == nil {
		return
	}
	w, h := c.Size()
	widgetTheme := *c.Theme()
	widgetTheme.Background = t.BackgroundSecondary
	widgetTheme.Surface = t.BackgroundPrimary
	widgetTheme.Border = t.Border
	widgetTheme.Text = t.TextPrimary
	base := c.Theme()
	c.SetTheme(&widgetTheme)
	panel := ui.DialogBase(c, &d.Open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.32))
		panel.Width(360).MaxWidth(max(200, w-40)).MaxHeight(max(140, h-40)).Padding(20).Gap(12).
			Radius(12).Background(t.BackgroundSecondary).Border(1, t.Border).
			Shadow(0, 10, 30, 0, ui.RGBA(0, 0, 0, 0.25))
		a.collectionFormView(c, t, d)
		panel.OnShortcut(0, ui.KeyEnter, func() {
			if d.Form != nil && !d.Form.Icons {
				a.submitCollectionForm(d)
			}
		})
	})
	if panel.Dismissed() {
		d.Open = true
		if !d.Saving {
			a.closeCollectionDialog(d)
		}
	}
	c.SetTheme(base)
}

func (a *app) collectionFormView(c *ui.Context, t themeValues, d *collectionDialogState) {
	f := d.Form
	title, action := "New Collection", "Create"
	if f.Edit {
		title, action = "Edit Collection", "Save"
	}
	ui.Text(c, title).FontSize(15).FontWeight(600).TextColor(t.TextPrimary)
	ui.Field(c, "Name", func() {
		ui.TextInput(c.Key("collection-name"), &f.Name).Label("Name").FillWidth().
			FocusBind(&d.Focus, "collection-name").AutoFocus().Disabled(d.Saving).
			OnChange(func() { f.NameError, f.SaveError = "", "" }).OnSubmit(func() { a.submitCollectionForm(d) })
	}).Error(f.NameError)
	ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Text(c, "Icon").FontSize(13).TextColor(t.TextPrimary)
		control := ui.ButtonBase(c).Key("collection-icon").Padding(6, 10).Gap(8).Radius(6).Border(1, t.Border).
			Label("Icon: "+collectionIconLabel(f.Icon)).Tooltip("Choose collection icon").
			FocusBind(&d.Focus, "collection-icon").Disabled(d.Saving)
		if control.Hovered() {
			control.Background(t.Hover)
		}
		if control.Pressed() || f.Icons {
			control.Background(t.Selected)
		}
		control.Children(func() {
			ui.Icon(c, collectionIcon(f.Icon)).Size(16, 16).TextColor(t.TextSecondary)
			ui.Text(c, collectionIconLabel(f.Icon)).FontSize(13)
		}).OnClick(func() {
			f.Icons = !f.Icons
			if f.Icons {
				key := "folder"
				for _, icon := range collectionIcons {
					if icon.key == f.Icon {
						key = icon.key
					}
				}
				d.Focus = "icon:" + key
			}
		})
	})
	if f.Icons {
		ui.Column(c).Key("icon-catalog").Gap(4).Label("Collection icons").Children(func() {
			for start := 0; start < len(collectionIcons); start += 6 {
				ui.Row(c).Gap(4).Children(func() {
					for _, item := range collectionIcons[start:min(start+6, len(collectionIcons))] {
						fill := ui.Transparent
						if item.key == f.Icon {
							fill = t.Selected
						}
						button := ui.ButtonBase(c).Key(item.key).Size(36, 32).Padding(8).Radius(6).Background(fill).
							Label(item.label+" icon").Tooltip(item.label).Checked(item.key == f.Icon).
							FocusBind(&d.Focus, "icon:"+item.key).Disabled(d.Saving)
						if button.Hovered() {
							button.Background(t.Hover)
						}
						if button.Pressed() {
							button.Background(t.Selected)
						}
						button.Children(func() {
							ui.Icon(c, item.icon).Size(16, 16).TextColor(t.TextSecondary)
						}).OnClick(func() { f.Icon, f.Icons, d.Focus = item.key, false, "collection-icon" })
					}
				})
			}
		})
	}
	if f.SaveError != "" {
		ui.Text(c, f.SaveError).FontSize(12).TextColor(c.Theme().Danger).Role(ui.RoleStatus)
	}
	ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
		ui.Button(c, "Cancel").Disabled(d.Saving).OnClick(func() { a.closeCollectionDialog(d) })
		ui.PrimaryButton(c, action).Disabled(d.Saving).OnClick(func() { a.submitCollectionForm(d) })
	})
}

func (a *app) collectionRowMenu(id string, m *ui.Menu) {
	entry := a.contextSidebarCollection
	if entry.ID != id || id == "" {
		return
	}
	if m.Item("Edit Collection…").Chosen() {
		a.contextSidebarCollection = collection{}
		a.openCollectionForm(&entry, nil, false)
		return
	}
	if m.Item("Delete Collection…").Chosen() {
		a.contextSidebarCollection = collection{}
		a.confirmDeleteCollection(entry)
	}
}

func (a *app) confirmDeleteCollection(entry collection) {
	if a.window == nil || a.collectionDialog != nil || a.linkDialog != nil {
		return
	}
	returnFocus := a.focus
	a.nativeDialogs++
	go func() {
		result, err := mygo.Dialog.Message(mygo.MessageOptions{Parent: a.window, Type: mygo.MessageWarning,
			Title: "Delete Collection", Message: "Delete “" + entry.Name + "”?",
			Detail:  "Captures stay in Library.",
			Buttons: []string{"Cancel", "Delete Collection"}, DefaultButton: 0, CancelButton: 0})
		a.update(func() {
			a.nativeDialogs--
			if a.closed {
				return
			}
			a.focus = returnFocus
			if err != nil {
				a.report("Could not delete collection", err.Error())
				return
			}
			if result.Button == 1 {
				a.enqueueCollections(collectionOperation{DeleteID: entry.ID}, nil)
			}
		})
	}()
}
