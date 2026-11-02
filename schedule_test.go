package main

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestRunOnTicksSkipsWhileBusy(t *testing.T) {
	ticks := make(chan time.Time)
	release := make(chan struct{})
	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		runOnTicks(ticks, func() {
			calls.Add(1)
			<-release
		})
		close(done)
	}()

	ticks <- time.Now() // starts a run that blocks
	waitFor(t, func() bool { return calls.Load() == 1 })
	ticks <- time.Now() // must be skipped, not queued
	ticks <- time.Now()
	time.Sleep(20 * time.Millisecond)
	if n := calls.Load(); n != 1 {
		t.Fatalf("ticks ran while the previous run was busy: %d calls", n)
	}

	close(release)
	// The busy slot is released asynchronously after fn returns, so a tick
	// that lands in that window is skipped; keep ticking until the run starts.
	waitFor(t, func() bool {
		select {
		case ticks <- time.Now():
		default:
		}
		return calls.Load() == 2
	})

	close(ticks)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runOnTicks did not return after the channel closed")
	}
}
