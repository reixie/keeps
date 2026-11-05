package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/egoist/mygo/ui"
)

const thumbnailBudget = 64 << 20

type thumbnailEntry struct {
	bitmap  *ui.Bitmap
	favicon *ui.Bitmap
	err     error
	used    uint64
	bytes   int
}
type thumbnailJob struct {
	item  capture
	entry *thumbnailEntry
}

// UI owns entries and the LRU; only bounded worker jobs perform disk IO,
// decoding and regeneration. Results cross back through Window.Update.
type thumbnailCache struct {
	entries map[string]*thumbnailEntry
	jobs    chan thumbnailJob
	stop    chan struct{}
	clock   uint64
	bytes   int
}

func newThumbnailCache(store *libraryStore, update func(func())) *thumbnailCache {
	c := &thumbnailCache{entries: make(map[string]*thumbnailEntry), jobs: make(chan thumbnailJob, 8), stop: make(chan struct{})}
	for range 2 {
		go func() {
			for {
				select {
				case <-c.stop:
					return
				case job := <-c.jobs:
					bitmap, err := loadThumbnail(store, job.item)
					var favicon *ui.Bitmap
					if job.item.isLink() {
						favicon, _ = loadLocalBitmap(filepath.Join(store.root, job.item.Favicon))
					}
					update(func() {
						if c.entries[job.item.ID] != job.entry {
							return
						}
						entry := job.entry
						entry.bitmap, entry.err, entry.favicon = bitmap, err, favicon
						if bitmap != nil {
							w, h := bitmap.Size()
							entry.bytes = w * h * 6
						}
						if favicon != nil {
							w, h := favicon.Size()
							entry.bytes += w * h * 6
						}
						c.bytes += entry.bytes
						c.trim()
					})
				}
			}
		}()
	}
	return c
}

func (c *thumbnailCache) close() {
	if c != nil {
		close(c.stop)
	}
}

func (c *thumbnailCache) get(item capture) *thumbnailEntry {
	c.clock++
	if e := c.entries[item.ID]; e != nil {
		e.used = c.clock
		return e
	}
	e := &thumbnailEntry{used: c.clock}
	select {
	case c.jobs <- thumbnailJob{item, e}:
		c.entries[item.ID] = e
	default:
		return nil // next completion invalidates and gives this card another turn
	}
	c.trim()
	return e
}

func (c *thumbnailCache) trim() {
	for c.bytes > thumbnailBudget || len(c.entries) > 48 {
		var oldest string
		var used = ^uint64(0)
		for id, e := range c.entries {
			if (e.bitmap != nil || e.err != nil) && e.used < used {
				oldest, used = id, e.used
			}
		}
		if oldest == "" {
			return
		}
		c.bytes -= c.entries[oldest].bytes
		delete(c.entries, oldest)
	}
}

func (c *thumbnailCache) retain(items []capture) {
	ids := make(map[string]bool, len(items))
	for _, item := range items {
		ids[item.ID] = true
	}
	for id, entry := range c.entries {
		if !ids[id] {
			c.bytes -= entry.bytes
			delete(c.entries, id)
		}
	}
}

// Regeneration checks membership both before decoding the original and before
// publishing its preview. Expensive media work stays outside the metadata lock;
// the final check+rename shares the lock with purge. A queued or running job can
// never recreate a purged capture, including across store instances/processes.
func (s *libraryStore) lockRetainedThumbnail(item capture) (func(), error) {
	unlock, err := s.lockMetadata()
	if err != nil {
		return nil, err
	}
	m, err := s.readManifest()
	if err == nil && !slices.ContainsFunc(m.Items, func(current capture) bool {
		return current.ID == item.ID && current.Original == item.Original && current.Preview == item.Preview
	}) {
		err = fmt.Errorf("capture is no longer retained")
	}
	if err != nil {
		unlock()
		return nil, err
	}
	return unlock, nil
}

func loadThumbnail(store *libraryStore, item capture) (*ui.Bitmap, error) {
	path := filepath.Join(store.root, item.Preview)
	decode := func() (*ui.Bitmap, error) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		// Only bounded previews are decoded, never the original. A damaged or
		// externally replaced preview is regenerated instead of allocating it.
		info, err := f.Stat()
		if err != nil {
			return nil, err
		}
		if info.Size() > 8<<20 {
			return nil, fmt.Errorf("preview exceeds cache limit")
		}
		data, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
		if err != nil {
			return nil, err
		}
		if len(data) > 8<<20 {
			return nil, fmt.Errorf("preview exceeds cache limit")
		}
		return decodePreview(data)
	}
	if bitmap, err := decode(); err == nil {
		return bitmap, nil
	} else if item.isLink() {
		// Link assets are saved as a unit. Never decode a .webloc as media or
		// recreate assets for a link after a durable purge intent.
		return nil, err
	}
	unlock, err := store.lockRetainedThumbnail(item)
	if err != nil {
		return nil, err
	}
	unlock()
	f, err := os.CreateTemp(filepath.Dir(path), ".preview-*.png")
	if err != nil {
		return nil, err
	}
	stage := f.Name()
	f.Close()
	defer os.Remove(stage)
	if _, err := nativePreview(filepath.Join(store.root, item.Original), stage); err != nil {
		return nil, err
	}
	unlock, err = store.lockRetainedThumbnail(item)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := os.Rename(stage, path); err != nil {
		return nil, err
	}
	return decode()
}

func loadLocalBitmap(path string) (*ui.Bitmap, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 8<<20 {
		return nil, fmt.Errorf("preview exceeds cache limit")
	}
	return decodePreview(data)
}
