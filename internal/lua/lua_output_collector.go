package lua

import (
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"time"

	"github.com/hedzr/go-ringbuf/v2/mpmc"
)

// LuaOutputCollectorMetrics provides lock-free metrics tracking for LuaOutputCollector
// All fields use atomic operations for thread-safe access
type LuaOutputCollectorMetrics struct {
	RecordsProcessed int64 // Total records successfully processed
	ErrorsOccurred   int64 // Total errors encountered

	// TODO: add proper collection after https://github.com/hedzr/go-ringbuf/issues/7 will be somehow added
	RecordsOverwritten int64 // Records lost due to buffer overflow
}

// IncrementRecordsProcessed atomically increments the records processed counter
func (m *LuaOutputCollectorMetrics) IncrementRecordsProcessed() {
	atomic.AddInt64(&m.RecordsProcessed, 1)
}

// IncrementErrorsOccurred atomically increments the error counter
func (m *LuaOutputCollectorMetrics) IncrementErrorsOccurred() {
	atomic.AddInt64(&m.ErrorsOccurred, 1)
}

// IncrementRecordsOverwritten atomically increments the overwritten records counter
func (m *LuaOutputCollectorMetrics) IncrementRecordsOverwritten(count uint32) {
	atomic.AddInt64(&m.RecordsOverwritten, int64(count))
}

// GetRecordsProcessed atomically reads the record processed counter
func (m *LuaOutputCollectorMetrics) GetRecordsProcessed() int64 {
	return atomic.LoadInt64(&m.RecordsProcessed)
}

// GetErrorsOccurred atomically reads the error counter
func (m *LuaOutputCollectorMetrics) GetErrorsOccurred() int64 {
	return atomic.LoadInt64(&m.ErrorsOccurred)
}

// GetRecordsOverwritten atomically reads the overwritten records counter
func (m *LuaOutputCollectorMetrics) GetRecordsOverwritten() int64 {
	return atomic.LoadInt64(&m.RecordsOverwritten)
}

// Reset resets all counters to zero
func (m *LuaOutputCollectorMetrics) Reset() {
	atomic.StoreInt64(&m.RecordsProcessed, 0)
	atomic.StoreInt64(&m.ErrorsOccurred, 0)
	atomic.StoreInt64(&m.RecordsOverwritten, 0)
}

// LuaOutputCollector gathers output records from concurrent Lua execution into
// a ring buffer and exposes them to a pluggable ConsumerFunc with metrics tracking.
//
// All methods are thread-safe.
type LuaOutputCollector struct {
	outputChan <-chan LuaOutputRecord
	buffer     mpmc.RichOverlappedRingBuffer[LuaOutputRecord]
	stop       chan struct{}
	done       chan struct{}             // signals when goroutine has stopped
	onError    func(error)               // error handler, defaults to panic if nil
	metrics    LuaOutputCollectorMetrics // lock-free metrics tracking
	state      uint32                    // atomic state using CollectorState constants (uint32 required for atomic ops)
}

const (
	// LuaOutputCollectorState the lifecycle state of a LuaOutputCollector
	CollectorStateNotRunning uint32 = iota // Collector is not running and ready to start
	CollectorStateRunning                  // Collector is running and processing records
	CollectorStateStopping                 // Collector is in the process of stopping

	// MaxBufferSize sets an upper limit on the buffer size to guard against accidental misconfiguration.
	MaxBufferSize uint32 = 1024 * 1024 // 1M records max
)

// NewLuaOutputCollector creates a new collector.
// bufferSize sets the ring buffer size
// onError is called when unexpected errors occur; if nil, it panics on any collecting error
func NewLuaOutputCollector(ch <-chan LuaOutputRecord, bufferSize uint32, onError func(error)) (*LuaOutputCollector, error) {
	if ch == nil {
		return nil, fmt.Errorf("output channel cannot be nil")
	}

	if bufferSize == 0 {
		return nil, fmt.Errorf("buffer size must be > 0")
	}

	if bufferSize > MaxBufferSize {
		return nil, fmt.Errorf("buffer size %d exceeds maximum %d", bufferSize, MaxBufferSize)
	}

	// Default to panic if no error handler provided (backward compatibility)
	if onError == nil {
		onError = func(err error) {
			panic(fmt.Sprintf("LuaOutputCollector: %v", err))
		}
	}

	return &LuaOutputCollector{
		outputChan: ch,
		buffer:     mpmc.NewOverlappedRingBuffer[LuaOutputRecord](bufferSize),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
		onError:    onError,
		metrics:    LuaOutputCollectorMetrics{}, // Initialize metrics
		state:      CollectorStateNotRunning,    // Initialize state
	}, nil
}

// Start begins collecting output records.
// Blocks until the collector goroutine is running or times out.
// Returns an error if already started or if startup takes too long.
func (c *LuaOutputCollector) Start() error {
	// Transition directly from NotRunning to Running (no intermediate Starting state needed)
	if !atomic.CompareAndSwapUint32(&c.state, CollectorStateNotRunning, CollectorStateRunning) {
		currentState := atomic.LoadUint32(&c.state)
		switch currentState {
		case CollectorStateRunning:
			return fmt.Errorf("collector is already running")
		case CollectorStateStopping:
			return fmt.Errorf("collector is stopping, wait for it to finish")
		default:
			return fmt.Errorf("collector is in unknown state %d", currentState)
		}
	}

	// Create fresh channels for this start cycle to prevent "close of closed channel" panics
	c.stop = make(chan struct{})
	c.done = make(chan struct{})

	// Buffered channel for startup signaling. Buffered (not context.Context) because:
	// - Simple one-time signal doesn't need context's propagation semantics
	// - Buffer prevents goroutine blocking even if timeout occurs before signal is sent
	// - Clearer control flow: timeout → close(c.stop) → goroutine exits cleanly
	started := make(chan struct{}, 1)

	go func() {
		// Signal that goroutine is running (non-blocking due to buffer)
		started <- struct{}{}

		defer func() {
			close(c.done)
			atomic.StoreUint32(&c.state, CollectorStateNotRunning) // Reset state on exit
		}()
		for {
			select {
			case <-c.stop:
				return
			case rec, ok := <-c.outputChan:
				if !ok {
					return // channel closed
				}
				// Ring buffer automatically handles overflow by dropping the oldest
				if overwrites, err := c.buffer.EnqueueM(rec); err != nil {
					c.metrics.IncrementErrorsOccurred()
					c.onError(fmt.Errorf("unexpected buffer.Enqueue error: %w", err))
					return
				} else {
					c.metrics.IncrementRecordsOverwritten(overwrites)
					c.metrics.IncrementRecordsProcessed()
				}
			}
		}
	}()

	// Wait for goroutine to signal it's running, or timeout
	select {
	case <-started:
		return nil
	case <-time.After(1 * time.Second):
		// Timeout: stop the goroutine and wait for clean exit
		close(c.stop)
		<-c.done
		return fmt.Errorf("collector failed to start within 1s timeout")
	}
}

// Stop stops an output collection.
// Returns an error if stopping takes longer than expected.
func (c *LuaOutputCollector) Stop() error {
	// Use CAS to transition from Running to Stopping
	if !atomic.CompareAndSwapUint32(&c.state, CollectorStateRunning, CollectorStateStopping) {
		currentState := atomic.LoadUint32(&c.state)
		switch currentState {
		case CollectorStateNotRunning:
			return nil // Already stopped
		case CollectorStateStopping:
			// Already stopping, wait for completion
			break
		default:
			return fmt.Errorf("collector is in unknown state %d", currentState)
		}
	} else {
		// Successfully transitioned to stopping, close the stop channel
		close(c.stop)
	}

	// Wait for the goroutine to finish (symmetric with Start's timeout handling)
	select {
	case <-c.done:
		return nil
	case <-time.After(5 * time.Second):
		// Timeout: goroutine is slow but we must wait for clean shutdown
		// We already signaled stop (closed c.stop), now ensure goroutine actually exits
		<-c.done // Block indefinitely until goroutine exits (ensures state consistency)
		return fmt.Errorf("stop completed but exceeded 5s timeout (possible slow shutdown or deadlock)")
	}
}

// GetMetrics returns a copy of the current metrics
func (c *LuaOutputCollector) GetMetrics() LuaOutputCollectorMetrics {
	return LuaOutputCollectorMetrics{
		RecordsProcessed:   c.metrics.GetRecordsProcessed(),
		ErrorsOccurred:     c.metrics.GetErrorsOccurred(),
		RecordsOverwritten: c.metrics.GetRecordsOverwritten(),
	}
}

// ResetMetrics atomically resets all metric counters
func (c *LuaOutputCollector) ResetMetrics() {
	c.metrics.Reset()
}

// ConsumerFunc defines the signature of a function that consumes output records.
//
// Protocol:
// - If record != nil: Process the record.
// Return (nil, nil) to continue processing more records.
// Return (result, nil) to stop early with a final result.
// - If record == nil: No more records will be provided.
// Return the final accumulated result.
//
// The function is responsible for managing any internal state or buffers
// needed across calls.
//
// For a ready-to-use example implementation, see PlainTextOutputConsumerFunc.
type ConsumerFunc[T any] func(record *LuaOutputRecord) (T, error)

// PlainTextOutputConsumerFunc returns a ConsumerFunc that concatenates plain-text
// output into a single string, ignoring metadata.
func PlainTextOutputConsumerFunc() ConsumerFunc[string] {
	var buffer strings.Builder
	return func(record *LuaOutputRecord) (string, error) {
		if record == nil {
			// No more data - return accumulated buffer
			return buffer.String(), nil
		}
		// Accumulate record content and continue
		buffer.WriteString(record.Content)
		return "", nil // Continue processing (empty string = zero value)
	}
}

// ConsumeRecords drains all buffered records and passes them to the given ConsumerFunc.
//
// The consumer decides when to stop and what result to return. See ConsumerFunc for the processing protocol.
func ConsumeRecords[T any](c *LuaOutputCollector, consumer ConsumerFunc[T]) (T, error) {
	for !c.buffer.IsEmpty() {
		rec, err := c.buffer.Dequeue()
		if err != nil {
			// Return error from dequeue operation
			var zero T
			return zero, fmt.Errorf("buffer dequeue error: %w", err)
		}

		result, err := consumer(&rec)
		if err != nil {
			return result, err
		}

		// Check if result is non-zero (consumer wants to stop)
		if !isZeroValue(result) {
			return result, nil
		}
	}

	// No more data - call consumer with nil to get final result
	return consumer(nil)
}

// isZeroValue checks if a value is the zero value for its type
func isZeroValue[T any](v T) bool {
	var zero T
	return reflect.DeepEqual(v, zero)
}

// GetState returns the current state of the collector
func (c *LuaOutputCollector) GetState() uint32 {
	return atomic.LoadUint32(&c.state)
}

// ConsumePlainText processes all output records and returns their content
// as a single concatenated string, ignoring metadata such as timestamps
// or source information
func (c *LuaOutputCollector) ConsumePlainText() (string, error) {
	return ConsumeRecords(c, PlainTextOutputConsumerFunc())
}
