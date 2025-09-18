package ble

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/go-ble/ble"
	"github.com/sirupsen/logrus"
	"github.com/srg/blecli/pkg/ble/internal"
)

// Bridge represents the Lua-based bidirectional BLE↔TTY transformation engine
// This implements the "dumb Go engine" + "smart Lua scripts" architecture
type Bridge struct {
	engine      *internal.LuaEngine
	ttyDevice   *os.File // Simple TTY device - no master/slave
	logger      *logrus.Logger
	isRunning   bool
	runMutex    sync.RWMutex
	stopChan    chan struct{}
	stoppedChan chan struct{}

	// BLE connection callbacks
	onBLEWrite func(uuid string, data []byte) error

	// Configuration
	transformInterval time.Duration
}

// BridgeOptions configures the bridge
type BridgeOptions struct {
	PTYName           string        // Optional: custom PTY name
	BufferSize        int           // Buffer size for data transfer
	TransformInterval time.Duration // How often to run transformations
	BLEToTTYScript    string        // Lua script for BLE→TTY transformation
	TTYToBLEScript    string        // Lua script for TTY→BLE transformation
}

// DefaultBridgeOptions returns sensible defaults for bridge
func DefaultBridgeOptions() *BridgeOptions {
	return &BridgeOptions{
		PTYName:           "", // Let system assign
		BufferSize:        1024,
		TransformInterval: 50 * time.Millisecond, // 20Hz transformation rate
		BLEToTTYScript:    defaultBLEToTTYScript(),
		TTYToBLEScript:    defaultTTYToBLEScript(),
	}
}

// NewBridge creates a new Lua-based bridge
func NewBridge(logger *logrus.Logger) *Bridge {
	if logger == nil {
		logger = logrus.New()
	}

	return &Bridge{
		engine:            internal.NewLuaEngine(logger),
		logger:            logger,
		isRunning:         false,
		stopChan:          make(chan struct{}),
		stoppedChan:       make(chan struct{}),
		transformInterval: 50 * time.Millisecond,
	}
}

// SetBLEWriteCallback sets the callback for writing to BLE characteristics
func (b *Bridge) SetBLEWriteCallback(callback func(uuid string, data []byte) error) {
	b.onBLEWrite = callback
}

// AddBLECharacteristic adds a BLE characteristic to the bridge
func (b *Bridge) AddBLECharacteristic(char *ble.Characteristic) {
	b.engine.GetBLEAPI().AddCharacteristic(char)
	b.logger.WithField("uuid", char.UUID.String()).Debug("Added BLE characteristic to bridge")
}

// UpdateCharacteristic updates a characteristic value (raw data from BLE device)
func (b *Bridge) UpdateCharacteristic(uuid string, value []byte) {
	b.engine.GetBLEAPI().UpdateCharacteristicValue(uuid, value)
	b.logger.WithFields(logrus.Fields{
		"uuid":  uuid,
		"bytes": len(value),
	}).Debug("Updated characteristic value in BLE API")
}

// Start creates and starts the Lua bridge
func (b *Bridge) Start(ctx context.Context, opts *BridgeOptions) error {
	b.runMutex.Lock()
	defer b.runMutex.Unlock()

	if b.isRunning {
		return fmt.Errorf("bridge is already running")
	}

	// Set a transformation interval
	b.transformInterval = opts.TransformInterval

	// Load Lua scripts
	b.engine.SetScripts(opts.BLEToTTYScript, opts.TTYToBLEScript)

	if err := b.engine.LoadScript(opts.BLEToTTYScript, "BLE→TTY"); err != nil {
		return fmt.Errorf("failed to load BLE→TTY script: %w", err)
	}

	if err := b.engine.LoadScript(opts.TTYToBLEScript, "TTY→BLE"); err != nil {
		return fmt.Errorf("failed to load TTY→BLE script: %w", err)
	}

	// Create a simple TTY device (no master/slave complexity)
	_, slave, err := pty.Open()
	if err != nil {
		return fmt.Errorf("failed to create TTY device: %w", err)
	}

	b.ttyDevice = slave
	ttyName := b.ttyDevice.Name()
	b.logger.WithField("tty", ttyName).Info("Created TTY device for Lua bridge")

	b.isRunning = true

	// Start the engine goroutines
	go b.runTransformationEngine(ctx)
	go b.handleTTYIO(ctx, opts.BufferSize)
	go b.monitorContext(ctx)

	b.logger.WithField("tty", ttyName).Info("Lua bridge started successfully")

	return nil
}

// runTransformationEngine runs the bidirectional transformation loop
// This is the "dumb Go engine" that calls Lua transformations
func (b *Bridge) runTransformationEngine(ctx context.Context) {
	defer func() {
		b.stoppedChan <- struct{}{}
	}()

	ticker := time.NewTicker(b.transformInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			b.logger.Debug("Transformation engine stopping due to context cancellation")
			return
		case <-b.stopChan:
			b.logger.Debug("Transformation engine stopping due to stop signal")
			return
		case <-ticker.C:
			// BLE→TTY: Raw BLE data → Lua script → TTY buffer
			if err := b.engine.TransformBLEToTTY(); err != nil {
				b.handleTransformError(err, "BLE→TTY")
			} else {
				// Flush TTY buffer to TTY device
				b.flushTTYBufferToDevice()
			}

			// TTY→BLE: TTY buffer → Lua script → BLE characteristics
			if err := b.engine.TransformTTYToBLE(); err != nil {
				b.handleTransformError(err, "TTY→BLE")
			} else {
				// Flush BLE updates to BLE device
				b.flushBLEUpdates()
			}
		}
	}
}

// handleTTYIO handles reading/writing to the TTY device
// This feeds raw TTY data into the TTY buffer for Lua processing
func (b *Bridge) handleTTYIO(ctx context.Context, bufferSize int) {
	defer func() {
		b.stoppedChan <- struct{}{}
	}()

	buffer := make([]byte, bufferSize)

	for {
		select {
		case <-ctx.Done():
			b.logger.Debug("TTY I/O goroutine stopping due to context cancellation")
			return
		case <-b.stopChan:
			b.logger.Debug("TTY I/O goroutine stopping due to stop signal")
			return
		default:
			// Set read timeout to make it non-blocking
			b.ttyDevice.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

			// Read raw data from TTY device
			n, err := b.ttyDevice.Read(buffer)
			if err != nil {
				if os.IsTimeout(err) {
					// Timeout is normal, continue loop
					continue
				}
				b.logger.WithError(err).Error("Error reading from TTY device")
				continue
			}

			if n > 0 {
				data := make([]byte, n)
				copy(data, buffer[:n])

				// Feed raw TTY data into buffer for Lua processing
				b.engine.GetBLEBuffer().Append(data)

				b.logger.WithField("bytes", n).Debug("Read raw data from TTY into buffer")
			}
		}
	}
}

// flushTTYBufferToDevice writes data from TTY buffer to TTY device
// This sends Lua-transformed data out to the TTY
func (b *Bridge) flushTTYBufferToDevice() {
	ttyBuffer := b.engine.GetTTYBuffer()
	if ttyBuffer.Len() == 0 {
		return
	}

	// Read all data from TTY buffer (Lua put it there)
	data := ttyBuffer.Read(ttyBuffer.Len())
	if len(data) == 0 {
		return
	}

	b.runMutex.RLock()
	device := b.ttyDevice
	running := b.isRunning
	b.runMutex.RUnlock()

	if !running || device == nil {
		return
	}

	// Write to TTY device
	if _, err := device.Write(data); err != nil {
		b.logger.WithError(err).Error("Failed to write to TTY device")
	} else {
		b.logger.WithField("bytes", len(data)).Debug("Wrote Lua-transformed data to TTY")
	}
}

// flushBLEUpdates writes Lua-updated BLE characteristic values to BLE device
func (b *Bridge) flushBLEUpdates() {
	if b.onBLEWrite == nil {
		return
	}

	bleAPI := b.engine.GetBLEAPI()

	// Check each characteristic for Lua-updated values
	for _, uuid := range bleAPI.ListCharacteristics() {
		value := bleAPI.GetCharacteristicValue(uuid)
		if len(value) > 0 {
			if err := b.onBLEWrite(uuid, value); err != nil {
				b.logger.WithError(err).WithField("uuid", uuid).Error("Failed to write Lua-updated value to BLE")
			} else {
				b.logger.WithFields(logrus.Fields{
					"uuid":  uuid,
					"bytes": len(value),
				}).Debug("Wrote Lua-transformed data to BLE characteristic")

				// Clear the value after successful write
				bleAPI.SetCharacteristicValue(uuid, nil)
			}
		}
	}
}

// handleTransformError handles errors from Lua transformations
func (b *Bridge) handleTransformError(err error, direction string) {
	if engineErr, ok := err.(*internal.EngineError); ok {
		if engineErr.Fatal {
			b.logger.WithError(engineErr).Error("Fatal transformation error - stopping bridge")
			b.Stop() // Stop the bridge on fatal errors
		} else {
			b.logger.WithError(engineErr).Debug("Non-fatal transformation error")
		}
	} else {
		b.logger.WithError(err).Error("Transformation error")
	}
}

// monitorContext monitors the context and stops the bridge when cancelled
func (b *Bridge) monitorContext(ctx context.Context) {
	<-ctx.Done()
	b.logger.Debug("Context cancelled, stopping Lua bridge")
	b.Stop()
}

// IsRunning returns whether the bridge is currently active
func (b *Bridge) IsRunning() bool {
	b.runMutex.RLock()
	defer b.runMutex.RUnlock()
	return b.isRunning
}

// GetPTYName returns the TTY device name
func (b *Bridge) GetPTYName() string {
	b.runMutex.RLock()
	defer b.runMutex.RUnlock()

	if b.ttyDevice != nil {
		return b.ttyDevice.Name()
	}
	return ""
}

// Stop stops the Lua bridge
func (b *Bridge) Stop() error {
	b.runMutex.Lock()

	if !b.isRunning {
		b.runMutex.Unlock()
		return fmt.Errorf("bridge is not running")
	}

	b.logger.Info("Stopping Lua bridge...")

	// Signal stop
	close(b.stopChan)

	// Release lock before waiting to avoid deadlock
	b.runMutex.Unlock()

	// Wait for goroutines to stop
	<-b.stoppedChan // transformation engine
	<-b.stoppedChan // TTY I/O

	// Re-acquire lock to finish cleanup
	b.runMutex.Lock()
	defer b.runMutex.Unlock()

	// Close TTY device
	if b.ttyDevice != nil {
		b.ttyDevice.Close()
		b.ttyDevice = nil
	}

	// Close Lua engine
	b.engine.Close()

	b.isRunning = false

	// Reset channels for potential restart
	b.stopChan = make(chan struct{})
	b.stoppedChan = make(chan struct{})

	b.logger.Info("Lua bridge stopped")
	return nil
}

// GetEngine returns the Lua engine for external access
func (b *Bridge) GetEngine() *internal.LuaEngine {
	return b.engine
}

// GetStats returns bridge statistics
func (b *Bridge) GetStats() map[string]interface{} {
	b.runMutex.RLock()
	defer b.runMutex.RUnlock()

	stats := map[string]interface{}{
		"running":            b.isRunning,
		"transform_interval": b.transformInterval.String(),
		"tty_buffer_len":     b.engine.GetTTYBuffer().Len(),
		"ble_buffer_len":     b.engine.GetBLEBuffer().Len(),
		"characteristics":    len(b.engine.GetBLEAPI().ListCharacteristics()),
	}

	if b.ttyDevice != nil {
		stats["tty_name"] = b.ttyDevice.Name()
	}

	return stats
}

// Default Lua scripts for basic functionality

func defaultBLEToTTYScript() string {
	return `
-- BLE→TTY transformation script
-- Read characteristic value and append to TTY buffer

local temp_val = ble["2A6E"] -- Temperature characteristic
if temp_val and #temp_val > 0 then
    -- Transform to TTY bytes (example: pack as big-endian short)
    local tty_bytes = string.pack(">h", string.unpack("f", temp_val))
    buffer:append(tty_bytes)
end
`
}

func defaultTTYToBLEScript() string {
	return `
-- TTY→BLE transformation script
-- Read from buffer and update BLE characteristic

-- Peek buffer to see if enough data
local chunk = buffer:peek(2)
if #chunk < 2 then
    return nil, "waiting for more data"
end

-- Decode value (example: unpack big-endian short)
local val = string.unpack(">h", chunk)
ble["2A6E"] = string.pack("f", val) -- Update temperature characteristic
buffer:consume(2)
`
}
