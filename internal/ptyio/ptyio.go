// Package ptyio provides a Ring-based async PTY master wrapper optimized
// for high-throughput use. It creates a pair with github.com/crack/pty.
//
// # Basic Usage
//
//	// Create PTY pair (returns PTY interface):
//	pty, err := ptyio.NewPty(readCap, writeCap, logger)
//	if err != nil {
//	    return err
//	}
//	// pty.TTYName() -> "/dev/pts/X"
//
//	// Send to slave (non-blocking, may overwrite oldest):
//	n, err := pty.Write([]byte("hello\n"))
//
//	// Read output produced by slave (non-blocking):
//	buf := make([]byte, 4096)
//	n, err := pty.Read(buf)
//
// # Poll Timeout Tuning
//
// The poll timeout controls the maximum time goroutines wait for I/O readiness
// before checking context cancellation. This affects both responsiveness and CPU usage.
//
// Use cases and recommended settings:
//
//	Interactive terminals (low latency required):
//	  PollTimeoutMs: 10-25ms
//	  - Provides sub-frame response time for human interaction
//	  - Shutdown latency: ~10-25ms
//	  - CPU impact: Moderate (goroutines wake up 40-100x per second when idle)
//
//	General purpose (balanced):
//	  PollTimeoutMs: 50ms (default)
//	  - Good balance for most applications
//	  - Shutdown latency: ~50ms
//	  - CPU impact: Low (goroutines wake up 20x per second when idle)
//
//	Batch processing / high-throughput logging:
//	  PollTimeoutMs: 100-200ms
//	  - Minimizes CPU overhead for background tasks
//	  - Shutdown latency: ~100-200ms
//	  - CPU impact: Minimal (goroutines wake up 5-10x per second when idle)
//
//	Real-time data streaming:
//	  PollTimeoutMs: 5-10ms
//	  - Ultra-low latency for time-sensitive data
//	  - Shutdown latency: ~5-10ms
//	  - CPU impact: Higher (goroutines wake up 100-200x per second when idle)
//
// Note: Actual CPU usage depends on I/O activity. When data is flowing continuously,
// poll timeout has minimal impact since goroutines rarely wait for the full timeout.
// The timeout primarily affects idle periods and shutdown responsiveness.
package ptyio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/creack/pty"
	"github.com/sirupsen/logrus"
	"github.com/smallnest/ringbuffer"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// ErrorCallback is invoked when a critical error occurs in read/write loops.
// This callback is called from background goroutines, so implementations must be thread-safe.
// The PTY remains in a degraded state after the error - Close() should be called.
type ErrorCallback func(err error)

// PTYOptions configures PTY creation with fine-grained control over behavior.
// Zero values use sensible defaults (see DefaultPollTimeoutMs constant).
type PTYOptions struct {
	ReadCap       int            // Ring buffer capacity for data read from PTY (bytes from slave)
	WriteCap      int            // Ring buffer capacity for data written to PTY (bytes to slave)
	Logger        *logrus.Logger // Optional logger (nil = no-op logger)
	OnError       ErrorCallback  // Optional callback for critical loop failures
	PollTimeoutMs int            // Poll timeout in milliseconds (0 = use DefaultPollTimeoutMs)
}

// PTY provides a non-blocking interface to pseudo-terminal devices.
// Implements io.ReadWriteCloser for compatibility with standard Go interfaces.
type PTY interface {
	io.ReadWriteCloser // Standard Go read/write/close interface
	Stats() Stats      // runtime metrics
	TTYName() string   // path of a tty device, empty if unknown
}

// Stats provides runtime counters useful for monitoring/backpressure.
type Stats struct {
	WriteQueueLen int32 // approximate
	WriteQueueCap int32
	ReadQueueLen  int32
	ReadQueueCap  int32

	DroppedWriteCount uint64 // how many bytes were dropped due to write buffer overflow
	DroppedReadCount  uint64 // how many bytes were dropped due to read buffer overflow
	ReadBytesTotal    uint64
	WriteBytesTotal   uint64
}

const (
	// DefaultPollTimeoutMs is the default poll timeout in milliseconds for I/O operations.
	// This affects shutdown latency (max delay before goroutines detect context cancellation)
	// and CPU usage (shorter = more responsive but higher CPU usage when idle).
	// Exported so users can reference it when creating custom PTYOptions.
	DefaultPollTimeoutMs = 50
)

// noopLogger is a shared logger instance that discards all output.
// Used when no logger is provided to avoid allocating a new logger for each PTY.
var noopLogger = func() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	return l
}()

// ringPTY implements PTY using ring buffers for non-blocking I/O.
// It wraps a PTY master/slave pair with background goroutines for async read/write,
// providing backpressure management via ring buffer semantics (oldest data dropped when full).
type ringPTY struct {
	logger         *logrus.Logger
	tty            *os.File      // slave
	pty            *os.File      // master
	onError        ErrorCallback // optional callback for critical errors
	writeErrorOnce sync.Once     // ensures write error callback is called at most once
	readErrorOnce  sync.Once     // ensures read error callback is called at most once
	pollTimeoutMs  int           // poll timeout in milliseconds

	writeBuf *ringbuffer.RingBuffer // bytes to write to PTY
	readBuf  *ringbuffer.RingBuffer // bytes read from PTY

	// internals
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	closed uint32 // atomic boolean

	// metrics
	droppedWrite uint64
	droppedRead  uint64
	readBytes    uint64
	writeBytes   uint64

	ttyName string
}

// NewPty creates a new PTY ptyx(master)/tty(slave) pair, wraps the master in ringPTY,
// and returns the PTY interface. The caller may hand off the slaveFile to another
// process. If the logger is nil, a no-op logger will be used.
func NewPty(readCap, writeCap int, logger *logrus.Logger) (PTY, error) {
	return NewPtyWithErrorHandler(readCap, writeCap, logger, nil)
}

// NewPtyWithErrorHandler creates a new PTY with an optional error callback for monitoring
// critical failures in background read/write loops. If onError is nil, errors are only logged.
//
// The callback is invoked at most once per loop (read/write) when a critical error occurs
// that causes the loop to exit. After callback invocation, the PTY remains in a degraded state
// and Close() should be called.
//
// Example usage with error handling:
//
//	pty, err := ptyio.NewPtyWithErrorHandler(1024, 1024, logger, func(err error) {
//	    log.Printf("PTY critical error: %v", err)
//	    // Trigger reconnection, cleanup, or user notification
//	})
func NewPtyWithErrorHandler(readCap, writeCap int, logger *logrus.Logger, onError ErrorCallback) (PTY, error) {
	return NewPtyWithOptions(&PTYOptions{
		ReadCap:  readCap,
		WriteCap: writeCap,
		Logger:   logger,
		OnError:  onError,
	})
}

// NewPtyWithOptions creates a new PTY with full configuration control.
// This is the most flexible constructor, allowing fine-grained tuning of all parameters.
//
// Example usage with custom poll timeout:
//
//	pty, err := ptyio.NewPtyWithOptions(&ptyio.PTYOptions{
//	    ReadCap:       4096,
//	    WriteCap:      4096,
//	    Logger:        logger,
//	    PollTimeoutMs: 10, // Lower latency for interactive applications
//	    OnError: func(err error) {
//	        log.Printf("PTY error: %v", err)
//	    },
//	})
func NewPtyWithOptions(opts *PTYOptions) (PTY, error) {
	if opts == nil {
		return nil, fmt.Errorf("PTYOptions cannot be nil")
	}

	master, slave, err := createPTY()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Get slave name - keep slave FD open so PTY remains in a connected state
	// External processes can still open additional FDs to the same slave device
	slaveName := slave.Name()

	// Apply defaults
	logger := opts.Logger
	if logger == nil {
		logger = noopLogger
	}

	pollTimeout := opts.PollTimeoutMs
	if pollTimeout == 0 {
		pollTimeout = DefaultPollTimeoutMs
	}

	p := &ringPTY{
		logger:        logger,
		pty:           master,
		tty:           slave, // keep slave open for PTY state
		ttyName:       slaveName,
		writeBuf:      ringbuffer.New(opts.WriteCap),
		readBuf:       ringbuffer.New(opts.ReadCap),
		ctx:           ctx,
		cancel:        cancel,
		onError:       opts.OnError,
		pollTimeoutMs: pollTimeout,
	}

	// start goroutines
	p.wg.Add(2)
	go p.readLoop()
	go p.writeLoop()

	return p, nil
}

func (p *ringPTY) writeLoop() {
	defer p.wg.Done()

	fd := int(p.pty.Fd())
	pollFd := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLOUT}}
	buf := make([]byte, 4096) // Write buffer for batching bytes from a ring

	for {
		select {
		case <-p.ctx.Done():
			return
		default:
		}

		// Check if there's data to write
		if p.writeBuf.IsEmpty() {
			// No data, check context with timeout
			nReady, err := unix.Poll(pollFd, p.pollTimeoutMs)
			if err != nil && !errors.Is(err, syscall.EINTR) {
				p.logger.Warnf("writeLoop poll error: %v", err)
			}
			if nReady == 0 {
				continue // timeout, check context
			}
		}

		// Read bytes from the ring buffer (bulk operation)
		n, err := p.writeBuf.TryRead(buf)
		if err != nil && !errors.Is(err, ringbuffer.ErrIsEmpty) {
			p.logger.Warnf("writeLoop TryRead error: %v", err)
			continue
		}
		if n == 0 {
			continue // buffer empty
		}

		// Write collected bytes to PTY
		offset := 0
		for offset < n {
			written, err := p.pty.Write(buf[offset:n])
			if written > 0 {
				offset += written
				atomic.AddUint64(&p.writeBytes, uint64(written))
				p.logger.Debugf("[writeLoop] Wrote %d bytes to PTY master", written)
			}

			if err != nil {
				switch {
				case errors.Is(err, syscall.EINTR):
					continue
				case errors.Is(err, syscall.EAGAIN), errors.Is(err, syscall.EWOULDBLOCK):
					// Wait until writable again
					if _, pollErr := unix.Poll(pollFd, p.pollTimeoutMs); pollErr != nil && !errors.Is(pollErr, syscall.EINTR) {
						p.logger.Warnf("writeLoop poll error: %v", pollErr)
					}
					continue
				case errors.Is(err, syscall.EBADF):
					// FD closed — terminate loop (expected during Close())
					p.logger.Debug("writeLoop exiting: master FD closed")
					return
				default:
					// Critical error — notify caller and exit
					p.logger.Warnf("writeLoop exiting on error: %v", err)
					if p.onError != nil {
						p.writeErrorOnce.Do(func() {
							p.onError(fmt.Errorf("writeLoop critical error: %w", err))
						})
					}
					return
				}
			}
		}
	}
}

func (p *ringPTY) readLoop() {
	defer p.wg.Done()
	p.logger.Infof("[PTY readLoop] STARTING for slave %s", p.ttyName)

	fd := int(p.pty.Fd())
	pollFd := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	buf := make([]byte, 4096) // Read buffer for PTY reads

	for {
		select {
		case <-p.ctx.Done():
			return
		default:
		}

		// Wait for readable data or timeout
		nReady, err := unix.Poll(pollFd, p.pollTimeoutMs)
		if err != nil && !errors.Is(err, syscall.EINTR) {
			p.logger.Warnf("readLoop poll error: %v", err)
			continue
		}
		if nReady == 0 {
			continue // timeout, check context
		}

		n, err := p.pty.Read(buf)

		if n > 0 {
			// Write bytes to ring buffer (bulk operation)
			written, writeErr := p.readBuf.Write(buf[:n])
			if writeErr != nil && !errors.Is(writeErr, ringbuffer.ErrIsFull) {
				p.logger.Warnf("readLoop Write error: %v", writeErr)
				continue
			}

			// Track dropped bytes: the difference between what we tried to write and what was accepted
			// Note: smallnest/ringbuffer.Write() returns how many bytes were actually written
			if written < n {
				dropped := n - written
				atomic.AddUint64(&p.droppedRead, uint64(dropped))
			}

			atomic.AddUint64(&p.readBytes, uint64(written))
		}

		if err != nil {
			switch {
			case errors.Is(err, syscall.EAGAIN), errors.Is(err, syscall.EWOULDBLOCK):
				continue
			case errors.Is(err, syscall.EINTR):
				continue
			case errors.Is(err, syscall.EBADF):
				// FD closed — exit immediately (expected during Close())
				p.logger.Debug("readLoop exiting: master FD closed")
				return
			case errors.Is(err, io.EOF):
				// EOF means slave side closed (expected if external process exits)
				p.logger.Debug("readLoop exiting: EOF")
				return
			default:
				// Critical error — notify caller and exit
				p.logger.Warnf("readLoop exiting on error: %v", err)
				if p.onError != nil {
					p.readErrorOnce.Do(func() {
						p.onError(fmt.Errorf("readLoop critical error: %w", err))
					})
				}
				return
			}
		}
	}
}

// Write queues data for async sending to the PTY slave.
// This is a NON-BLOCKING write that always returns immediately.
//
// Behavior:
//   - Data bytes are enqueued to the ring buffer for background transmission
//   - If buffer is full, oldest bytes may be dropped (ring buffer semantics)
//
// Return values:
//   - (len(data), nil): Data successfully queued
//   - (0, io.ErrClosedPipe): PTY has been closed
//
// Note: Returning len(data) does NOT guarantee data was written to PTY,
// only that it was queued. Use Stats() to monitor actual write progress.
func (p *ringPTY) Write(data []byte) (int, error) {
	if atomic.LoadUint32(&p.closed) == 1 {
		return 0, io.ErrClosedPipe
	}
	if len(data) == 0 {
		return 0, nil
	}

	// Write bytes to ring buffer (bulk operation)
	written, err := p.writeBuf.Write(data)
	if err != nil && !errors.Is(err, ringbuffer.ErrIsFull) {
		p.logger.Warnf("Write error: %v", err)
		return 0, err
	}

	// Track dropped bytes: the difference between what we tried to write and what was accepted
	// Note: smallnest/ringbuffer.Write() returns how many bytes were actually written
	if written < len(data) {
		dropped := len(data) - written
		atomic.AddUint64(&p.droppedWrite, uint64(dropped))
	}

	// Note: writeBytes is counted in writeLoop() when actually written to PTY
	// We return len(data) to indicate all data was accepted (even if some was dropped)
	return len(data), nil
}

// Read implements io.Reader by reading up to len(b) bytes from the buffered input.
// This is a NON-BLOCKING read that returns immediately.
//
// Return values:
//   - (n, nil) where n > 0: Successfully read n bytes
//   - (0, syscall.EAGAIN): No data currently available (standard non-blocking I/O semantics)
//   - (0, io.ErrClosedPipe): PTY has been closed
//   - (0, nil): Only when len(b) == 0 (standard io.Reader contract)
//
// This implements io.Reader with non-blocking semantics. Callers should check for
// syscall.EAGAIN using errors.Is() and retry with poll/select/timer.
func (p *ringPTY) Read(b []byte) (n int, err error) {
	if atomic.LoadUint32(&p.closed) == 1 {
		return 0, io.ErrClosedPipe
	}
	if len(b) == 0 {
		return 0, nil
	}

	// Read bytes from the ring buffer (bulk operation)
	n, err = p.readBuf.TryRead(b)
	if err != nil && !errors.Is(err, ringbuffer.ErrIsEmpty) {
		p.logger.Warnf("Read TryRead error: %v", err)
		return 0, err
	}

	// If no data available, return EAGAIN (standard non-blocking I/O semantics)
	// This complies with io.Reader contract: (0, nil) only when len(b) == 0
	if n == 0 {
		return 0, syscall.EAGAIN
	}

	return n, nil
}

// Close shuts down goroutines and closes the master FD.
func (p *ringPTY) Close() error {
	if !atomic.CompareAndSwapUint32(&p.closed, 0, 1) {
		return nil
	}

	// 1. Cancel context to signal goroutines to exit
	p.cancel()

	// 2. Close FDs to unblock any I/O operations immediately with EBADF
	//    This ensures goroutines don't wait for poll timeouts (up to 50ms)
	//    CRITICAL: Must close FD even if Close() fails to prevent goroutine leak
	if p.pty != nil {
		// Capture FD BEFORE Close() - after Close() fails, Fd() may return invalid/undefined values
		fd := int(p.pty.Fd())
		if err := p.pty.Close(); err != nil {
			p.logger.Warnf("failed to close PTY(ptyx): %v, forcing FD close", err)
			// Force FD closure using pre-captured FD to ensure goroutines get EBADF
			_ = syscall.Close(fd)
		}
		p.pty = nil
	}

	if p.tty != nil {
		// Capture FD BEFORE Close() - after Close() fails, Fd() may return invalid/undefined values
		fd := int(p.tty.Fd())
		if err := p.tty.Close(); err != nil {
			p.logger.Warnf("failed to close PTY(tty): %v, forcing FD close", err)
			// Force FD closure using pre-captured FD to ensure goroutines get EBADF
			_ = syscall.Close(fd)
		}
		p.tty = nil
	}

	// 3. Wait for goroutines to exit cleanly
	//    They will exit via context cancellation or EBADF from closed FDs
	p.wg.Wait()

	// Note: Ring buffers don't need explicit Close() like channels do
	// The smallnest/ringbuffer library handles cleanup automatically

	return nil
}

// Stats returns instantaneous stats for monitoring.
func (p *ringPTY) Stats() Stats {
	return Stats{
		WriteQueueLen:     int32(p.writeBuf.Length()),
		WriteQueueCap:     int32(p.writeBuf.Capacity()),
		ReadQueueLen:      int32(p.readBuf.Length()),
		ReadQueueCap:      int32(p.readBuf.Capacity()),
		DroppedWriteCount: atomic.LoadUint64(&p.droppedWrite),
		DroppedReadCount:  atomic.LoadUint64(&p.droppedRead),
		ReadBytesTotal:    atomic.LoadUint64(&p.readBytes),
		WriteBytesTotal:   atomic.LoadUint64(&p.writeBytes),
	}
}

// TTYName returns the filesystem path to the slave (e.g., "/dev/pts/5")
// if known (only when created via NewPty).
func (p *ringPTY) TTYName() string {
	return p.ttyName
}

// createPTY creates a pseudo-terminal and configures it for raw mode.
func createPTY() (master *os.File, slave *os.File, err error) {
	master, slave, err = pty.Open()
	if err != nil {
		// Enhance an error message for common permission/resource issues
		return nil, nil, fmt.Errorf("failed to create PTY (check permissions and available PTY devices): %w", err)
	}

	// Set PTY slave to raw mode for proper terminal behavior
	if _, err := term.MakeRaw(int(slave.Fd())); err != nil {
		ptyPath := slave.Name()
		if closeErr := master.Close(); closeErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: failed to close PTY(ptyx) during cleanup: %v\n", closeErr)
		}
		if closeErr := slave.Close(); closeErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: failed to close PTY(tty) during cleanup: %v\n", closeErr)
		}
		return nil, nil, fmt.Errorf("failed to set PTY(tty) %s to raw mode: %w", ptyPath, err)
	}

	if err := syscall.SetNonblock(int(master.Fd()), true); err != nil {
		ptyPath := slave.Name()
		if closeErr := master.Close(); closeErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: failed to close PTY(ptyx) during cleanup: %v\n", closeErr)
		}
		if closeErr := slave.Close(); closeErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: failed to close PTY(tty) during cleanup: %v\n", closeErr)
		}
		return nil, nil, fmt.Errorf("failed to set PTY(ptyx) %s to nonblocking mode: %w", ptyPath, err)
	}

	return master, slave, nil
}
