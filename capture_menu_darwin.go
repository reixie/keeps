package main

import (
	"github.com/ebitengine/purego"
	"github.com/egoist/mygo/ui"
)

// Matches CaptureMenuResult in the Swift adapter.
const (
	captureMenuLink  int32 = 1
	captureMenuMedia int32 = 2
)

type macCaptureMenu struct {
	handle  uintptr
	present func(uintptr, float64, float64, float64, float64)
	close   func(uintptr)
	destroy func(uintptr)
}

func newMacCaptureMenu(window uintptr, closed func(int32)) (*macCaptureMenu, error) {
	m := &macCaptureMenu{}
	var create func(uintptr, uintptr) uintptr
	if err := bindKeepsNative(map[string]any{
		"KeepsCaptureMenuCreate":  &create,
		"KeepsCaptureMenuShow":    &m.present,
		"KeepsCaptureMenuClose":   &m.close,
		"KeepsCaptureMenuDestroy": &m.destroy,
	}); err != nil {
		return nil, err
	}
	// purego callbacks live for the process. Keeps creates one main window.
	m.handle = create(window, purego.NewCallback(closed))
	return m, nil
}

func (m *macCaptureMenu) show(bounds ui.Rect) {
	m.present(m.handle, float64(bounds.X), float64(bounds.Y), float64(bounds.W), float64(bounds.H))
}

func (m *macCaptureMenu) dismiss() {
	if m != nil {
		m.close(m.handle)
	}
}

func (m *macCaptureMenu) dispose() {
	if m != nil {
		m.destroy(m.handle)
	}
}
