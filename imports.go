package main

import (
	"fmt"
	"log"
	"strings"
)

// Imports and metadata actions share this queue. A worker publishes its result
// on the UI thread BEFORE the next job starts, so an older import snapshot can
// never overwrite a newer Starred result in memory. The store lock protects disk.
type importInput struct {
	paths       []string
	link        *preparedLink
	cleanup     func()
	failures    []importFailure
	starred     *starredOperation
	trash       *trashOperation
	purge       bool
	deleteIDs   []string
	destination string
	collections *collectionOperation
	completed   func(error)
	// Paste gathers feedback across independent, serialized mutations.
	finished func(importResult, error)
}
type importBatch func() (importInput, error)

func (a *app) addCapture() {
	if a.collectionDialog != nil || a.linkDialog != nil || a.picking || a.loading || a.store == nil || a.loadError != nil || a.pickMedia == nil {
		return
	}
	a.picking = true
	returnFocus := a.focus
	destination := a.activeCollectionID()
	a.enqueueImport(func() (importInput, error) {
		paths, err := a.pickMedia()
		a.update(func() {
			a.picking = false
			if a.navigation.hidden && (returnFocus == focusAddCapture || returnFocus == focusNavigation) {
				a.focus = focusToggle
			} else if returnFocus != "" {
				a.focus = returnFocus
			} else {
				a.focus = focusLibrary
			}
		})
		return importInput{paths: paths, destination: destination}, err
	})
}

func (a *app) enqueueImport(batch importBatch) {
	if a.closed {
		return
	}
	if a.loadError != nil {
		a.report("Library could not be loaded", a.loadError.Error())
		return
	}
	a.importQueue = append(a.importQueue, batch)
	a.startNextImport()
}

func (a *app) startNextImport() {
	if a.closed || a.importing || a.loading || a.store == nil || len(a.importQueue) == 0 {
		return
	}
	batch := a.importQueue[0]
	a.importQueue = a.importQueue[1:]
	a.importing = true
	store := a.store
	go func() {
		input, err := batch()
		result := importResult{Failures: input.failures}
		var purge purgeResult
		var snapshot manifest
		if err == nil && input.link != nil {
			result = store.saveLink(input.link, input.destination)
			err = result.Fatal
		} else if err == nil && input.collections != nil {
			snapshot, err = store.updateCollections(*input.collections)
		} else if err == nil && input.purge {
			purge = store.cleanupExpired()
		} else if err == nil && len(input.deleteIDs) > 0 {
			purge = store.deletePermanently(input.deleteIDs)
		} else if err == nil && input.trash != nil {
			snapshot, err = store.setTrashedSnapshot(input.trash.IDs, input.trash.Restore)
		} else if err == nil && input.starred != nil {
			snapshot, err = store.setStarredSnapshot(input.starred.IDs, input.starred.Value)
		} else if err == nil && len(input.paths) > 0 {
			result = store.importFiles(input.paths, nil, input.destination)
			result.Failures = append(input.failures, result.Failures...)
		}
		// Inputs belong to the batch until importFiles has closed every source.
		if input.cleanup != nil {
			input.cleanup()
		}
		a.update(func() {
			defer func() {
				a.importing = false
				a.startNextImport()
			}()
			if a.closed {
				return
			}
			if input.collections != nil {
				if err == nil {
					a.publishSnapshot(snapshot)
					a.status = "Collections updated"
				} else {
					a.status = "Could not update collections"
				}
				if input.completed != nil {
					input.completed(err)
				} else if err != nil {
					a.report(a.status, err.Error())
				}
				a.announce(a.status)
			} else if input.purge || len(input.deleteIDs) > 0 {
				if input.purge {
					a.cleanupQueued = false
				}
				a.purging = false
				if purge.Publish {
					a.publishSnapshot(manifest{Items: purge.Items, Collections: purge.Collections})
				}
				if purge.Err != nil {
					if len(input.deleteIDs) > 0 {
						a.status = "Could not delete captures permanently"
						detail := purge.Err.Error()
						if purge.Publish {
							a.status = "Deletion could not finish"
							detail = "Check library permissions. Keeps will retry automatically.\n\n" + detail
						}
						a.report(a.status, detail)
						a.announce(a.status)
						return
					}
					a.status = "Trash cleanup could not finish; Keeps will retry."
					if detail := purge.Err.Error(); detail != a.cleanupError {
						a.announce(a.status)
						a.report("Trash cleanup could not finish", "Keeps will retry. "+detail)
						log.Printf("Trash cleanup: %s", detail)
						a.cleanupError = detail
					}
				} else {
					a.cleanupError, a.status = "", ""
					if len(input.deleteIDs) > 0 {
						a.status = "Deleted permanently"
						a.announce(a.status)
					}
				}
			} else if input.trash != nil {
				if err != nil {
					a.status = "Could not move captures to Trash"
					if input.trash.Restore {
						a.status = "Could not restore captures"
					}
					a.report(a.status, err.Error())
				} else {
					a.publishSnapshot(snapshot)
					a.status = "Moved to Trash"
					if input.trash.Restore {
						a.status = "Restored"
					}
				}
				a.announce(a.status)
			} else if input.starred != nil {
				if err != nil {
					a.status = "Could not update Starred"
					a.report(a.status, err.Error())
				} else {
					a.publishSnapshot(snapshot)
					a.status = "Added to Starred"
					if !input.starred.Value {
						a.status = "Removed from Starred"
					}
				}
				a.announce(a.status)
			} else {
				a.finishCaptureImport(input, result, err)
			}
		})
	}()
}

// This text contains only failures and actions the user can take. Routine
// completion counts are announced separately, never placed in a warning.
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

func (a *app) finishCaptureImport(input importInput, result importResult, err error) {
	if err == nil && result.Fatal == nil && (input.link != nil || len(input.paths) > 0) {
		a.publishSnapshot(manifest{Items: result.Items, Collections: result.Collections})
	}
	if result.Added > 0 && len(input.paths) > 0 {
		if input.destination == "" {
			a.navigation.selected = sectionLibrary
			a.libraryScroll.Y = 0
		} else if a.activeCollectionID() == input.destination {
			a.libraryScroll.Y = 0
		}
	}
	if input.finished != nil {
		// The callback observes the committed snapshot before any next job.
		input.finished(result, err)
		return
	}
	if input.link != nil {
		a.status = linkSavedStatus(result, input.link)
		if err != nil {
			a.status = "Could not save link"
		}
		if input.completed != nil {
			input.completed(err) // Add Link keeps save errors inline.
		} else if err != nil {
			a.report(a.status, err.Error())
		}
		a.announce(a.status)
		if err != nil {
			return
		}
	} else if err != nil {
		a.status = "Import could not start"
		a.report(a.status, err.Error())
		a.announce(a.status)
		return
	} else if len(input.paths) > 0 {
		a.status = fmt.Sprintf("%d added, %d already saved, %d failed", result.Added, result.Duplicates, len(result.Failures))
		a.announce(a.status)
	}
	if detail := importWarningDetail(result); detail != "" {
		title := "Import needs attention"
		if len(result.Failures) > 0 || result.Fatal != nil {
			title = "Some files could not be imported"
		}
		a.report(title, detail)
		a.announce(detail)
	}
}

func (a *app) dropFiles(paths []string) {
	if a.collectionDialog != nil || a.linkDialog != nil {
		return
	}
	frozen := append([]string(nil), paths...)
	if len(frozen) == 0 {
		return
	}
	destination := a.activeCollectionID()
	a.enqueueImport(func() (importInput, error) { return importInput{paths: frozen, destination: destination}, nil })
}
