package main

import (
	"encoding/json"
	"image"
	"unsafe"
)

// The app owns this handle until shutdown on Mygo's main thread.
type macPlayback struct {
	handle        uintptr
	destroy       func(uintptr)
	getState      func(uintptr) int32
	addPlayer     func(uintptr, string, string)
	removePlayer  func(uintptr, string)
	copyFrame     func(uintptr, string, unsafe.Pointer, int32, int32) int32
	updatePreview func(uintptr, string, string)
}

func newMacPlayback(window uintptr) (*macPlayback, error) {
	var create func(uintptr) uintptr
	m := &macPlayback{}
	if err := bindKeepsNative(map[string]any{
		"KeepsPlaybackCreate": &create, "KeepsPlaybackDestroy": &m.destroy,
		"KeepsPlaybackState": &m.getState, "KeepsPlaybackAdd": &m.addPlayer,
		"KeepsPlaybackRemove": &m.removePlayer, "KeepsPlaybackFrame": &m.copyFrame,
		"KeepsPreviewUpdate": &m.updatePreview,
	}); err != nil {
		return nil, err
	}
	m.handle = create(window)
	return m, nil
}

func (m *macPlayback) state() (bool, bool) {
	bits := m.getState(m.handle)
	return bits&1 != 0, bits&2 != 0
}
func (m *macPlayback) add(id, path string) { m.addPlayer(m.handle, id, path) }
func (m *macPlayback) remove(id string)    { m.removePlayer(m.handle, id) }
func (m *macPlayback) frame(id string, pixels *image.RGBA) inlineFrameStatus {
	return inlineFrameStatus(m.copyFrame(m.handle, id, unsafe.Pointer(&pixels.Pix[0]), int32(pixels.Rect.Dx()), int32(pixels.Rect.Dy())))
}
func (m *macPlayback) preview(records []previewRecord, target string) {
	if records == nil {
		records = []previewRecord{}
	}
	// The wire type contains only strings and marshals without error.
	data, _ := json.Marshal(records)
	m.updatePreview(m.handle, string(data), target)
}
func (m *macPlayback) close() { m.destroy(m.handle); m.handle = 0 }
