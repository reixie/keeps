package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type settingsState struct {
	Appearance    mygo.ThemeSource `json:"appearance"`
	Translucent   bool             `json:"translucent"`
	CanEdit       bool             `json:"can_edit"`
	Spaces        []librarySpace   `json:"spaces"`
	SelectedSpace string           `json:"selected_space"`
	ActiveSpace   string           `json:"active_space"`
	SpaceIcons    []string         `json:"space_icons"`
	SpaceError    string           `json:"space_error"`
	CanEditSpace  bool             `json:"can_edit_space"`
	OpenSpaces    bool             `json:"open_spaces,omitempty"`
	StoragePath   string           `json:"storage_path"`
	Version       string           `json:"version"`
	UpdateStatus  string           `json:"update_status"`
	Checking      bool             `json:"checking"`
	ReleaseReady  bool             `json:"release_ready"`
}

func (a *app) settingsState() settingsState {
	state := settingsState{
		Appearance: a.preferences.Appearance, Translucent: a.preferences.Translucent,
		CanEdit: a.preferencesError == nil, Spaces: a.preferences.Spaces,
		SelectedSpace: a.settingsSpaceID, Version: mygo.App.Version(),
		ActiveSpace: a.preferences.ActiveSpace, SpaceIcons: spaceIcons[:], SpaceError: a.settingsSpaceError,
		CanEditSpace: a.canEditSpaces(),
		UpdateStatus: a.updateStatus, Checking: a.checkingUpdates, ReleaseReady: a.releaseURL != "",
	}
	if a.libraryOverride {
		state.Spaces = defaultPreferences().Spaces
		state.ActiveSpace = defaultSpaceID
	}
	state.StoragePath = a.spaceFolder(state.SelectedSpace)
	return state
}

func (a *app) showSettings() {
	a.presentSettings(false)
}

func (a *app) showSpaceSettings(id string) {
	if !a.canEditSpaces() || a.spaceByID(id) == nil {
		return
	}
	a.settingsSpaceID, a.settingsSpaceError = id, ""
	a.presentSettings(true)
}

func (a *app) presentSettings(openSpaces bool) {
	if a.closed {
		return
	}
	a.captureMenu.dismiss()
	a.controls.dismiss(controlTags)
	if a.settingsSpaceID == "" {
		a.settingsSpaceID = a.preferences.ActiveSpace
	}
	if a.libraryOverride {
		a.settingsSpaceID = defaultSpaceID
	}
	state := a.settingsState()
	state.OpenSpaces = openSpaces
	a.controls.present(controlSettings, ui.Rect{}, state)
}

func (a *app) refreshSettings() {
	if a.settingsSpaceID != "" {
		a.controls.update(controlSettings, a.settingsState())
	}
}

func (a *app) spaceFolder(id string) string {
	if id == defaultSpaceID {
		return a.defaultRoot
	}
	if !a.libraryOverride {
		if space := a.spaceByID(id); space != nil {
			return space.Root
		}
	}
	return ""
}

func (a *app) revealSpace(id string) {
	path := a.spaceFolder(id)
	if path == "" {
		return
	}
	go func() {
		info, err := os.Stat(path)
		a.window.Update(func() {
			if a.closed {
				return
			}
			switch {
			case err != nil:
				a.report("Could not show Space in Finder", err.Error(), "")
			case !info.IsDir():
				a.report("Could not show Space in Finder", "The Space path is no longer a folder.", "")
			default:
				mygo.Shell.ShowItemInFolder(path)
			}
		})
	}()
}

func (a *app) setAppearance(appearance mygo.ThemeSource) {
	if a.preferencesError != nil {
		return
	}
	switch appearance {
	case mygo.ThemeSystem, mygo.ThemeLight, mygo.ThemeDark:
		a.preferences.Appearance = appearance
		mygo.Theme.SetSource(appearance)
		a.saveAppearance()
	}
}

func (a *app) saveAppearance() {
	if !a.libraryOverride {
		savePreferences(a.preferences)
	}
	a.refreshSettings()
	a.window.Invalidate()
}

func (a *app) checkUpdates() {
	if a.checkingUpdates {
		return
	}
	a.checkingUpdates = true
	a.updateStatus, a.releaseURL = "Checking for updates…", ""
	a.refreshSettings()
	current := mygo.App.Version()
	go func() {
		status, release, err := latestRelease(current)
		a.window.Update(func() {
			if a.closed {
				return
			}
			a.checkingUpdates = false
			if err != nil {
				status = "Could not check for updates. " + err.Error()
			}
			a.updateStatus, a.releaseURL = status, release
			a.refreshSettings()
		})
	}()
}

func latestRelease(current string) (string, string, error) {
	const endpoint = "https://api.github.com/repos/reixie/keeps/releases/latest"
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Keeps")
	response, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return "No stable releases have been published yet.", "", nil
	}
	if response.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("GitHub returned HTTP %d.", response.StatusCode)
	}
	var release struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&release); err != nil {
		return "", "", err
	}
	installed, installedOK := releaseVersion(current)
	available, availableOK := releaseVersion(release.Tag)
	if !installedOK || !availableOK {
		return "", "", fmt.Errorf("The version numbers could not be compared.")
	}
	for i := range installed {
		if available[i] > installed[i] {
			return "Keeps " + release.Tag + " is available.", "https://github.com/reixie/keeps/releases/tag/" + url.PathEscape(release.Tag), nil
		}
		if available[i] < installed[i] {
			break
		}
	}
	return "Keeps is up to date.", "", nil
}

func releaseVersion(version string) ([3]uint64, bool) {
	var parts [3]uint64
	segments := strings.Split(strings.TrimPrefix(version, "v"), ".")
	if len(segments) != len(parts) {
		return parts, false
	}
	for i, segment := range segments {
		value, err := strconv.ParseUint(segment, 10, 64)
		if err != nil {
			return parts, false
		}
		parts[i] = value
	}
	return parts, true
}
