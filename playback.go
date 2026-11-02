package main

import (
	"encoding/json"
	"image"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"
)

// Four decoders, at most 512 pixels on the longest edge, sampled at 30 Hz.
// Waiting and failed cards retain their cached posters. Native preview has an
// independent lifecycle and never reuses an inline player's audio state.
const inlinePlayerLimit = 4
const inlinePixelLimit = 512

type previewRecord struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Title string `json:"title"`
}

// All methods run on Mygo's UI/main thread. frame copies into caller-owned
// memory synchronously; no native callback retains a Go pointer.
type playbackBackend interface {
	state() (eligible, preview bool)
	add(id, path string)
	remove(id string)
	frame(id string, pixels *image.RGBA) int
	preview(records []previewRecord, target string)
	current() string
	close()
}

type inlineFrame struct {
	pixels *image.RGBA
	bitmap *ui.Bitmap
	failed bool
}

type mediaController struct {
	backend     playbackBackend
	frames      map[string]*inlineFrame
	stillGIFs   map[string]bool // one-frame GIFs need only their existing poster
	desired     []capture
	previewOpen bool
	savedFocus  string
	savedScroll float32
	stop        func()
	cadence     atomic.Int64
}

// Unlike visibleCard, this excludes the virtualization buffer. Rank by visible
// fraction, then visible area, with stable sequence order as the final tie-break.
func playbackCandidates(items []capture, layout masonryLayout, offset, height float32, stillGIFs map[string]bool) []capture {
	type candidate struct {
		item           capture
		fraction, area float32
	}
	var ranked []candidate
	if height <= 0 {
		return nil
	}
	for i, item := range items {
		if (item.MediaType != "video" && item.Format != "gif") || stillGIFs[item.ID] || i >= len(layout.Cards) {
			continue
		}
		r := layout.Cards[i]
		visible := max(float32(0), min(r.Y+r.H, offset+height)-max(r.Y, offset))
		if visible > 0 && r.W > 0 && r.H > 0 {
			ranked = append(ranked, candidate{item, visible / r.H, visible * r.W})
		}
	}
	slices.SortStableFunc(ranked, func(a, b candidate) int {
		if a.fraction != b.fraction {
			if a.fraction > b.fraction {
				return -1
			}
			return 1
		}
		if a.area > b.area {
			return -1
		}
		if a.area < b.area {
			return 1
		}
		return 0
	})
	result := make([]capture, 0, min(inlinePlayerLimit, len(ranked)))
	for _, entry := range ranked[:min(inlinePlayerLimit, len(ranked))] {
		result = append(result, entry.item)
	}
	return result
}

func (a *app) previewRecords() []previewRecord {
	var records []previewRecord
	if a.store == nil {
		return records
	}
	for _, item := range a.visibleItems() {
		path, title := item.Original, item.Filename
		if item.isLink() {
			path, title = item.QuickLook, item.Title
		}
		records = append(records, previewRecord{item.ID, filepath.Join(a.store.root, path), title})
	}
	return records
}

func (a *app) previewTarget() (capture, bool) {
	items := a.visibleItems()
	if id, ok := strings.CutPrefix(a.focus, "capture:"); ok {
		for _, item := range items {
			if item.ID == id {
				return item, true
			}
		}
		return capture{}, false
	}
	if a.focus == focusLibrary {
		for _, item := range items {
			if a.selection[item.ID] {
				return item, true
			}
		}
	}
	return capture{}, false
}

func (a *app) showPreview(item capture) {
	if a.media == nil || a.closed || a.purging || a.linkDialog != nil || a.collectionDialog != nil {
		return
	}
	if !slices.ContainsFunc(a.visibleItems(), func(current capture) bool { return current.ID == item.ID }) {
		return
	}
	m := a.media
	if !m.previewOpen {
		m.savedFocus, m.savedScroll = a.focus, a.libraryScroll.Y
	}
	m.releaseInline()
	m.backend.preview(a.previewRecords(), item.ID)
	_, m.previewOpen = m.backend.state()
}

func (m *mediaController) releaseInline() {
	for id := range m.frames {
		m.backend.remove(id)
	}
	clear(m.frames)
}

func (a *app) restorePreviewState() {
	a.focus, a.libraryScroll.Y = a.media.savedFocus, a.media.savedScroll
	a.reconcileVisible(a.visibleItems())
}

// Native close can precede the next idle tick. Reconcile before handling the
// first grid key so a delayed restoration cannot overwrite that user's input.
func (a *app) previewOwnsFocus() bool {
	if a.media == nil {
		return false
	}
	if a.media.previewOpen {
		_, open := a.media.backend.state()
		if !open {
			a.restorePreviewState()
			a.media.previewOpen = false
		}
	}
	return a.media.previewOpen
}

func (a *app) tickPlayback() {
	if a.closed || a.media == nil {
		return
	}
	m := a.media
	eligible, preview := m.backend.state()
	if preview {
		m.backend.preview(a.previewRecords(), "")
		_, preview = m.backend.state()
	}
	if m.previewOpen && !preview {
		a.restorePreviewState()
	}
	m.previewOpen = preview
	desired := m.desired
	if !eligible || preview || a.reduceMotion || a.purging {
		desired = nil
	}
	m.cadence.Store(int64(200 * time.Millisecond))
	if len(desired) > 0 {
		m.cadence.Store(int64(time.Second / 30))
	}
	for id := range m.frames {
		if !slices.ContainsFunc(desired, func(item capture) bool { return item.ID == id }) {
			m.backend.remove(id)
			delete(m.frames, id)
		}
	}
	for _, item := range desired {
		entry := m.frames[item.ID]
		if entry == nil {
			scale := min(1, float64(inlinePixelLimit)/float64(max(item.Width, item.Height)))
			w, h := max(1, int(math.Round(float64(item.Width)*scale))), max(1, int(math.Round(float64(item.Height)*scale)))
			entry = &inlineFrame{pixels: image.NewRGBA(image.Rect(0, 0, w, h))}
			m.frames[item.ID] = entry
			m.backend.add(item.ID, filepath.Join(a.store.root, item.Original))
		}
		if entry.failed {
			continue
		}
		// Idempotent: a native inactivity notification can evict players between
		// two Go ticks, even when the app becomes active again immediately.
		m.backend.add(item.ID, filepath.Join(a.store.root, item.Original))
		switch m.backend.frame(item.ID, entry.pixels) {
		case -2: // static GIF: free this slot for an actual animation/video
			m.backend.remove(item.ID)
			delete(m.frames, item.ID)
			if m.stillGIFs == nil {
				m.stillGIFs = make(map[string]bool)
			}
			m.stillGIFs[item.ID] = true
		case -1:
			m.backend.remove(item.ID)
			entry.bitmap, entry.failed = nil, true
		case 1:
			if entry.bitmap == nil {
				entry.bitmap = ui.NewBitmap(entry.pixels)
			} else {
				entry.bitmap.Update(entry.pixels)
			}
		}
	}
}

func (a *app) startPlayback(backend playbackBackend) {
	a.media = &mediaController{backend: backend, frames: make(map[string]*inlineFrame)}
	a.media.cadence.Store(int64(200 * time.Millisecond))
	m := a.media
	stop := make(chan struct{})
	a.media.stop = func() { close(stop) }
	// Coalesce ticks if the UI is busy; never accumulate frame requests. Frame
	// extraction and Bitmap.Update happen in Window.Update, outside the view.
	var queued atomic.Bool
	go func() {
		ticker := time.NewTicker(time.Second / 30)
		defer ticker.Stop()
		var last time.Time
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if time.Since(last) < time.Duration(m.cadence.Load()) {
					continue
				}
				if !queued.CompareAndSwap(false, true) {
					continue
				}
				last = time.Now()
				a.update(func() { defer queued.Store(false); a.tickPlayback() })
			}
		}
	}()
}

// Called under the store lock after durable purge intent, before any unlink.
// The serial worker waits for the UI owner to release native references. Late
// thumbnail/player completions cannot republish removed IDs.
func (a *app) preparePurge(retained []capture) {
	a.preparePurgeSnapshot(manifest{Items: retained, Collections: a.collections})
}

func (a *app) preparePurgeSnapshot(snapshot manifest) {
	a.purging = true
	if a.media != nil {
		a.media.releaseInline()
		a.media.desired = nil
	}
	a.publishSnapshot(snapshot)
	if a.media != nil {
		a.media.backend.preview(a.previewRecords(), "")
	}
}

// Keep serialization in one place for the narrow native ABI.
func encodePreview(records []previewRecord) string {
	if records == nil {
		records = []previewRecord{}
	}
	data, _ := json.Marshal(records)
	return string(data)
}
