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
	request := func() { a.window.Update(a.requestCleanup) }
	stop := scheduleRetention(ticker.C, request)
	off := mygo.Power.OnResume(request)
	a.stopRetention = func() { off(); ticker.Stop(); stop() }
}
