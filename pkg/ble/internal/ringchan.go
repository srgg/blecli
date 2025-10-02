package internal

// RingChannel is a bounded channel-like buffer with overwrite-oldest semantics.
//
// It wraps an underlying buffered channel and ensures producers never block
// indefinitely: if the buffer is full, the oldest element is discarded.
//
// # Example
//
//	rc := internal.NewRingChannel
//
//	// Writer: always succeeds, drops oldest if full.
//	for i := 0; i < 10; i++ {
//	    rc.Send(i)
//	}
//
//	// Reader: acts like a normal Go channel.
//	for v := range rc.C() {
//	    fmt.Println("got:", v)
//	}
//
// In the example above, only the *last 3* values will be printed because
// earlier ones were overwritten.
//
// Writers use methods like Send, TrySend, or ForceSend.
// Readers treat rc.C() as a normal <-chan T.
type RingChannel[T any] struct {
	ch chan T
}

// NewRingChannel New creates a RingChan with the given capacity.
func NewRingChannel[T any](capacity int) *RingChannel[T] {
	if capacity <= 0 {
		panic("ringchan: capacity must be > 0")
	}
	return &RingChannel[T]{ch: make(chan T, capacity)}
}

// C returns the underlying receive-only channel.
// Consumers can range over this until it's closed.
func (rc *RingChannel[T]) C() <-chan T {
	return rc.ch
}

// Send inserts an item. If the buffer is full, it discards the oldest.
// This call always succeeds and never blocks indefinitely.
func (rc *RingChannel[T]) Send(v T) {
	select {
	case rc.ch <- v:
	default:
		<-rc.ch // drop oldest
		rc.ch <- v
	}
}

// TrySend attempts to insert without blocking.
// Returns true if successful, false if the buffer is full.
func (rc *RingChannel[T]) TrySend(v T) bool {
	select {
	case rc.ch <- v:
		return true
	default:
		return false
	}
}

// ForceSend always succeeds immediately, discarding the oldest if needed.
// It never blocks.
func (rc *RingChannel[T]) ForceSend(v T) {
	select {
	case rc.ch <- v:
	default:
		select {
		case <-rc.ch: // drop oldest
		default:
		}
		rc.ch <- v
	}
}

// Receive blocks until a value is available or the channel is closed.
// The ok result is false if the channel is closed.
func (rc *RingChannel[T]) Receive() (v T, ok bool) {
	v, ok = <-rc.ch
	return
}

// TryReceive attempts a non-blocking receive.
// Returns (zero, false) if no value is ready.
func (rc *RingChannel[T]) TryReceive() (v T, ok bool) {
	select {
	case v, ok = <-rc.ch:
		return
	default:
		var zero T
		return zero, false
	}
}

// Len returns the number of buffered elements.
func (rc *RingChannel[T]) Len() int {
	return len(rc.ch)
}

// Cap returns the channel capacity.
func (rc *RingChannel[T]) Cap() int {
	return cap(rc.ch)
}

// Close closes the underlying channel. After this, Send/ForceSend panics.
func (rc *RingChannel[T]) Close() {
	close(rc.ch)
}
