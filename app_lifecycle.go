package main

import (
	"slices"

	"github.com/egoist/mygo"
)

const libraryLoadRecovery = "Quit and reopen Keeps to try again."

func (a *app) startLibrary() {
	a.loading = true
	go func() {
		root, err := defaultLibraryRoot()
		var snapshot manifest
		store := &libraryStore{root: root}
		if err == nil {
			snapshot, err = store.loadSnapshot()
		}
		a.window.Update(func() {
			if a.closed {
				return
			}
			a.loading = false
			a.loadError = err
			if err != nil {
				const title = "Library could not be loaded"
				a.announce(title)
				a.report(title, err.Error(), libraryLoadRecovery)
				a.discardLibraryJobs()
				return
			}
			a.store = store
			a.publishSnapshot(snapshot)
			a.thumbnails = newThumbnailCache(store, a.window.Update)
			a.startRetention()
			a.startNextLibraryJob()
			a.startNextPaste()
		})
	}()
}

func (a *app) shutdown() {
	if a.closed {
		return
	}
	a.closed = true
	a.captureMenu.dispose()
	a.captureMenu = nil
	if a.pasteCancel != nil {
		a.pasteCancel()
	}
	a.pasteQueue = nil
	if d := a.linkDialog; d != nil && d.cancel != nil {
		d.cancel()
	}
	a.media.close()
	if a.stopRetention != nil {
		a.stopRetention()
	}
	a.thumbnails.close()
	a.discardLibraryJobs()
}

func (a *app) publishSnapshot(snapshot manifest) {
	// Sorting the UI copy must not mutate the store's in-flight purge snapshot.
	a.collections = slices.Clone(snapshot.Collections)
	sortCollections(a.collections)
	if id := a.activeCollectionID(); id != "" && !hasCollection(a.collections, id) {
		a.navigation.selected = sectionLibrary
		a.focus = focusLibrary
	}
	previous := a.visibleItems()
	a.items = snapshot.Items
	current := a.visibleItems()
	visible := make(map[string]bool, len(current))
	for _, item := range current {
		visible[item.ID] = true
	}
	a.media.retainGIFs(visible)
	if slices.ContainsFunc(previous, func(item capture) bool { return !visible[item.ID] }) {
		// An hourly cleanup that removed nothing must not cancel a pending Copy.
		a.copyGeneration++
	}
	if a.thumbnails != nil {
		a.thumbnails.retain(snapshot.Items)
	}
	a.reconcileVisible(current)
}

func (a *app) nativeMessage(options mygo.MessageOptions, complete func(mygo.MessageResult, error)) {
	options.Parent = a.window
	a.nativeDialogs++
	go func() {
		result, err := mygo.Dialog.Message(options)
		a.window.Update(func() {
			a.nativeDialogs--
			if complete != nil && !a.closed {
				complete(result, err)
			}
		})
	}()
}

func (a *app) report(title, detail, recovery string) {
	if a.window == nil {
		return
	}
	if recovery != "" {
		detail = recovery + "\n\n" + detail
	}
	a.nativeMessage(mygo.MessageOptions{Type: mygo.MessageWarning, Title: title, Message: title, Detail: detail}, nil)
}

func (a *app) announce(text string) {
	if text != "" {
		a.announcements = append(a.announcements, text)
	}
}
