package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo/transfer"
)

type pasteRequest struct {
	data        transfer.Data
	destination string
}

// One paste prepares one link at a time. Other library mutations can proceed
// during network/image preparation. No metadata lock is held by this worker.
type pasteWork struct {
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	pending *pasteCommit
}

// Pending inputs belong to the paste until the mutation queue claims them.
// Shutdown can discard a queued input, but never assets an active writer uses.
type pasteCommit struct {
	input importInput
	state atomic.Int32 // 0: pending, 1: writer owns it, 2: released
}

func (p *pasteCommit) discard() {
	if p.state.CompareAndSwap(0, 2) && p.input.cleanup != nil {
		p.input.cleanup()
	}
}

func (p *pasteCommit) batch() (importInput, error) {
	if !p.state.CompareAndSwap(0, 1) {
		return importInput{}, context.Canceled
	}
	input := p.input
	input.cleanup = func() {
		if p.input.cleanup != nil {
			p.input.cleanup()
		}
		p.state.Store(2)
	}
	return input, nil
}

func (w *pasteWork) stop() {
	w.cancel()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pending != nil {
		w.pending.discard()
	}
}

func (a *app) clipboardBlocked() bool {
	return a.closed || a.picking || a.nativeDialogs > 0 || a.collectionDialog != nil || a.linkDialog != nil || a.previewOwnsFocus()
}

func (a *app) startNextPaste() {
	if a.closed || a.loading || a.store == nil || a.loadError != nil || a.pasteWork != nil || len(a.pasteQueue) == 0 {
		return
	}
	request := a.pasteQueue[0]
	a.pasteQueue[0] = pasteRequest{}
	a.pasteQueue = a.pasteQueue[1:]
	ctx, cancel := context.WithCancel(context.Background())
	w := &pasteWork{ctx: ctx, cancel: cancel}
	a.pasteWork = w
	store := a.store
	go func() {
		result := a.runPaste(w, store, request)
		w.stop()
		a.update(func() {
			if a.closed {
				return
			}
			a.pasteWork = nil
			if detail := importWarningDetail(result); detail != "" {
				a.report("Paste needs attention", detail)
				a.announce(detail)
			} else if result.Added+result.Duplicates > 0 {
				a.announce(fmt.Sprintf("%d added, %d already saved", result.Added, result.Duplicates))
			}
			a.startNextPaste()
		})
	}()
}

func (a *app) runPaste(w *pasteWork, store *libraryStore, request pasteRequest) (total importResult) {
	input, err := pastedInputs(request.data)
	total.Failures = input.failures
	if err != nil {
		total.Failures = append(total.Failures, importFailure{"Clipboard", err.Error()})
	}
	merge := func(result importResult, err error, label string) {
		total.Added += result.Added
		total.Duplicates += result.Duplicates
		total.InTrash += result.InTrash
		total.DestinationMissing = total.DestinationMissing || result.DestinationMissing
		total.Failures = append(total.Failures, result.Failures...)
		if err == nil {
			err = result.Fatal
		}
		if err != nil && len(result.Failures) == 0 {
			total.Failures = append(total.Failures, importFailure{label, err.Error()})
		}
	}
	if len(input.paths) > 0 {
		result, err := a.commitPaste(w, importInput{paths: input.paths, cleanup: input.cleanup, destination: request.destination})
		merge(result, err, "Clipboard media")
	} else {
		input.cleanup()
	}
	for _, raw := range input.urls {
		if w.ctx.Err() != nil {
			break
		}
		prepared, err := store.prepareLink(w.ctx, raw)
		if err != nil {
			total.Failures = append(total.Failures, importFailure{raw, err.Error()})
			continue
		}
		result, err := a.commitPaste(w, importInput{link: prepared, cleanup: prepared.cleanup, destination: request.destination})
		merge(result, err, raw)
	}
	return total
}

// Wait for publication, not just the disk write. A buffered response also lets
// an in-flight writer finish cleanup after shutdown has canceled its waiter.
func (a *app) commitPaste(w *pasteWork, input importInput) (importResult, error) {
	type response struct {
		result importResult
		err    error
	}
	done := make(chan response, 1)
	input.finished = func(result importResult, err error) { done <- response{result, err} }
	pending := &pasteCommit{input: input}
	w.mu.Lock()
	if w.ctx.Err() != nil {
		w.mu.Unlock()
		pending.discard()
		return importResult{}, w.ctx.Err()
	}
	w.pending = pending
	w.mu.Unlock()
	a.update(func() {
		if a.closed || w.ctx.Err() != nil {
			pending.discard()
			return
		}
		if a.loadError != nil || a.store == nil {
			pending.discard()
			done <- response{err: fmt.Errorf("Library is unavailable. Reopen Keeps and try Paste again.")}
			return
		}
		a.enqueueImport(pending.batch)
	})
	select {
	case result := <-done:
		return result.result, result.err
	case <-w.ctx.Done():
		return importResult{}, w.ctx.Err()
	}
}
