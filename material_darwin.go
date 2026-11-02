package main

import (
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

// Mygo v0.6.4 creates its NSVisualEffectView with StateActive. Let AppKit
// follow window activation instead. Called on the main thread after creation;
// Mygo still owns the effect, its material, placement, and lifetime.
func followWindowActivity(win *mygo.Window) {
	content := objc.ID(win.NativeHandle()).Send(objc.RegisterName("contentView"))
	views := content.Send(objc.RegisterName("subviews"))
	for i, count := uintptr(0), uintptr(views.Send(objc.RegisterName("count"))); i < count; i++ {
		view := views.Send(objc.RegisterName("objectAtIndex:"), i)
		if view.Send(objc.RegisterName("isKindOfClass:"), objc.GetClass("NSVisualEffectView")) != 0 {
			view.Send(objc.RegisterName("setState:"), 0) // NSVisualEffectStateFollowsWindowActiveState
		}
	}
}
