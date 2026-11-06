package main

import (
	"encoding/json"
	"strings"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Keep these panel and action values aligned with library_controls.swift.
const (
	controlTags int32 = iota + 1
	controlSettings
	controlNewSpace
	controlTagEditor
)

const (
	controlSelectTag int32 = iota + 1
	controlCreateTag
	controlCreateSpace
	controlAppearance
	controlTranslucency
	controlSettingsSpace
	controlRevealSpace
	controlCheckUpdates
	controlOpenRelease
	controlRenameTag
	controlColorTag
	controlDeleteTag
	controlSubmitTagEditor
	controlCancelTagEditor
	controlEditSpace
	controlToggleSpace
)

type nativeLibraryControls struct {
	handle  uintptr
	show    func(uintptr, int32, string, float64, float64, float64, float64)
	refresh func(uintptr, int32, string)
	close   func(uintptr, int32)
	destroy func(uintptr)
}

func newLibraryControls(window uintptr, onAction func(int32, string)) (*nativeLibraryControls, error) {
	resources, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		return nil, err
	}
	controls := &nativeLibraryControls{}
	var create func(uintptr, string, uintptr) uintptr
	if err := bindKeepsNative(map[string]any{
		"KeepsControlsCreate":  &create,
		"KeepsControlsShow":    &controls.show,
		"KeepsControlsRefresh": &controls.refresh,
		"KeepsControlsClose":   &controls.close,
		"KeepsControlsDestroy": &controls.destroy,
	}); err != nil {
		return nil, err
	}
	controls.handle = create(window, resources, purego.NewCallback(func(action int32, value *byte, length int32) {
		onAction(action, string(unsafe.Slice(value, int(length))))
	}))
	return controls, nil
}

func (n *nativeLibraryControls) present(panel int32, bounds ui.Rect, state any) {
	if n != nil {
		data, _ := json.Marshal(state)
		n.show(n.handle, panel, string(data), float64(bounds.X), float64(bounds.Y), float64(bounds.W), float64(bounds.H))
	}
}

func (n *nativeLibraryControls) update(panel int32, state any) {
	if n != nil {
		data, _ := json.Marshal(state)
		n.refresh(n.handle, panel, string(data))
	}
}

func (n *nativeLibraryControls) dismiss(panel int32) {
	if n != nil {
		n.close(n.handle, panel)
	}
}

func (n *nativeLibraryControls) dispose() {
	if n != nil {
		n.destroy(n.handle)
	}
}

func (a *app) nativeControlAction(action int32, value string) {
	generation := a.spaceGeneration
	a.window.Update(func() {
		if a.closed {
			return
		}
		switch action {
		case controlSelectTag:
			if a.spaceGeneration == generation && !a.libraryInputBlocked() && hasTag(a.tags, value) {
				a.filters.tag = value
				a.filtersChanged()
			}
		case controlCreateTag:
			if a.spaceGeneration == generation && !a.libraryInputBlocked() {
				a.changeTags("Could not create tag", func(store *libraryStore) (manifest, error) {
					return store.saveTag("", value, nil)
				})
			}
		case controlRenameTag, controlColorTag, controlDeleteTag:
			if a.spaceGeneration != generation || a.libraryInputBlocked() {
				return
			}
			id, color, _ := strings.Cut(value, ":")
			for _, tag := range a.tags {
				if tag.ID != id {
					continue
				}
				switch action {
				case controlRenameTag:
					a.openTagEditor(tag, nil)
				case controlColorTag:
					a.changeTags("Could not update tag color", func(store *libraryStore) (manifest, error) {
						return store.editTag(id, color, false)
					})
				case controlDeleteTag:
					a.confirmDeleteTag(tag)
				}
				break
			}
		case controlSubmitTagEditor:
			a.submitTagEditor(value)
		case controlCancelTagEditor:
			if a.tagEditor != nil && !a.tagEditor.Busy {
				a.closeTagEditor()
			}
		case controlCreateSpace:
			var request struct{ Name, Root, Icon, Emoji string }
			if err := json.Unmarshal([]byte(value), &request); err != nil {
				a.report("Could not create Space", err.Error(), "")
				return
			}
			a.createSpace(request.Name, request.Root, request.Icon, request.Emoji)
		case controlAppearance:
			a.setAppearance(mygo.ThemeSource(value))
		case controlTranslucency:
			if a.preferencesError == nil {
				a.preferences.Translucent = value == "true"
				a.saveAppearance()
			}
		case controlSettingsSpace:
			a.settingsSpaceID = value
			a.settingsSpaceError = ""
			a.refreshSettings()
		case controlEditSpace:
			var request struct{ ID, Name, Icon, Emoji string }
			if json.Unmarshal([]byte(value), &request) == nil {
				a.editSpace(request.ID, request.Name, request.Icon, request.Emoji)
			}
		case controlToggleSpace:
			id, state, ok := strings.Cut(value, ":")
			if ok {
				a.setSpaceHidden(id, state == "true", false)
			}
		case controlRevealSpace:
			a.revealSpace(a.settingsSpaceID)
		case controlCheckUpdates:
			a.checkUpdates()
		case controlOpenRelease:
			if a.releaseURL != "" {
				if err := mygo.Shell.OpenExternal(a.releaseURL); err != nil {
					a.report("Could not open release", err.Error(), "")
				}
			}
		}
	})
}
