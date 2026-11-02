package main

import "time"

// runOnTicks calls fn for each tick and returns when ticks is closed. A tick
// that arrives while fn is still running is dropped, so a slow sensor or a
// stalled broker write delays only its own schedule and never queues up work.
func runOnTicks(ticks <-chan time.Time, fn func()) {
	busy := make(chan struct{}, 1)
	for range ticks {
		select {
		case busy <- struct{}{}:
			go func() {
				defer func() { <-busy }()
				fn()
			}()
		default:
			// previous run still in progress; skip this tick
		}
	}
}
