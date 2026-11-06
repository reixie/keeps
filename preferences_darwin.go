package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

const preferencesKey = "KeepsPreferences"

type appPreferences struct {
	Appearance  mygo.ThemeSource `json:"appearance"`
	Translucent bool             `json:"translucent"`
	Spaces      []librarySpace   `json:"spaces"`
	ActiveSpace string           `json:"active_space"`
	Sort        string           `json:"sort,omitempty"`
	Density     string           `json:"density,omitempty"`
}

func defaultPreferences() appPreferences {
	return appPreferences{Appearance: mygo.ThemeSystem, Translucent: true, ActiveSpace: defaultSpaceID,
		Spaces: []librarySpace{{ID: defaultSpaceID, Name: "Default", Icon: "folder"}}}
}

func (p appPreferences) activeSpace() librarySpace {
	for _, space := range p.Spaces {
		if space.ID == p.ActiveSpace {
			return space
		}
	}
	return p.Spaces[0]
}

func loadPreferences() (appPreferences, error) {
	defaults := defaultPreferences()
	store := objc.ID(objc.GetClass("NSUserDefaults")).Send(objc.RegisterName("standardUserDefaults"))
	raw := nativeString(store.Send(objc.RegisterName("stringForKey:"), nativeNSString(preferencesKey)))
	if raw == "" {
		return defaults, nil
	}
	p := defaults
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return defaults, fmt.Errorf("read Keeps preferences: %w", err)
	}
	switch p.Appearance {
	case mygo.ThemeSystem, mygo.ThemeLight, mygo.ThemeDark:
	default:
		return defaults, fmt.Errorf("Invalid appearance preference.")
	}
	if len(p.Spaces) == 0 {
		return defaults, fmt.Errorf("The default Space is missing from Keeps preferences.")
	}
	base := p.Spaces[0]
	base.Icon, base.Emoji = defaults.Spaces[0].Icon, ""
	if base != defaults.Spaces[0] {
		return defaults, fmt.Errorf("The default Space is missing from Keeps preferences.")
	}
	for i := range p.Spaces {
		if !validSpaceSymbol(p.Spaces[i].Icon, p.Spaces[i].Emoji) {
			p.Spaces[i].Icon, p.Spaces[i].Emoji = "folder", ""
		}
	}
	seen := map[string]bool{defaultSpaceID: true}
	for _, space := range p.Spaces[1:] {
		if !validCollectionID(space.ID) || seen[space.ID] || strings.TrimSpace(space.Name) == "" || !filepath.IsAbs(space.Root) {
			return defaults, fmt.Errorf("Invalid Space in Keeps preferences.")
		}
		seen[space.ID] = true
	}
	if !slices.ContainsFunc(p.Spaces, func(space librarySpace) bool { return space.ID == p.ActiveSpace && !space.Hidden }) {
		return defaults, fmt.Errorf("The active Space is missing from Keeps preferences.")
	}
	return p, nil
}

func savePreferences(p appPreferences) {
	data, _ := json.Marshal(p)
	store := objc.ID(objc.GetClass("NSUserDefaults")).Send(objc.RegisterName("standardUserDefaults"))
	store.Send(objc.RegisterName("setObject:forKey:"), nativeNSString(string(data)), nativeNSString(preferencesKey))
}
