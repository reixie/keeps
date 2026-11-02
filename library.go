package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"
)

const manifestVersion = 4

func hostArch() string { return runtime.GOARCH }

type capture struct {
	Kind          string     `json:"kind"`
	URL           string     `json:"url,omitempty"`
	Title         string     `json:"title,omitempty"`
	Favicon       string     `json:"favicon,omitempty"`
	QuickLook     string     `json:"quicklook,omitempty"`
	ID            string     `json:"id"`
	Filename      string     `json:"filename"`
	Original      string     `json:"original"`
	Preview       string     `json:"preview"`
	MediaType     string     `json:"media_type"`
	Format        string     `json:"format"`
	Width         int        `json:"width"`
	Height        int        `json:"height"`
	ImportedAt    time.Time  `json:"imported_at"`
	SHA256        string     `json:"sha256"`
	Starred       bool       `json:"starred"`
	TrashedAt     *time.Time `json:"trashed_at,omitempty"`
	CollectionIDs []string   `json:"collection_ids,omitempty"`
}

type manifest struct {
	Version       int          `json:"version"`
	Items         []capture    `json:"items"`
	PendingPurges []capture    `json:"pending_purges,omitempty"`
	Collections   []collection `json:"collections"`
}

type libraryStore struct {
	root    string
	preview mediaAdapter
	// Metadata commits atomically replace the previous manifest.
	commit func(string, []byte) error
	now    func() time.Time
	remove func(*os.Root, string) error
	// Invoked by the mutation worker under the metadata lock after purge intent
	// is durable, before unlinking. It synchronously hands ownership to the UI.
	beforePurge         func(retained []capture)
	beforePurgeSnapshot func(manifest)
}

func newLibraryStore(root string) *libraryStore {
	return &libraryStore{root: root, preview: nativePreview, commit: atomicWrite, now: time.Now,
		remove: func(root *os.Root, path string) error { return root.Remove(path) }}
}

func (s *libraryStore) load() ([]capture, error) {
	m, err := s.loadSnapshot()
	return m.Items, err
}

func (s *libraryStore) loadSnapshot() (manifest, error) {
	// Startup participates in the writer lock: it cannot validate an old
	// snapshot while cleanup is removing files belonging to a newer one.
	if _, err := os.Stat(s.root); errors.Is(err, os.ErrNotExist) {
		return manifest{Version: manifestVersion}, nil
	}
	unlock, err := s.lockMetadata()
	if err != nil {
		return manifest{}, err
	}
	defer unlock()
	m, err := s.readManifest()
	return m, err
}

// Caller holds the metadata lock. Pending entries validate identities/paths,
// but intentionally tolerate missing originals after an interrupted purge.
func (s *libraryStore) readManifest() (manifest, error) {
	data, err := os.ReadFile(filepath.Join(s.root, "manifest.json"))
	if errors.Is(err, os.ErrNotExist) {
		// An absent manifest is only a new library if it is actually absent,
		// not a dangling manifest symlink or an unreadable parent.
		if _, statErr := os.Lstat(filepath.Join(s.root, "manifest.json")); statErr == nil || !errors.Is(statErr, os.ErrNotExist) {
			return manifest{}, fmt.Errorf("read library manifest: %w", err)
		}
		for _, folder := range []string{"originals", "previews"} {
			entries, readErr := os.ReadDir(filepath.Join(s.root, folder))
			if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
				return manifest{}, fmt.Errorf("read library: %w", readErr)
			}
			if len(entries) > 0 {
				return manifest{}, fmt.Errorf("library manifest is missing; existing media has been left untouched")
			}
		}
		return manifest{Version: manifestVersion}, nil
	}
	if err != nil {
		return manifest{}, fmt.Errorf("read library manifest: %w", err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return manifest{}, fmt.Errorf("read library manifest: %w", err)
	}
	if m.Version != 1 && m.Version != 2 && m.Version != 3 && m.Version != manifestVersion {
		return manifest{}, fmt.Errorf("unsupported library version %d", m.Version)
	}
	if m.Version == 1 && len(m.PendingPurges) > 0 {
		return manifest{}, fmt.Errorf("pending purges require library version 2")
	}
	// Older schemas did not have collections, even if unknown JSON fields
	// happen to use the same names. Migration is written only by a mutation.
	if m.Version < 3 {
		m.Collections = nil
		for i := range m.Items {
			m.Items[i].CollectionIDs = nil
		}
		for i := range m.PendingPurges {
			m.PendingPurges[i].CollectionIDs = nil
		}
	}
	if m.Version < 4 {
		for i := range m.Items {
			m.Items[i].Kind = captureMedia
			m.Items[i].URL = ""
			m.Items[i].Title = ""
			m.Items[i].Favicon = ""
			m.Items[i].QuickLook = ""
		}
		for i := range m.PendingPurges {
			m.PendingPurges[i].Kind = captureMedia
			m.PendingPurges[i].URL = ""
			m.PendingPurges[i].Title = ""
			m.PendingPurges[i].Favicon = ""
			m.PendingPurges[i].QuickLook = ""
		}
	}
	if err := validateCollectionMetadata(m); err != nil {
		return manifest{}, err
	}
	ids, hashes := map[string]bool{}, map[string]bool{}
	for i, item := range append(slices.Clone(m.Items), m.PendingPurges...) {
		pending := i >= len(m.Items)
		id, idErr := hex.DecodeString(item.ID)
		hash, hashErr := hex.DecodeString(item.SHA256)
		if idErr != nil || len(id) != 16 || hashErr != nil || len(hash) != 32 || ids[item.ID] || hashes[item.SHA256] ||
			item.Filename == "" || item.ImportedAt.IsZero() || !validTrashTime(item.TrashedAt) || pending && item.TrashedAt == nil || !validCaptureInfo(item) ||
			item.Original != "originals/"+item.ID+"."+item.Format || item.Preview != "previews/"+item.ID+".png" {
			return manifest{}, fmt.Errorf("invalid library entry %q", item.ID)
		}
		ids[item.ID], hashes[item.SHA256] = true, true
		if pending {
			continue
		}
		info, err := os.Lstat(filepath.Join(s.root, item.Original))
		if err != nil || !info.Mode().IsRegular() {
			return manifest{}, fmt.Errorf("stored original missing or unreadable: %s", item.Filename)
		}
	}
	sortCaptures(m.Items)
	return m, nil
}

func (s *libraryStore) writeManifest(m manifest) error {
	m.Version = manifestVersion
	if m.Collections == nil {
		m.Collections = []collection{}
	}
	if m.Items == nil {
		m.Items = []capture{}
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return s.commit(filepath.Join(s.root, "manifest.json"), append(data, '\n'))
}

func sortCaptures(items []capture) {
	slices.SortStableFunc(items, func(a, b capture) int {
		if n := b.ImportedAt.Compare(a.ImportedAt); n != 0 {
			return n
		}
		return strings.Compare(a.ID, b.ID)
	})
}

type importFailure struct{ Filename, Reason string }
type importResult struct {
	Fatal              error
	Items              []capture
	Added, Duplicates  int
	InTrash            int
	Failures           []importFailure
	Collections        []collection
	DestinationMissing bool
}

// importFiles serializes writers, reloads under the lock, and commits each
// successful item independently. Cancellation never calls this function.
func (s *libraryStore) importFiles(paths []string, progress func(int, int), destination ...string) (result importResult) {
	if len(paths) == 0 {
		return
	}
	failAll := func(err error) importResult {
		result.Fatal = err
		for _, path := range paths {
			result.Failures = append(result.Failures, importFailure{filepath.Base(path), err.Error()})
		}
		return result
	}
	if err := os.MkdirAll(s.root, 0755); err != nil {
		return failAll(err)
	}
	unlock, err := s.lockMetadata()
	if err != nil {
		return failAll(err)
	}
	defer unlock()
	m, err := s.readManifest()
	if err != nil {
		return failAll(err)
	}
	result.Items, result.Collections = m.Items, m.Collections
	destinationID := ""
	if len(destination) > 0 {
		destinationID = destination[0]
	}
	if destinationID != "" && !hasCollection(m.Collections, destinationID) {
		result.DestinationMissing = true
		destinationID = ""
	}
	for _, dir := range []string{"originals", "previews"} {
		if err := os.MkdirAll(filepath.Join(s.root, dir), 0755); err != nil {
			return failAll(err)
		}
	}
	for i, path := range paths {
		item, duplicate, err := s.importOne(path, &m, destinationID)
		switch {
		case err != nil:
			result.Failures = append(result.Failures, importFailure{filepath.Base(path), err.Error()})
		case duplicate:
			result.Duplicates++
			if item.TrashedAt != nil {
				result.InTrash++
			}
		default:
			result.Added++
		}
		if progress != nil {
			progress(i+1, len(paths))
		}
	}
	result.Items, result.Collections = m.Items, m.Collections
	sortCaptures(result.Items)
	return
}

// All metadata writers use the existing import lock and reload under it. This
// also serializes separate store instances/processes pointing at one library.
func (s *libraryStore) lockMetadata() (func(), error) {
	lock, err := os.OpenFile(filepath.Join(s.root, ".import.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		lock.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}, nil
}

// setStarred sets an explicit value for stable IDs in one atomic manifest
// replacement. A missing target fails the whole batch, never a partial update.
func (s *libraryStore) setStarred(ids []string, starred bool) ([]capture, error) {
	m, err := s.setStarredSnapshot(ids, starred)
	return m.Items, err
}

func (s *libraryStore) setStarredSnapshot(ids []string, starred bool) (manifest, error) {
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
	items := m.Items
	targets := make(map[string]bool, len(ids))
	for _, id := range ids {
		targets[id] = true
	}
	for i := range items {
		if targets[items[i].ID] {
			items[i].Starred = starred
			delete(targets, items[i].ID)
		}
	}
	if len(targets) != 0 {
		return manifest{}, fmt.Errorf("targeted capture no longer exists")
	}
	if err = s.writeManifest(m); err != nil {
		return manifest{}, err
	}
	return m, nil
}

func (s *libraryStore) importOne(path string, m *manifest, destination string) (item capture, duplicate bool, err error) {
	source, err := os.Open(path)
	if err != nil {
		return item, false, err
	}
	defer source.Close()
	stat, err := source.Stat()
	if err != nil {
		return item, false, err
	}
	if !stat.Mode().IsRegular() {
		return item, false, fmt.Errorf("not a regular media file")
	}
	stage, err := os.MkdirTemp(s.root, ".incoming-")
	if err != nil {
		return item, false, err
	}
	defer os.RemoveAll(stage)
	original, preview := filepath.Join(stage, "original"), filepath.Join(stage, "preview.png")
	hash := sha256.New()
	if err = copySynced(original, io.TeeReader(source, hash)); err != nil {
		return item, false, err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	for i, old := range m.Items {
		if !old.isLink() && old.SHA256 == digest {
			if destination != "" && old.TrashedAt == nil && !slices.Contains(old.CollectionIDs, destination) {
				updated := *m
				updated.Items = slices.Clone(m.Items)
				old.CollectionIDs = append(slices.Clone(old.CollectionIDs), destination)
				updated.Items[i] = old
				if err := s.writeManifest(updated); err != nil {
					return item, false, err
				}
				*m = updated
			}
			return old, true, nil
		}
	}
	for _, old := range m.PendingPurges {
		if !old.isLink() && old.SHA256 == digest {
			return item, false, fmt.Errorf("expired capture is awaiting Trash cleanup; try again after cleanup succeeds")
		}
	}
	info, err := s.preview(original, preview)
	if err != nil {
		return item, false, err
	}
	if !validMediaInfo(info) {
		return item, false, fmt.Errorf("invalid media metadata")
	}
	if err := syncFile(preview); err != nil {
		return item, false, err
	}
	idBytes := make([]byte, 16)
	if _, err = rand.Read(idBytes); err != nil {
		return item, false, err
	}
	id := hex.EncodeToString(idBytes)
	item = capture{ID: id, Kind: captureMedia, Filename: filepath.Base(path), Original: "originals/" + id + "." + info.Format,
		Preview: "previews/" + id + ".png", MediaType: info.Type, Format: info.Format, Width: info.Width, Height: info.Height,
		ImportedAt: s.now().UTC(), SHA256: digest}
	if destination != "" {
		item.CollectionIDs = []string{destination}
	}
	originalDest, previewDest := filepath.Join(s.root, item.Original), filepath.Join(s.root, item.Preview)
	committed, originalLinked, previewLinked := false, false, false
	defer func() {
		if !committed {
			if originalLinked {
				os.Remove(originalDest)
			}
			if previewLinked {
				os.Remove(previewDest)
			}
		}
	}()
	// Link fails if an internal name already exists; never clobber an original.
	if err = os.Link(original, originalDest); err != nil {
		return item, false, err
	}
	originalLinked = true
	if err = os.Link(preview, previewDest); err != nil {
		return item, false, err
	}
	previewLinked = true
	if err = syncDirectory(filepath.Dir(originalDest)); err != nil {
		return item, false, err
	}
	if err = syncDirectory(filepath.Dir(previewDest)); err != nil {
		return item, false, err
	}
	updated := *m
	updated.Items = append([]capture{item}, m.Items...)
	sortCaptures(updated.Items)
	if err = s.writeManifest(updated); err != nil {
		return item, false, err
	}
	committed = true
	*m = updated
	return item, false, nil
}

func copySynced(path string, r io.Reader) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := io.Copy(f, r)
	return errors.Join(writeErr, f.Sync(), f.Close())
}

func syncFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

func syncDirectory(path string) error { return syncFile(path) }

// All failure points before rename preserve the old manifest. Once rename
// succeeds the commit is published: a directory-sync error cannot roll it back
// and must never cause its originals to be removed.
func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".manifest-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	if err = errors.Join(writeErr, f.Sync(), f.Close()); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	// APFS journal + synced contents provide atomic replacement. Best-effort
	// parent fsync improves durability without misreporting a committed write.
	_ = syncDirectory(filepath.Dir(path))
	return nil
}

func validCaptureInfo(item capture) bool {
	if item.isLink() {
		return validLinkCapture(item)
	}
	return (item.Kind == captureMedia || item.Kind == "") && item.URL == "" && item.Title == "" && item.Favicon == "" && item.QuickLook == "" && validMediaInfo(mediaInfo{item.MediaType, item.Format, item.Width, item.Height})
}
