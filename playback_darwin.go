package main

import (
	"fmt"
	"image"
	"path/filepath"
	"unsafe"

	"github.com/ebitengine/purego"
)

// The app owns this handle until shutdown on Mygo's main thread. The dylib is
// loaded once for the process; Swift runtime code must not be dlclosed while
// AVFoundation cancellation completions can still be queued.
type macPlayback struct {
	handle         uintptr
	destroy        func(uintptr)
	getState       func(uintptr) int32
	addPlayer      func(uintptr, string, string)
	removePlayer   func(uintptr, string)
	copyFrame      func(uintptr, string, unsafe.Pointer, int32, int32) int32
	updatePreview  func(uintptr, string, string)
	currentPreview func(uintptr, *byte, int32) int32
}

func newMacPlayback(window uintptr) (*macPlayback, error) {
	path := filepath.Join(filepath.Dir(nativeHelper()), "libKeepsPlayback.dylib")
	lib, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, fmt.Errorf("load native playback: %w", err)
	}
	var create func(uintptr) uintptr
	m := &macPlayback{}
	for name, fn := range map[string]any{
		"KeepsPlaybackCreate": &create, "KeepsPlaybackDestroy": &m.destroy,
		"KeepsPlaybackState": &m.getState, "KeepsPlaybackAdd": &m.addPlayer,
		"KeepsPlaybackRemove": &m.removePlayer, "KeepsPlaybackFrame": &m.copyFrame,
		"KeepsPreviewUpdate": &m.updatePreview, "KeepsPreviewCurrent": &m.currentPreview,
	} {
		address, err := purego.Dlsym(lib, name)
		if err != nil {
			return nil, fmt.Errorf("native playback ABI %s: %w", name, err)
		}
		purego.RegisterFunc(fn, address)
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
func (m *macPlayback) frame(id string, pixels *image.RGBA) int {
	return int(m.copyFrame(m.handle, id, unsafe.Pointer(&pixels.Pix[0]), int32(pixels.Rect.Dx()), int32(pixels.Rect.Dy())))
}
func (m *macPlayback) preview(records []previewRecord, target string) {
	m.updatePreview(m.handle, encodePreview(records), target)
}
func (m *macPlayback) current() string {
	var data [256]byte
	n := m.currentPreview(m.handle, &data[0], int32(len(data)))
	return string(data[:n])
}
func (m *macPlayback) close() { m.destroy(m.handle); m.handle = 0 }
