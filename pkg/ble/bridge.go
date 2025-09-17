package ble

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/creack/pty"
	"github.com/sirupsen/logrus"
	"golang.org/x/term"
)

// Bridge represents a PTY bridge for BLE serial communication
type Bridge struct {
	ptyMaster   *os.File
	ptySlave    *os.File
	logger      *logrus.Logger
	writeFunc   func([]byte) error
	isRunning   bool
	runMutex    sync.RWMutex
	stopChan    chan struct{}
	stoppedChan chan struct{}
}

// BridgeOptions configures the PTY bridge
type BridgeOptions struct {
	PTYName    string // Optional: custom PTY name
	BufferSize int    // Buffer size for data transfer
}

// DefaultBridgeOptions returns sensible defaults for PTY bridge
func DefaultBridgeOptions() *BridgeOptions {
	return &BridgeOptions{
		PTYName:    "", // Let system assign
		BufferSize: 1024,
	}
}

// NewBridge creates a new PTY bridge
func NewBridge(logger *logrus.Logger) *Bridge {
	if logger == nil {
		logger = logrus.New()
	}

	return &Bridge{
		logger:      logger,
		isRunning:   false,
		stopChan:    make(chan struct{}),
		stoppedChan: make(chan struct{}),
	}
}

// Start creates and starts the PTY bridge
func (b *Bridge) Start(ctx context.Context, opts *BridgeOptions, writeFunc func([]byte) error) error {
	b.runMutex.Lock()
	defer b.runMutex.Unlock()

	if b.isRunning {
		return fmt.Errorf("bridge is already running")
	}

	if writeFunc == nil {
		return fmt.Errorf("write function is required")
	}

	b.writeFunc = writeFunc

	// Create PTY
	master, slave, err := pty.Open()
	if err != nil {
		return fmt.Errorf("failed to create PTY: %w", err)
	}

	b.ptyMaster = master
	b.ptySlave = slave

	// Get the PTY name
	ptyName := b.ptySlave.Name()
	b.logger.WithField("pty", ptyName).Info("Created PTY bridge")

	// Make the slave PTY raw (disable line buffering, echo, etc.)
	if _, err := term.MakeRaw(int(b.ptySlave.Fd())); err != nil {
		b.logger.WithError(err).Warn("Failed to set PTY to raw mode")
	}

	b.isRunning = true

	// Start the bridge goroutines
	go b.readFromPTY(ctx, opts.BufferSize)
	go b.monitorContext(ctx)

	b.logger.WithField("pty", ptyName).Info("PTY bridge started successfully")
	fmt.Printf("PTY bridge created at: %s\n", ptyName)
	fmt.Printf("Connect your application to this device file\n")

	return nil
}

// WriteToDevice writes data from PTY to the BLE device
func (b *Bridge) WriteToDevice(data []byte) error {
	b.runMutex.RLock()
	writeFunc := b.writeFunc
	running := b.isRunning
	b.runMutex.RUnlock()

	if !running {
		return fmt.Errorf("bridge is not running")
	}

	if writeFunc == nil {
		return fmt.Errorf("write function not set")
	}

	b.logger.WithField("bytes", len(data)).Debug("Writing data to BLE device")
	return writeFunc(data)
}

// WriteFromDevice writes data from BLE device to PTY
func (b *Bridge) WriteFromDevice(data []byte) error {
	b.runMutex.RLock()
	master := b.ptyMaster
	running := b.isRunning
	b.runMutex.RUnlock()

	if !running {
		return fmt.Errorf("bridge is not running")
	}

	if master == nil {
		return fmt.Errorf("PTY master not available")
	}

	b.logger.WithField("bytes", len(data)).Debug("Writing data to PTY")

	_, err := master.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write to PTY: %w", err)
	}

	return nil
}

// readFromPTY reads data from PTY and sends it to the BLE device
func (b *Bridge) readFromPTY(ctx context.Context, bufferSize int) {
	defer func() {
		b.stoppedChan <- struct{}{}
	}()

	buffer := make([]byte, bufferSize)

	for {
		select {
		case <-ctx.Done():
			b.logger.Debug("PTY read goroutine stopping due to context cancellation")
			return
		case <-b.stopChan:
			b.logger.Debug("PTY read goroutine stopping due to stop signal")
			return
		default:
			// Read from PTY master
			n, err := b.ptyMaster.Read(buffer)
			if err != nil {
				if err == io.EOF {
					b.logger.Debug("PTY closed")
					return
				}
				b.logger.WithError(err).Error("Error reading from PTY")
				continue
			}

			if n > 0 {
				data := make([]byte, n)
				copy(data, buffer[:n])

				// Send data to BLE device
				if err := b.WriteToDevice(data); err != nil {
					b.logger.WithError(err).Error("Failed to write data to BLE device")
				}
			}
		}
	}
}

// monitorContext monitors the context and stops the bridge when cancelled
func (b *Bridge) monitorContext(ctx context.Context) {
	<-ctx.Done()
	b.logger.Debug("Context cancelled, stopping PTY bridge")
	b.Stop()
}

// IsRunning returns whether the bridge is currently active
func (b *Bridge) IsRunning() bool {
	b.runMutex.RLock()
	defer b.runMutex.RUnlock()
	return b.isRunning
}

// GetPTYName returns the PTY device name
func (b *Bridge) GetPTYName() string {
	b.runMutex.RLock()
	defer b.runMutex.RUnlock()

	if b.ptySlave != nil {
		return b.ptySlave.Name()
	}
	return ""
}

// Stop stops the PTY bridge
func (b *Bridge) Stop() error {
	b.runMutex.Lock()
	defer b.runMutex.Unlock()

	if !b.isRunning {
		return fmt.Errorf("bridge is not running")
	}

	b.logger.Info("Stopping PTY bridge...")

	// Signal stop
	close(b.stopChan)

	// Wait for read goroutine to stop
	<-b.stoppedChan

	// Close PTY files
	if b.ptyMaster != nil {
		b.ptyMaster.Close()
		b.ptyMaster = nil
	}

	if b.ptySlave != nil {
		b.ptySlave.Close()
		b.ptySlave = nil
	}

	b.isRunning = false
	b.writeFunc = nil

	// Reset channels for potential restart
	b.stopChan = make(chan struct{})
	b.stoppedChan = make(chan struct{})

	b.logger.Info("PTY bridge stopped")
	return nil
}

// GetStats returns bridge statistics
func (b *Bridge) GetStats() map[string]interface{} {
	b.runMutex.RLock()
	defer b.runMutex.RUnlock()

	stats := map[string]interface{}{
		"running": b.isRunning,
	}

	if b.ptySlave != nil {
		stats["pty_name"] = b.ptySlave.Name()
	}

	return stats
}
