package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo"
)

// run performs the mutation off the main thread and returns its UI completion.
// Staged inputs belong to the queue until run starts, then to the worker.
type libraryJob struct {
	run     func(*libraryStore) func()
	release func()
}

func (j libraryJob) discard() {
	if j.release != nil {
		j.release()
	}
}

func (a *app) enqueueLibraryJob(job libraryJob) {
	if a.closed || a.loadError != nil {
		job.discard()
		if !a.closed {
			a.report("Library could not be loaded", a.loadError.Error(), libraryLoadRecovery)
		}
		return
	}
	a.libraryJobs = append(a.libraryJobs, job)
	a.startNextLibraryJob()
}

func (a *app) startNextLibraryJob() {
	if a.closed || a.libraryJobRunning || a.loading || a.store == nil || len(a.libraryJobs) == 0 {
		return
	}
	job := a.libraryJobs[0]
	a.libraryJobs[0] = libraryJob{}
	a.libraryJobs = a.libraryJobs[1:]
	a.libraryJobRunning = true
	store := a.store
	go func() {
		finish := job.run(store)
		job.discard()
		a.window.Update(func() {
			// A later mutation must not publish ahead of this snapshot.
			if !a.closed {
				finish()
			}
			a.libraryJobRunning = false
			a.startNextLibraryJob()
		})
	}()
}

func (a *app) discardLibraryJobs() {
	for _, job := range a.libraryJobs {
		job.discard()
	}
	a.libraryJobs = nil
}

// The producer owns the input until this main-thread handoff. Mygo drains
// accepted calls during shutdown and drops calls posted after the loop stops.
func (a *app) submitPrepared(ctx context.Context, job libraryJob, accept func(libraryJob)) bool {
	handled, accepted := false, false
	mygo.RunOnMain(func() {
		handled = true
		if a.closed || ctx.Err() != nil {
			job.discard()
			return
		}
		accept(job)
		accepted = true
		a.window.Invalidate()
	})
	if !handled {
		job.discard()
	}
	return accepted
}

func (a *app) addCapture() {
	if a.closed || a.collectionDialog != nil || a.linkDialog != nil || a.picking || a.loading || a.store == nil || a.loadError != nil {
		return
	}
	a.captureMenu.dismiss()
	a.picking = true
	returnFocus := a.focus
	destination := a.activeCollectionID()
	a.enqueueLibraryJob(libraryJob{run: func(store *libraryStore) func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: a.window, Title: "Add capture", ButtonLabel: "Import", Multiple: true,
		})
		a.window.Update(func() {
			if a.closed {
				return
			}
			a.picking = false
			a.restoreCaptureFocus(returnFocus)
		})
		if err != nil {
			return func() {
				const title = "Import could not start"
				a.report(title, err.Error(), mediaImportRecovery)
				a.announce(title)
			}
		}
		return a.importMedia(store, paths, destination, a.reportMediaImport)
	}})
}

func (a *app) dropFiles(paths []string) {
	if len(paths) == 0 || a.collectionDialog != nil || a.linkDialog != nil {
		return
	}
	paths = slices.Clone(paths)
	destination := a.activeCollectionID()
	a.enqueueLibraryJob(libraryJob{run: func(store *libraryStore) func() {
		return a.importMedia(store, paths, destination, a.reportMediaImport)
	}})
}

func (a *app) importMedia(store *libraryStore, paths []string, destination string, finish func(importResult)) func() {
	result := store.importFiles(paths, destination)
	return func() {
		if len(paths) > 0 && result.Fatal == nil {
			a.publishSnapshot(manifest{Items: result.Items, Collections: result.Collections})
		}
		if result.Added > 0 {
			if destination == "" {
				a.navigation.selected = sectionLibrary
				a.grid.scroll.Y = 0
			} else if a.activeCollectionID() == destination {
				a.grid.scroll.Y = 0
			}
		}
		if len(paths) > 0 {
			finish(result)
		}
	}
}

func (a *app) linkJob(prepared *preparedLink, destination string, finish func(importResult)) libraryJob {
	return libraryJob{release: prepared.cleanup, run: func(store *libraryStore) func() {
		result := store.saveLink(prepared, destination)
		return func() {
			if result.Fatal == nil {
				a.publishSnapshot(manifest{Items: result.Items, Collections: result.Collections})
			}
			finish(result)
		}
	}}
}

const mediaImportRecovery = "Try Add capture again with readable image or video files."

func (a *app) reportMediaImport(result importResult) {
	a.announce(fmt.Sprintf("%d added, %d already saved, %d failed", result.Added, result.Duplicates, len(result.Failures)))
	a.reportImportWarnings(result)
}

func (a *app) reportImportWarnings(result importResult) {
	if detail := importWarningDetail(result); detail != "" {
		title, recovery := "Import needs attention", ""
		if len(result.Failures) > 0 || result.Fatal != nil {
			title, recovery = "Some files could not be imported", mediaImportRecovery
		}
		a.report(title, detail, recovery)
		a.announce(detail)
	}
}

func importWarningDetail(result importResult) string {
	var lines []string
	for _, failure := range result.Failures {
		lines = append(lines, failure.Filename+": "+failure.Reason)
	}
	if result.Fatal != nil && len(result.Failures) == 0 {
		lines = append(lines, result.Fatal.Error())
	}
	if result.InTrash > 0 {
		lines = append(lines, fmt.Sprintf("%d already in Trash. Open Trash and restore the captures there.", result.InTrash))
	}
	if result.DestinationMissing && result.Added+result.Duplicates-result.InTrash > 0 {
		lines = append(lines, "The destination collection no longer exists; captures remain in Library. Choose another collection to assign them.")
	}
	return strings.Join(lines, "\n\n")
}
