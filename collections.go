package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"slices"
	"strings"
)

type collection struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Icon string `json:"icon"`
}

const collectionPrefix = "collection:"

func collectionSection(id string) string { return collectionPrefix + id }

func (a *app) activeCollectionID() string {
	id, ok := strings.CutPrefix(a.navigation.selected, collectionPrefix)
	if ok {
		return id
	}
	return ""
}

func hasCollection(collections []collection, id string) bool {
	return slices.ContainsFunc(collections, func(c collection) bool { return c.ID == id })
}

func sortCollections(collections []collection) {
	slices.SortFunc(collections, func(a, b collection) int {
		if n := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); n != 0 {
			return n
		}
		return strings.Compare(a.ID, b.ID)
	})
}

func newCollectionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func validCollectionID(id string) bool {
	b, err := hex.DecodeString(id)
	return err == nil && len(b) == 16 && id == strings.ToLower(id)
}

func collectionNameError(name, exceptID string, collections []collection) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Enter a collection name."
	}
	for _, c := range collections {
		if c.ID != exceptID && strings.EqualFold(name, c.Name) {
			return "A collection with this name already exists."
		}
	}
	return ""
}

func validateCollectionMetadata(m manifest) error {
	ids := map[string]bool{}
	var previous []collection
	for _, c := range m.Collections {
		if !validCollectionID(c.ID) || ids[c.ID] || strings.TrimSpace(c.Name) != c.Name || collectionNameError(c.Name, "", previous) != "" {
			return fmt.Errorf("invalid collection %q", c.ID)
		}
		ids[c.ID] = true
		previous = append(previous, c)
	}
	for _, item := range append(slices.Clone(m.Items), m.PendingPurges...) {
		seen := map[string]bool{}
		for _, id := range item.CollectionIDs {
			if !ids[id] || seen[id] {
				return fmt.Errorf("invalid collection membership on capture %q", item.ID)
			}
			seen[id] = true
		}
	}
	return nil
}

// Changes contains only the explicitly chosen collection action. All other
// memberships, including partially assigned collections, remain untouched.
type collectionOperation struct {
	Create   []collection
	Edit     *collection
	DeleteID string
	IDs      []string
	Changes  map[string]bool
}

type collectionNameConflict struct{ ID, Message string }

func (e collectionNameConflict) Error() string { return e.Message }

func (s *libraryStore) updateCollections(op collectionOperation) (manifest, error) {
	// Only an actual submission may create an empty library on disk.
	if len(op.Create) > 0 {
		if err := os.MkdirAll(s.root, 0755); err != nil {
			return manifest{}, err
		}
	}
	unlock, err := s.lockMetadata()
	if err != nil {
		return manifest{}, err
	}
	defer unlock()
	m, err := s.readManifest()
	if err != nil {
		return manifest{}, err
	}
	for _, draft := range op.Create {
		draft.Name = strings.TrimSpace(draft.Name)
		if !validCollectionID(draft.ID) || hasCollection(m.Collections, draft.ID) {
			return manifest{}, fmt.Errorf("collection ID is no longer available")
		}
		if message := collectionNameError(draft.Name, "", m.Collections); message != "" {
			return manifest{}, collectionNameConflict{draft.ID, message}
		}
		m.Collections = append(m.Collections, draft)
	}
	if op.Edit != nil {
		if !hasCollection(m.Collections, op.Edit.ID) {
			return manifest{}, fmt.Errorf("This collection no longer exists.")
		}
		edited := *op.Edit
		edited.Name = strings.TrimSpace(edited.Name)
		if message := collectionNameError(edited.Name, edited.ID, m.Collections); message != "" {
			return manifest{}, collectionNameConflict{edited.ID, message}
		}
		for i := range m.Collections {
			if m.Collections[i].ID == edited.ID {
				m.Collections[i] = edited
			}
		}
	}
	if op.DeleteID != "" {
		if !hasCollection(m.Collections, op.DeleteID) {
			return manifest{}, fmt.Errorf("This collection no longer exists.")
		}
		m.Collections = slices.DeleteFunc(m.Collections, func(c collection) bool { return c.ID == op.DeleteID })
		for _, items := range [][]capture{m.Items, m.PendingPurges} {
			for i := range items {
				items[i].CollectionIDs = slices.DeleteFunc(items[i].CollectionIDs, func(id string) bool { return id == op.DeleteID })
			}
		}
	}
	if len(op.Changes) > 0 {
		if len(op.IDs) == 0 {
			return manifest{}, fmt.Errorf("No captures targeted.")
		}
		for id := range op.Changes {
			if !hasCollection(m.Collections, id) {
				return manifest{}, fmt.Errorf("The selected collection no longer exists. Reopen the capture’s context menu to refresh the list.")
			}
		}
		targets := map[string]bool{}
		for _, id := range op.IDs {
			targets[id] = true
		}
		for i := range m.Items {
			item := &m.Items[i]
			if !targets[item.ID] {
				continue
			}
			if item.TrashedAt != nil {
				return manifest{}, fmt.Errorf("A targeted capture is now in Trash. Restore it before assigning collections.")
			}
			for id, checked := range op.Changes {
				if checked && !slices.Contains(item.CollectionIDs, id) {
					item.CollectionIDs = append(item.CollectionIDs, id)
				}
				if !checked {
					item.CollectionIDs = slices.DeleteFunc(item.CollectionIDs, func(value string) bool { return value == id })
				}
			}
			slices.Sort(item.CollectionIDs)
			delete(targets, item.ID)
		}
		if len(targets) != 0 {
			return manifest{}, fmt.Errorf("A targeted capture no longer exists.")
		}
	}
	sortCollections(m.Collections)
	if err := s.writeManifest(m); err != nil {
		return manifest{}, err
	}
	return m, nil
}

func (a *app) publishSnapshot(m manifest) {
	// Called once on the UI owner, before the next mutation may start.
	a.collections = m.Collections
	sortCollections(a.collections)
	if id := a.activeCollectionID(); id != "" && !hasCollection(a.collections, id) {
		a.navigation.selected = sectionLibrary
		a.focus = focusLibrary
	}
	a.publishItems(m.Items)
}

func (a *app) enqueueCollections(op collectionOperation, done func(error)) {
	if a.store == nil || a.loadError != nil || a.closed {
		if done != nil {
			done(fmt.Errorf("Library is unavailable."))
		}
		return
	}
	a.enqueueImport(func() (importInput, error) { return importInput{collections: &op, completed: done}, nil })
}
