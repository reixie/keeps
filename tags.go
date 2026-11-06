package main

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

type libraryTag struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

var tagColors = [...]string{"", "red", "orange", "yellow", "green", "blue", "purple"}

var foldTagName = cases.Fold()

func tagNameKey(name string) string { return norm.NFC.String(foldTagName.String(name)) }

func validTagName(name string) bool {
	return name != "" && name == strings.TrimSpace(name) && !strings.ContainsFunc(name, unicode.IsControl)
}

func hasTag(tags []libraryTag, id string) bool {
	return slices.ContainsFunc(tags, func(tag libraryTag) bool { return tag.ID == id })
}

func validateTagMetadata(m manifest) error {
	ids := make(map[string]bool, len(m.Tags))
	names := make(map[string]bool, len(m.Tags))
	for _, tag := range m.Tags {
		name := tagNameKey(tag.Name)
		if !validCollectionID(tag.ID) || ids[tag.ID] || !validTagName(tag.Name) || names[name] || !slices.Contains(tagColors[:], tag.Color) {
			return fmt.Errorf("invalid tag %q", tag.ID)
		}
		ids[tag.ID] = true
		names[name] = true
	}
	for _, items := range [][]capture{m.Items, m.PendingPurges} {
		for _, item := range items {
			seen := make(map[string]bool, len(item.TagIDs))
			for _, id := range item.TagIDs {
				if !ids[id] || seen[id] {
					return fmt.Errorf("invalid tag membership on capture %q", item.ID)
				}
				seen[id] = true
			}
		}
	}
	return nil
}

func (s *libraryStore) saveTag(id, name string, targets []string) (manifest, error) {
	name = strings.TrimSpace(name)
	if !validTagName(name) {
		return manifest{}, fmt.Errorf("Enter a valid tag name.")
	}
	if id == "" {
		if err := os.MkdirAll(s.root, 0755); err != nil {
			return manifest{}, err
		}
	}
	return s.editManifest(func(m *manifest) error {
		key := tagNameKey(name)
		if slices.ContainsFunc(m.Tags, func(tag libraryTag) bool { return tag.ID != id && tagNameKey(tag.Name) == key }) {
			return fmt.Errorf("A tag with this name already exists.")
		}
		if id != "" {
			i := slices.IndexFunc(m.Tags, func(tag libraryTag) bool { return tag.ID == id })
			if i < 0 {
				return fmt.Errorf("This tag no longer exists.")
			}
			m.Tags[i].Name = name
			return nil
		}
		newID, err := newLibraryID()
		if err != nil {
			return err
		}
		m.Tags = append(m.Tags, libraryTag{ID: newID, Name: name})
		return applyTag(m.Items, targets, newID, true)
	})
}

func (s *libraryStore) assignTag(ids []string, tagID string, assign bool) (manifest, error) {
	return s.editManifest(func(m *manifest) error {
		if !hasTag(m.Tags, tagID) {
			return fmt.Errorf("This tag no longer exists.")
		}
		return applyTag(m.Items, ids, tagID, assign)
	})
}

func applyTag(items []capture, ids []string, tagID string, assign bool) error {
	for _, item := range items {
		if item.TrashedAt != nil && slices.Contains(ids, item.ID) {
			return fmt.Errorf("Restore captures from Trash before changing their tags.")
		}
	}
	return editCaptureTargets(items, ids, func(item *capture) {
		if assign && !slices.Contains(item.TagIDs, tagID) {
			item.TagIDs = append(item.TagIDs, tagID)
		} else if !assign {
			item.TagIDs = slices.DeleteFunc(item.TagIDs, func(id string) bool { return id == tagID })
		}
	})
}

func (s *libraryStore) editTag(id, color string, remove bool) (manifest, error) {
	return s.editManifest(func(m *manifest) error {
		i := slices.IndexFunc(m.Tags, func(tag libraryTag) bool { return tag.ID == id })
		if i < 0 {
			return fmt.Errorf("This tag no longer exists.")
		}
		if !remove {
			if !slices.Contains(tagColors[:], color) {
				return fmt.Errorf("Choose a tag color from the palette.")
			}
			m.Tags[i].Color = color
			return nil
		}
		m.Tags = slices.Delete(m.Tags, i, i+1)
		for _, items := range [][]capture{m.Items, m.PendingPurges} {
			for i := range items {
				items[i].TagIDs = slices.DeleteFunc(items[i].TagIDs, func(value string) bool { return value == id })
			}
		}
		return nil
	})
}

type tagEditorState struct {
	Title string `json:"title"`
	Name  string `json:"name"`
	Error string `json:"error"`
	Busy  bool   `json:"busy"`
	id    string
	ids   []string
}

func (a *app) openTagEditor(tag libraryTag, ids []string) {
	if a.controls == nil || a.store == nil || a.loading || a.loadError != nil || a.libraryInputBlocked() {
		return
	}
	title := "New Tag"
	if tag.ID != "" {
		title = "Rename Tag"
	}
	a.controls.dismiss(controlTags)
	a.tagEditor = &tagEditorState{Title: title, Name: tag.Name, id: tag.ID, ids: slices.Clone(ids)}
	a.controls.present(controlTagEditor, ui.Rect{}, a.tagEditor)
}

func (a *app) closeTagEditor() {
	a.tagEditor = nil
	a.controls.dismiss(controlTagEditor)
	a.advanceSpaceSwitch()
}

func (a *app) submitTagEditor(name string) {
	d := a.tagEditor
	if d == nil || d.Busy {
		return
	}
	d.Name, d.Error, d.Busy = name, "", true
	a.controls.update(controlTagEditor, d)
	a.enqueueLibraryJob(libraryJob{run: func(store *libraryStore) func() {
		snapshot, err := store.saveTag(d.id, name, d.ids)
		return func() {
			if err != nil {
				d.Error, d.Busy = err.Error(), false
				a.controls.update(controlTagEditor, d)
				return
			}
			a.publishSnapshot(snapshot)
			a.announce("Tags updated")
			a.closeTagEditor()
		}
	}})
}

func (a *app) confirmDeleteTag(tag libraryTag) {
	a.controls.dismiss(controlTags)
	a.nativeMessage(mygo.MessageOptions{Type: mygo.MessageWarning, Title: "Delete Tag?",
		Message: "Delete “" + tag.Name + "”?", Detail: "This removes the tag from all captures in this Space, including Trash. Your captures are kept.",
		Buttons: []string{"Cancel", "Delete Tag"}, DefaultButton: 0, CancelButton: 0}, func(result mygo.MessageResult, err error) {
		if err == nil && result.Button == 1 {
			a.changeTags("Could not delete tag", func(store *libraryStore) (manifest, error) {
				return store.editTag(tag.ID, "", true)
			})
		} else if err != nil {
			a.report("Could not delete tag", err.Error(), "")
		}
	})
}

func (a *app) showTags(bounds ui.Rect) {
	if a.navigation.hidden || a.loading || a.store == nil || a.loadError != nil || a.libraryInputBlocked() {
		return
	}
	a.captureMenu.dismiss()
	a.controls.present(controlTags, bounds, a.tags)
}

func (a *app) changeTags(title string, edit func(*libraryStore) (manifest, error)) {
	if a.closed || a.switchingSpace || a.store == nil || a.loadError != nil {
		return
	}
	a.enqueueLibraryJob(libraryJob{run: func(store *libraryStore) func() {
		snapshot, err := edit(store)
		return func() {
			if err != nil {
				a.report(title, err.Error(), "")
				a.announce(title)
				return
			}
			a.publishSnapshot(snapshot)
			a.announce("Tags updated")
		}
	}})
}

var tagSwatches = map[string]ui.Color{
	"red": ui.Hex("#EF5350"), "orange": ui.Hex("#F59235"), "yellow": ui.Hex("#D4A928"),
	"green": ui.Hex("#41A45C"), "blue": ui.Hex("#4688E6"), "purple": ui.Hex("#A16ADD"),
}

func (a *app) tagBar(c *ui.Context, t themeValues, item capture) {
	ui.Row(c).Key("capture-tags").Height(browseBarHeight).Shrink(0).Padding(0, headerPadding).
		Gap(8).AlignItems(ui.Center).Background(t.BackgroundSecondary).Children(func() {
		if item.TrashedAt == nil {
			button := a.browseButton(c, t, "edit-capture-tags", "Tags…")
			bounds := button.Bounds()
			generation := a.spaceGeneration
			button.OnClick(func() {
				a.window.Update(func() {
					if generation == a.spaceGeneration && !a.libraryInputBlocked() {
						a.showCaptureTagsMenu(item.ID, bounds)
					}
				})
			})
		}
		ui.ScrollHorizontal(c).Grow(1).MinWidth(0).Height(25).Label("Assigned tags").Children(func() {
			ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
				if len(item.TagIDs) == 0 {
					ui.Text(c, "No tags").FontSize(12).TextColor(t.TextSecondary)
				}
				for _, tag := range a.tags {
					if !slices.Contains(item.TagIDs, tag.ID) {
						continue
					}
					ink, ok := tagSwatches[tag.Color]
					if !ok {
						ink = t.TextSecondary
					}
					ui.Row(c).Shrink(0).Height(22).Padding(0, 7).Gap(5).Radius(6).AlignItems(ui.Center).
						Background(ink.Alpha(0.15)).Children(func() {
						ui.Box(c).Size(6, 6).Radius(3).Background(ink)
						ui.Text(c, tag.Name).SingleLine().MaxWidth(200).FontSize(12).TextColor(t.TextPrimary).Tooltip(tag.Name)
					})
				}
			})
		})
	})
}

func (a *app) showCaptureTagsMenu(id string, bounds ui.Rect) {
	if len(a.selection.order) != 1 || !a.selection.has(id) {
		return
	}
	items := a.visibleItems()
	index := slices.IndexFunc(items, func(item capture) bool { return item.ID == id })
	if index < 0 || items[index].TrashedAt != nil {
		return
	}
	item := items[index]
	a.controls.dismiss(controlTags)
	generation := a.spaceGeneration
	active := func() bool { return a.spaceGeneration == generation && !a.libraryInputBlocked() }
	var entries []*mygo.MenuItem
	for _, tag := range a.tags {
		assigned := slices.Contains(item.TagIDs, tag.ID)
		entries = append(entries, &mygo.MenuItem{
			Label: tag.Name, Checked: assigned, Type: mygo.MenuItemCheckbox,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				if active() {
					a.changeTags("Could not update tags", func(store *libraryStore) (manifest, error) {
						return store.assignTag([]string{item.ID}, tag.ID, !assigned)
					})
				}
			},
		})
	}
	if len(entries) != 0 {
		entries = append(entries, mygo.Separator())
	}
	entries = append(entries, &mygo.MenuItem{Label: "New Tag…", Disabled: a.controls == nil,
		Click: func(*mygo.MenuItem, *mygo.Window) {
			if active() {
				a.openTagEditor(libraryTag{}, []string{item.ID})
			}
		},
	})
	mygo.NewMenu(entries).PopupAt(a.window, int(bounds.X), int(bounds.Y+bounds.H))
}
