package main

import (
	"context"
	"fmt"

	"github.com/egoist/mygo/transfer"
)

type pasteRequest struct {
	data        transfer.Data
	destination string
}

func (a *app) startNextPaste() {
	if a.closed || a.loading || a.store == nil || a.loadError != nil || a.pasteCancel != nil || len(a.pasteQueue) == 0 {
		return
	}
	request := a.pasteQueue[0]
	a.pasteQueue[0] = pasteRequest{}
	a.pasteQueue = a.pasteQueue[1:]
	ctx, cancel := context.WithCancel(context.Background())
	a.pasteCancel = cancel
	store := a.store
	go func() {
		result := a.runPaste(ctx, store, request)
		cancel()
		a.window.Update(func() {
			if a.closed {
				return
			}
			a.pasteCancel = nil
			if detail := importWarningDetail(result); detail != "" {
				a.report("Paste needs attention", detail, "")
				a.announce(detail)
			} else if result.Added+result.Duplicates > 0 {
				a.announce(fmt.Sprintf("%d added, %d already saved", result.Added, result.Duplicates))
			}
			a.startNextPaste()
		})
	}()
}

// One paste prepares one link at a time, without holding the mutation queue
// or metadata lock while fetching metadata and images.
func (a *app) runPaste(ctx context.Context, store *libraryStore, request pasteRequest) (total importResult) {
	input := pastedInputs(request.data)
	total.Failures = input.failures
	merge := func(result importResult, label string) {
		total.Added += result.Added
		total.Duplicates += result.Duplicates
		total.InTrash += result.InTrash
		total.DestinationMissing = total.DestinationMissing || result.DestinationMissing
		total.Failures = append(total.Failures, result.Failures...)
		if result.Fatal != nil && len(result.Failures) == 0 {
			total.Failures = append(total.Failures, importFailure{label, result.Fatal.Error()})
		}
	}
	if len(input.paths) > 0 {
		result := a.commitPaste(ctx, func(finish func(importResult)) libraryJob {
			return libraryJob{release: input.cleanup, run: func(store *libraryStore) func() {
				return a.importMedia(store, input.paths, request.destination, finish)
			}}
		})
		merge(result, "Clipboard media")
	} else {
		input.cleanup()
	}
	for _, raw := range input.urls {
		if ctx.Err() != nil {
			break
		}
		prepared, err := store.prepareLink(ctx, raw)
		if err != nil {
			total.Failures = append(total.Failures, importFailure{raw, err.Error()})
			continue
		}
		result := a.commitPaste(ctx, func(finish func(importResult)) libraryJob {
			return a.linkJob(prepared, request.destination, finish)
		})
		merge(result, raw)
	}
	return total
}

func (a *app) commitPaste(ctx context.Context, makeJob func(func(importResult)) libraryJob) importResult {
	// The buffered response lets the UI finish even if cancellation wins the wait.
	done := make(chan importResult, 1)
	job := makeJob(func(result importResult) { done <- result })
	if !a.submitPrepared(ctx, job, func(job libraryJob) {
		if a.loadError != nil || a.store == nil {
			job.discard()
			done <- importResult{Fatal: fmt.Errorf("Library is unavailable. Reopen Keeps and try Paste again.")}
			return
		}
		a.enqueueLibraryJob(job)
	}) {
		return importResult{Fatal: context.Canceled}
	}
	select {
	case result := <-done:
		return result
	case <-ctx.Done():
		return importResult{Fatal: ctx.Err()}
	}
}
