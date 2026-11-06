package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/egoist/mygo"
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

func (s *libraryStore) setTrashedSnapshot(ids []string, restore bool) (manifest, error) {
	if len(ids) == 0 {
		return manifest{}, fmt.Errorf("no captures targeted")
	}
	return s.editManifest(func(m *manifest) error {
		now := time.Now().UTC()
		return editCaptureTargets(m.Items, ids, func(item *capture) {
			if restore {
				item.TrashedAt = nil
			} else if item.TrashedAt == nil {
				item.TrashedAt = &now
			}
		})
	})
}

type purgeResult struct {
	Snapshot manifest
	Publish  bool // intent may commit even when subsequent deletion fails
	Err      error
}

// cleanupExpired is called only by the serial mutation worker. The same flock
// also protects against separate store instances and processes. No IDs or clock
// readings are captured at enqueue time: a committed Restore always wins.
func (s *libraryStore) cleanupExpired(beforeUnlink func(manifest) error) purgeResult {
	return s.purgeCaptures(nil, beforeUnlink)
}

func (s *libraryStore) deletePermanently(ids []string, beforeUnlink func(manifest) error) purgeResult {
	if len(ids) == 0 {
		return purgeResult{Err: fmt.Errorf("no captures targeted")}
	}
	return s.purgeCaptures(ids, beforeUnlink)
}

// Explicit targets bypass retention, but must ALL still be in Trash when the
// lock is acquired. Both deletion paths share durable intent and recovery.
func (s *libraryStore) purgeCaptures(ids []string, beforeUnlink func(manifest) error) (result purgeResult) {
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
	now := time.Now().UTC()
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
	result.Snapshot, result.Publish = m, true
	if len(m.PendingPurges) == 0 {
		return
	}
	// Unlike an ordinary metadata update, deletion must wait for the intent's
	// directory entry to be durable, including when resuming an earlier intent.
	if err := syncDirectory(s.root); err != nil {
		result.Err = err
		return
	}
	if err := beforeUnlink(m); err != nil {
		result.Err = err
		return
	}
	root, err := os.OpenRoot(s.root)
	if err != nil {
		result.Err = err
		return
	}
	defer root.Close()
	var pending []capture
	for _, item := range m.PendingPurges {
		// A failed unlink leaves the full intent so retries check all assets.
		var err error
		for _, asset := range item.assets() {
			err = errors.Join(err, removeManaged(root, asset.managed))
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
func removeManaged(root *os.Root, path string) error {
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
	if err = root.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
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
	a.enqueueLibraryJob(libraryJob{run: func(store *libraryStore) func() {
		snapshot, err := store.setTrashedSnapshot(op.IDs, op.Restore)
		return func() {
			success, failure, recovery := "Moved to Trash", "Could not move captures to Trash", "Open the capture’s context menu and try Move to Trash again."
			if op.Restore {
				success, failure, recovery = "Restored", "Could not restore captures", "Open the capture’s context menu in Trash and try Restore again."
			}
			if err != nil {
				a.report(failure, err.Error(), recovery)
				a.announce(failure)
				return
			}
			a.publishSnapshot(snapshot)
			a.announce(success)
		}
	}})
}

const deleteRecovery = "Check library permissions and try again from Trash."

func (a *app) confirmDeletePermanently(ids []string) {
	if a.closed || a.switchingSpace || a.store == nil || a.loadError != nil || a.nativeDialogs > 0 || a.collectionDialog != nil || a.linkDialog != nil || len(ids) == 0 {
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
	a.nativeMessage(mygo.MessageOptions{
		Type: mygo.MessageWarning, Title: "Delete Permanently", Message: message,
		Detail: "This can’t be undone.", Buttons: []string{"Cancel", "Delete Permanently"},
		DefaultButton: 0, CancelButton: 0,
	}, func(result mygo.MessageResult, err error) {
		a.focus = returnFocus
		a.reconcileVisible(a.visibleItems())
		if err != nil {
			a.report("Could not delete captures permanently", err.Error(), deleteRecovery)
			return
		}
		if result.Button == 1 {
			a.enqueuePermanentDeletion(ids)
		}
	})
}

func (a *app) enqueuePermanentDeletion(ids []string) {
	a.enqueueLibraryJob(libraryJob{run: func(store *libraryStore) func() {
		result := store.deletePermanently(ids, a.beforePurgeUnlink)
		return func() {
			a.finishPurge(result)
			if result.Err == nil {
				a.cleanupError = ""
				a.announce("Deleted permanently")
				return
			}
			title, recovery := "Could not delete captures permanently", deleteRecovery
			if result.Publish {
				title, recovery = "Deletion could not finish", "Check library permissions. Keeps will retry automatically."
			}
			a.report(title, result.Err.Error(), recovery)
			a.announce(title)
		}
	}})
}

func (a *app) requestCleanup() {
	if a.closed || a.switchingSpace || a.loading || a.store == nil || a.loadError != nil || a.cleanupQueued {
		return
	}
	a.cleanupQueued = true
	a.enqueueLibraryJob(libraryJob{run: func(store *libraryStore) func() {
		result := store.cleanupExpired(a.beforePurgeUnlink)
		return func() {
			a.cleanupQueued = false
			a.finishPurge(result)
			if result.Err == nil {
				a.cleanupError = ""
				return
			}
			if detail := result.Err.Error(); detail != a.cleanupError {
				a.announce("Trash cleanup could not finish; Keeps will retry.")
				a.report("Trash cleanup could not finish", "Keeps will retry. "+detail, "")
				log.Printf("Trash cleanup: %s", detail)
				a.cleanupError = detail
			}
		}
	}})
}

func (a *app) finishPurge(result purgeResult) {
	a.purging = false
	if result.Publish {
		a.publishSnapshot(result.Snapshot)
	}
}

func (a *app) beforePurgeUnlink(snapshot manifest) error {
	// RunOnMain can return without invoking the callback after Mygo quits.
	// Leave the durable purge intent in place if the handoff did not run.
	acknowledged := false
	mygo.RunOnMain(func() {
		if a.closed {
			return
		}
		a.preparePurgeSnapshot(snapshot)
		a.window.Invalidate()
		acknowledged = true
	})
	if !acknowledged {
		return fmt.Errorf("Keeps closed before releasing captures for Trash cleanup")
	}
	return nil
}
