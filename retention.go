package main

import (
	"sync"
	"time"

	"github.com/egoist/mygo"
)

// Every request is delivered to the UI owner; requestCleanup coalesces queued/running
// work and ignores callbacks posted before shutdown but delivered after it.
func scheduleRetention(ticks <-chan time.Time, request func()) func() {
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			case <-ticks:
				request()
			}
		}
	}()
	return sync.OnceFunc(func() { close(stop); <-done })
}

func (a *app) startRetention() {
	if a.closed || a.stopRetention != nil {
		return
	}
	ticker := time.NewTicker(time.Hour)
	request := func() { a.update(a.requestCleanup) }
	stop := scheduleRetention(ticker.C, request)
	off := mygo.Power.OnResume(request)
	a.stopRetention = func() { off(); ticker.Stop(); stop() }
	a.requestCleanup()
}

func (a *app) shutdown() {
	if a.closed {
		return
	}
	a.closed = true
	if a.pasteWork != nil {
		a.pasteWork.stop()
	}
	a.pasteQueue = nil
	if d := a.linkDialog; d != nil {
		if d.cancel != nil {
			d.cancel()
		}
		if d.prepared != nil && d.prepared.state.CompareAndSwap(0, 2) {
			d.prepared.cleanup()
		}
	}
	if a.media != nil {
		if a.media.stop != nil {
			a.media.stop()
		}
		a.media.releaseInline()
		a.media.backend.close()
	}
	if a.stopRetention != nil {
		a.stopRetention()
	}
	if a.thumbnails != nil {
		close(a.thumbnails.stop)
	}
	a.importQueue = nil
}
