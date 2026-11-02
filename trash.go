package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const trashRetention = 30 * 24 * time.Hour

func validTrashTime(t *time.Time) bool {
	if t == nil {
		return true
	}
	_, offset := t.Zone()
	return !t.IsZero() && offset == 0
}

type trashOperation struct {
	IDs     []string
	Restore bool
}

func (op trashOperation) label() string {
	if op.Restore {
		return "Restore"
	}
	return "Move to Trash"
}

// One atomic metadata update, retaining all media and original ordering.
func (s *libraryStore) setTrashed(ids []string, restore bool) ([]capture, error) {
	m, err := s.setTrashedSnapshot(ids, restore)
	return m.Items, err
}

func (s *libraryStore) setTrashedSnapshot(ids []string, restore bool) (manifest, error) {
	if len(ids) == 0 {
		return manifest{}, fmt.Errorf("no captures targeted")
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
	targets := make(map[string]bool, len(ids))
	for _, id := range ids {
		targets[id] = true
	}
	now := s.now().UTC()
	for i := range m.Items {
		item := &m.Items[i]
		if !targets[item.ID] {
			continue
		}
		if restore {
			item.TrashedAt = nil
		} else if item.TrashedAt == nil {
			item.TrashedAt = &now
		}
		delete(targets, item.ID)
	}
	if len(targets) != 0 {
		return manifest{}, fmt.Errorf("targeted capture no longer exists")
	}
	if err := s.writeManifest(m); err != nil {
		return manifest{}, err
	}
	return m, nil
}

type purgeResult struct {
	Items       []capture
	Collections []collection
	Publish     bool // intent may commit even when subsequent deletion fails
	Err         error
}

// cleanupExpired is called only by the serial mutation worker. The same flock
// also protects against separate store instances and processes. No IDs or clock
// readings are captured at enqueue time: a committed Restore always wins.
func (s *libraryStore) cleanupExpired() purgeResult {
	return s.purgeCaptures(nil)
}

func (s *libraryStore) deletePermanently(ids []string) purgeResult {
	if len(ids) == 0 {
		return purgeResult{Err: fmt.Errorf("no captures targeted")}
	}
	return s.purgeCaptures(ids)
}

// Explicit targets bypass retention, but must ALL still be in Trash when the
// lock is acquired. Both deletion paths share durable intent and recovery.
func (s *libraryStore) purgeCaptures(ids []string) (result purgeResult) {
	if _, err := os.Stat(s.root); errors.Is(err, os.ErrNotExist) {
		if len(ids) > 0 {
			result.Err = err
		}
		return result
	}
	unlock, err := s.lockMetadata()
	if err != nil {
		result.Err = err
		return
	}
	defer unlock()
	m, err := s.readManifest()
	if err != nil {
		result.Err = err
		return
	}
	targets := make(map[string]bool, len(ids))
	for _, id := range ids {
		targets[id] = true
	}
	if len(targets) > 0 {
		found := 0
		for _, item := range m.Items {
			if targets[item.ID] {
				if item.TrashedAt == nil {
					result.Err = fmt.Errorf("capture is no longer in Trash: %s", item.Filename)
					return
				}
				found++
			}
		}
		if found != len(targets) {
			result.Err = fmt.Errorf("targeted capture no longer exists")
			return
		}
	}
	now := s.now().UTC()
	var retained []capture
	marked := false
	for _, item := range m.Items {
		if targets[item.ID] || len(targets) == 0 && item.TrashedAt != nil && !now.Before(item.TrashedAt.Add(trashRetention)) {
			m.PendingPurges = append(m.PendingPurges, item)
			marked = true
		} else {
			retained = append(retained, item)
		}
	}
	if marked {
		m.Items = retained
		if err := s.writeManifest(m); err != nil {
			result.Err = err
			return
		}
	}
	result.Items, result.Collections, result.Publish = m.Items, m.Collections, true
	if len(m.PendingPurges) == 0 {
		return
	}
	// Unlike an ordinary metadata update, deletion must wait for the intent's
	// directory entry to be durable, including when resuming an earlier intent.
	if err := syncDirectory(s.root); err != nil {
		result.Err = err
		return
	}
	if s.beforePurgeSnapshot != nil {
		s.beforePurgeSnapshot(m)
	} else if s.beforePurge != nil {
		s.beforePurge(m.Items)
	}
	root, err := os.OpenRoot(s.root)
	if err != nil {
		result.Err = err
		return
	}
	defer root.Close()
	var pending []capture
	for _, item := range m.PendingPurges {
		// Both attempts are idempotent. Failure of either retains the full intent.
		var err error
		for _, asset := range item.ownedAssets() {
			err = errors.Join(err, s.removeManaged(root, asset))
		}
		if err != nil {
			pending = append(pending, item)
			result.Err = errors.Join(result.Err, fmt.Errorf("purge %s: %w", item.ID, err))
		}
	}
	if len(pending) != len(m.PendingPurges) {
		m.PendingPurges = pending
		result.Err = errors.Join(result.Err, s.writeManifest(m))
	}
	return
}

// Exact relative names were validated against the stable ID and media format
// before intent. Root confines even a concurrent symlink replacement to this
// library. Refuse redirected managed directories and non-files; never recurse.
func (s *libraryStore) removeManaged(root *os.Root, path string) error {
	dir := filepath.Dir(path)
	info, err := root.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("managed directory is not a directory: %s", dir)
	}
	info, err = root.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return syncRootDirectory(root, dir)
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("managed file is not regular: %s", path)
	}
	if err = s.remove(root, path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Do not discard intent until each unlink is durable.
	return syncRootDirectory(root, dir)
}

func syncRootDirectory(root *os.Root, dir string) error {
	f, err := root.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

func (a *app) enqueueTrash(op trashOperation) {
	if a.store == nil || a.loadError != nil || len(op.IDs) == 0 {
		return
	}
	op.IDs = slices.Clone(op.IDs)
	a.enqueueImport(func() (importInput, error) { return importInput{trash: &op}, nil })
}

func (a *app) confirmDeletePermanently(ids []string) {
	if a.closed || a.store == nil || a.loadError != nil || a.nativeDialogs > 0 || a.collectionDialog != nil || a.linkDialog != nil || a.confirmPermanentDeletion == nil || len(ids) == 0 {
		return
	}
	ids = slices.Clone(ids)
	message := fmt.Sprintf("Permanently delete %d captures?", len(ids))
	if len(ids) == 1 {
		for _, item := range a.items {
			if item.ID == ids[0] {
				name := item.Filename
				if item.isLink() {
					name = item.Title
				}
				message = fmt.Sprintf("Permanently delete “%s”?", name)
				break
			}
		}
	}
	returnFocus := a.focus
	a.nativeDialogs++
	go func() {
		confirmed, err := a.confirmPermanentDeletion(message)
		a.update(func() {
			a.nativeDialogs--
			if a.closed {
				return
			}
			a.focus = returnFocus
			a.reconcileVisible(a.visibleItems())
			if err != nil {
				a.report("Could not delete captures permanently", err.Error())
				return
			}
			if confirmed {
				a.enqueueImport(func() (importInput, error) { return importInput{deleteIDs: ids}, nil })
			}
		})
	}()
}

func (a *app) requestCleanup() {
	if a.closed || a.loading || a.store == nil || a.loadError != nil || a.cleanupQueued {
		return
	}
	a.cleanupQueued = true
	a.enqueueImport(func() (importInput, error) { return importInput{purge: true}, nil })
}

func (a *app) publishItems(items []capture) {
	previous := a.visibleItems()
	a.items = items
	current := a.visibleItems()
	visible := make(map[string]bool, len(current))
	for _, item := range current {
		visible[item.ID] = true
	}
	if a.media != nil {
		for id := range a.media.stillGIFs {
			if !visible[id] {
				delete(a.media.stillGIFs, id)
			}
		}
	}
	if slices.ContainsFunc(previous, func(item capture) bool { return !visible[item.ID] }) {
		// Discard clipboard preparation for a removed visible capture. An
		// hourly check that removed nothing must not cancel a pending Copy.
		a.copyGeneration++
	}
	if a.thumbnails != nil {
		a.thumbnails.retain(items)
	}
	a.reconcileVisible(current)
}
