package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo"
)

const defaultSpaceID = "default"

type librarySpace struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Root   string `json:"root,omitempty"`
	Icon   string `json:"icon"`
	Emoji  string `json:"emoji,omitempty"`
	Hidden bool   `json:"hidden,omitempty"`
}

type spaceRequest struct {
	space           librarySpace
	create          bool
	hideID          string
	reportHideError bool
}

var spaceIcons = [...]string{"folder", "globe", "star", "camera", "palette", "book", "heart", "archive", "image", "lightbulb", "bookmark", "video"}

func (a *app) spaceLocked() bool {
	return a.preferencesError != nil || a.libraryOverride
}

func (a *app) selectSpace(id string) {
	if a.closed || a.spaceLocked() || a.switchingSpace || a.spaceHidePending() {
		return
	}
	if id == a.preferences.ActiveSpace {
		a.pendingSpace = nil
		a.refreshSettings()
		return
	}
	for _, space := range a.preferences.Spaces {
		if space.ID == id && !space.Hidden {
			a.queueSpace(spaceRequest{space: space})
			return
		}
	}
}

func (a *app) createSpace(name, root, icon, emoji string) {
	if a.closed || a.spaceLocked() || a.switchingSpace || a.spaceHidePending() {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		a.report("Could not create Space", "Enter a name for the Space.", "")
		return
	}
	if !validSpaceSymbol(icon, emoji) {
		a.report("Could not create Space", "Choose a Reicon icon or enter one emoji.", "")
		return
	}
	for _, existing := range a.preferences.Spaces {
		if strings.EqualFold(existing.Name, name) {
			a.report("Could not create Space", "A Space with that name already exists.", "")
			return
		}
	}
	id, err := newLibraryID()
	if err != nil {
		a.report("Could not create Space", err.Error(), "")
		return
	}
	a.queueSpace(spaceRequest{create: true, space: librarySpace{
		ID: id, Name: name, Root: root, Icon: icon, Emoji: emoji,
	}})
}

func (a *app) spaceHidePending() bool { return a.pendingSpace != nil && a.pendingSpace.hideID != "" }

// AppKit validates the emoji grapheme before submission. Check the persisted
// representation here without applying a second Unicode emoji database.
func validSpaceSymbol(icon, emoji string) bool {
	return slices.Contains(spaceIcons[:], icon) && len(emoji) <= 128 && utf8.ValidString(emoji) &&
		emoji == strings.TrimSpace(emoji) && !strings.ContainsFunc(emoji, unicode.IsControl)
}

func (a *app) spaceByID(id string) *librarySpace {
	for i := range a.preferences.Spaces {
		if a.preferences.Spaces[i].ID == id {
			return &a.preferences.Spaces[i]
		}
	}
	return nil
}

func (a *app) canEditSpaces() bool {
	return !a.closed && !a.spaceLocked() && !a.loading && !a.switchingSpace && a.pendingSpace == nil
}

func (a *app) editSpace(id, name, icon, emoji string) {
	space := a.spaceByID(id)
	if space == nil || !a.canEditSpaces() {
		return
	}
	if !validSpaceSymbol(icon, emoji) {
		a.settingsSpaceError = "Choose a Reicon icon or enter one emoji."
		a.refreshSettings()
		return
	}
	if id != defaultSpaceID {
		name = strings.TrimSpace(name)
		if name == "" {
			a.settingsSpaceError = "Enter a name for the Space."
			a.refreshSettings()
			return
		}
		if slices.ContainsFunc(a.preferences.Spaces, func(other librarySpace) bool {
			return other.ID != id && strings.EqualFold(other.Name, name)
		}) {
			a.settingsSpaceError = "A Space with that name already exists."
			a.refreshSettings()
			return
		}
		space.Name = name
	}
	space.Icon, space.Emoji = icon, emoji
	a.settingsSpaceError = ""
	savePreferences(a.preferences)
	a.refreshSettings()
}

func (a *app) setSpaceHidden(id string, hidden, reportHideError bool) {
	space := a.spaceByID(id)
	if space == nil || !a.canEditSpaces() || id == defaultSpaceID || space.Hidden == hidden {
		return
	}
	if hidden && id == a.preferences.ActiveSpace {
		a.queueSpace(spaceRequest{space: a.preferences.Spaces[0], hideID: id, reportHideError: reportHideError})
		return
	}
	space.Hidden = hidden
	a.settingsSpaceError = ""
	savePreferences(a.preferences)
	a.refreshSettings()
}

func (a *app) queueSpace(request spaceRequest) {
	a.pendingSpace = &request
	a.advanceSpaceSwitch()
	a.refreshSettings()
	if a.pendingSpace != nil {
		a.announce("Keeps will switch Spaces after the current work finishes.")
	}
	a.window.Invalidate()
}

func (a *app) advanceSpaceSwitch() {
	if a.closed || a.loading || a.switchingSpace || a.pendingSpace == nil ||
		a.libraryJobRunning || len(a.libraryJobs) != 0 || a.pasteCancel != nil || len(a.pasteQueue) != 0 ||
		a.picking || a.nativeDialogs > 0 || a.collectionDialog != nil || a.linkDialog != nil || a.tagEditor != nil {
		return
	}
	request := *a.pendingSpace
	a.pendingSpace = nil
	a.switchingSpace = true
	a.refreshSettings()
	a.copyGeneration++
	a.captureMenu.dismiss()
	a.controls.dismiss(controlTags)
	spaces := slices.Clone(a.preferences.Spaces)

	go func() {
		space := request.space
		var store *libraryStore
		var snapshot manifest
		var err error
		if request.create {
			space.Root, err = createSpaceRoot(space, spaces)
		}
		if err == nil {
			store, snapshot, err = loadSpace(space)
		}
		a.window.Update(func() {
			if a.closed {
				return
			}
			a.switchingSpace = false
			if err != nil {
				if request.hideID != "" {
					a.settingsSpaceError = "Could not open Default. This Space is still visible. " + err.Error()
					a.refreshSettings()
					if request.reportHideError {
						a.report("Could not hide Space", a.settingsSpaceError, "")
					}
					return
				}
				a.refreshSettings()
				title := "Could not open Space"
				if request.create {
					title = "Could not create Space"
				}
				a.report(title, err.Error(), "")
				return
			}
			if request.create {
				a.preferences.Spaces = append(a.preferences.Spaces, space)
			}
			if request.hideID != "" {
				for i := range a.preferences.Spaces {
					if a.preferences.Spaces[i].ID == request.hideID {
						a.preferences.Spaces[i].Hidden = true
					}
				}
			} else if a.settingsSpaceID == a.preferences.ActiveSpace {
				a.settingsSpaceID = space.ID
			}
			a.preferences.ActiveSpace = space.ID
			savePreferences(a.preferences)
			a.spaceGeneration++
			a.loadError, a.cleanupError = nil, ""
			a.cardMenu = nil
			a.contextSidebarCollection = collection{}
			a.pointerActivation = ""
			a.filters = captureFilters{}
			a.browse = browseState{}
			a.selection.clear()
			a.grid = captureGrid{}
			a.navigation.selected, a.focus = sectionLibrary, focusLibrary
			a.media.resetForSpace()
			a.thumbnails.close()
			a.thumbnails = nil
			a.items, a.collections, a.tags = nil, nil, nil
			a.installLibrary(store, snapshot)
			a.refreshSettings()
			a.announce("Opened " + space.Name)
		})
	}()
}

func loadSpace(space librarySpace) (*libraryStore, manifest, error) {
	root := space.Root
	if root == "" {
		var err error
		root, err = defaultLibraryRoot()
		if err != nil {
			return nil, manifest{}, err
		}
	} else {
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			return nil, manifest{}, err
		}
		if resolved != root {
			return nil, manifest{}, fmt.Errorf("The Space folder now points to a different location.")
		}
	}
	store := &libraryStore{root: root}
	snapshot, err := store.loadSnapshot()
	return store, snapshot, err
}

func createSpaceRoot(space librarySpace, existing []librarySpace) (string, error) {
	root := space.Root
	create := root == ""
	if create {
		dir, err := mygo.App.Path(mygo.PathUserData)
		if err != nil {
			return "", err
		}
		dir = filepath.Join(dir, "Spaces")
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", err
		}
		root = filepath.Join(dir, space.ID)
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("Choose an absolute folder path.")
	}
	resolved, err := resolveFuturePath(root)
	if err != nil {
		return "", err
	}
	for _, other := range existing {
		path := other.Root
		if path == "" {
			path, err = defaultLibraryRoot()
			if err != nil {
				return "", err
			}
		}
		path, err = resolveFuturePath(path)
		if err != nil {
			return "", err
		}
		if pathContains(path, resolved) || pathContains(resolved, path) {
			return "", fmt.Errorf("Choose a folder separate from the %s library.", other.Name)
		}
	}
	if create {
		if err := os.Mkdir(resolved, 0700); err != nil {
			return "", err
		}
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return "", err
	}
	if len(entries) > 0 {
		return "", fmt.Errorf("Choose an empty folder for the new Space.")
	}
	probe, err := os.CreateTemp(resolved, ".keeps-write-")
	if err != nil {
		return "", err
	}
	closeErr := probe.Close()
	removeErr := os.Remove(probe.Name())
	if closeErr != nil {
		return "", closeErr
	}
	return resolved, removeErr
}

// Resolve existing ancestors too, since the default library can be absent.
func resolveFuturePath(path string) (string, error) {
	path = filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(path)
	if os.IsNotExist(err) {
		parent, resolveErr := resolveFuturePath(filepath.Dir(path))
		if resolveErr != nil {
			return "", resolveErr
		}
		return filepath.Join(parent, filepath.Base(path)), nil
	}
	return resolved, err
}

func pathContains(parent, path string) bool {
	info, _ := os.Stat(parent)
	for {
		if parent == path {
			return true
		}
		if info != nil {
			if ancestor, err := os.Stat(path); err == nil && os.SameFile(info, ancestor) {
				return true
			}
		}
		dir := filepath.Dir(path)
		if dir == path {
			return false
		}
		path = dir
	}
}
