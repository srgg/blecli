package main

import (
	"fmt"
	"sync/atomic"
	"time"
)

const (
	progressUpdateInterval = 100 * time.Millisecond
	clearLineSequence      = "\r\033[K"
)

// ProgressPrinter displays progress messages with elapsed time.
//
// Usage:
//
//	p := NewProgressPrinter(...)
//	p.Start()
//	defer p.Stop()
//
// The caller must call Stop to release resources and terminate the internal
// goroutine; failing to do so will leak a goroutine.
//
// A ProgressPrinter is single-use. Start may be called at most once, and Stop
// should be called exactly once. After Stop, the instance cannot be restarted.
type ProgressPrinter struct {
	prefix     string
	phase      atomic.Value        // stores string - current phase name
	stopPhases map[string]struct{} // set of phases that trigger a graceful shutdown
	startTime  time.Time
	ticker     atomic.Pointer[time.Ticker]
	stopChan   chan struct{}
	done       chan struct{} // closed when goroutine exits
	started    atomic.Bool   // ensures Start is called at most once
	countUp    bool          // true for count up, false for countdown
	duration   time.Duration // for countdown mode
}

// NewProgressPrinter creates a progress printer that counts up (shows elapsed time).
// stopPhases are phase names that will trigger automatic cleanup when set via Callback.
func NewProgressPrinter(prefix string, phase string, stopPhases ...string) *ProgressPrinter {
	stopSet := make(map[string]struct{})
	for _, p := range stopPhases {
		stopSet[p] = struct{}{}
	}
	p := &ProgressPrinter{
		prefix:     prefix,
		stopPhases: stopSet,
		countUp:    true,
	}
	p.phase.Store(phase)
	return p
}

// NewCountdownProgressPrinter creates a progress printer that counts down from the duration.
// stopPhases are phase names that will trigger automatic cleanup when set via Callback.
func NewCountdownProgressPrinter(prefix string, phase string, duration time.Duration, stopPhases ...string) *ProgressPrinter {
	stopSet := make(map[string]struct{})
	for _, p := range stopPhases {
		stopSet[p] = struct{}{}
	}
	p := &ProgressPrinter{
		prefix:     prefix,
		stopPhases: stopSet,
		countUp:    false,
		duration:   duration,
	}
	p.phase.Store(phase)
	return p
}

// Start begins displaying progress updates in a background goroutine.
// Panics if called more than once on the same ProgressPrinter instance.
func (p *ProgressPrinter) Start() {
	if !p.started.CompareAndSwap(false, true) {
		panic("ProgressPrinter.Start called more than once")
	}

	if p.stopChan != nil {
		panic("ProgressPrinter cannot be reused after Stop")
	}

	p.done = make(chan struct{})
	p.stopChan = make(chan struct{})
	p.startTime = time.Now()
	ticker := time.NewTicker(progressUpdateInterval)
	p.ticker.Store(ticker)

	p.startProgressLoop(ticker)
}

// printProgress displays a progress line with optional elapsed/remaining seconds
func (p *ProgressPrinter) printProgress(phase string, seconds int) {
	if seconds > 0 {
		fmt.Printf("\r%s (%s %ds)   ", p.prefix, phase, seconds)
	} else {
		fmt.Printf("\r%s (%s...)   ", p.prefix, phase)
	}
}

// startProgressLoop starts the progress display goroutine.
// Uses p.countUp to determine whether to count up (elapsed time) or down (remaining time).
func (p *ProgressPrinter) startProgressLoop(ticker *time.Ticker) {
	initialPhase := p.phase.Load().(string)
	fmt.Printf("\r%s (%s...)   ", p.prefix, initialPhase)

	go func() {
		defer close(p.done)
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("\nprogress printer panic: %v\n", r)
			}
		}()

		for {
			select {
			case <-p.stopChan:
				return
			case <-ticker.C:
				currentPhase := p.phase.Load().(string)
				// Check if phase is a stop phase - if so, stop
				if _, isStopPhase := p.stopPhases[currentPhase]; isStopPhase {
					return
				}
				elapsed := time.Since(p.startTime)

				var seconds int

				if p.countUp {
					// Count up mode: show elapsed time
					seconds = int(elapsed.Seconds())
				} else {
					// Countdown mode: show remaining time
					remaining := p.duration - elapsed
					if remaining <= 0 {
						// Show 0s when countdown completes (don't auto-stop)
						seconds = 0
					} else {
						// Round to the nearest second (add 0.5 before truncating to int)
						// e.g., 3.7s -> 4s, 3.3s -> 3s
						seconds = int(remaining.Seconds() + 0.5)
					}
				}
				p.printProgress(currentPhase, seconds)
			}
		}
	}()
}

// Callback returns a progress callback function that updates the phase.
// If the new phase is a stop phase, Stop() is called automatically.
// This function is safe to call from multiple goroutines.
func (p *ProgressPrinter) Callback() func(phase string) {
	return func(phase string) {
		p.phase.Store(phase)
		// If this is a stop phase, stop immediately
		if _, isStopPhase := p.stopPhases[phase]; isStopPhase {
			p.Stop()
		}
	}
}

// Stop stops the progress display and clears the line.
// This function is safe to call multiple times and from multiple goroutines.
// Only the first call will actually stop the ticker, wait for goroutine cleanup,
// and clear the progress line from the terminal.
func (p *ProgressPrinter) Stop() {
	ticker := p.ticker.Swap(nil)
	if ticker == nil {
		return // Already stopped
	}

	ticker.Stop()     // Stop ticker before signaling goroutine
	close(p.stopChan) // Wake up goroutine by closing the channel
	<-p.done          // Wait for the goroutine to finish

	p.stopChan = nil // Prevent reuse

	fmt.Print(clearLineSequence)
}
