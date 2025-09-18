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
	engine            *internal.LuaEngine
	ptyMaster         *os.File
	ptySlave          *os.File
	logger            *logrus.Logger
	isRunning         bool
	runMutex          sync.RWMutex
	stopChan          chan struct{}
	stoppedChan       chan struct{}
	onBLEWrite        func(uuid string, data []byte) error
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
		stoppedChan:       make(chan struct{}, 2),
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
	// Use level 6 (trace-like) to avoid spam in debug logs
	if b.logger.Level >= logrus.TraceLevel {
		b.logger.WithFields(logrus.Fields{
			"uuid":  uuid,
			"bytes": len(value),
		}).Trace("Updated characteristic value in BLE API")
	}
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

	master, slave, err := pty.Open()
	if err != nil {
		return fmt.Errorf("failed to create PTY device: %w", err)
	}

	b.ptyMaster = master
	b.ptySlave = slave
	ttyName := b.ptyMaster.Name()
	b.logger.WithField("tty", ttyName).Info("Created TTY device for Lua bridge")

	b.isRunning = true

	go b.runTransformationEngine(ctx)
	go b.handleTTYIO(ctx, opts.BufferSize)

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
			b.logger.Debug("=== TRANSFORM CYCLE START ===")
			// BLE→TTY: Raw BLE data → Lua script → TTY buffer
			b.logger.Debug("Starting BLE→TTY transform")
			if err := b.engine.TransformBLEToTTY(); err != nil {
				b.handleTransformError(err, "BLE→TTY")
			} else {
				b.logger.Debug("BLE→TTY transform completed, flushing to TTY")
				b.flushTTYBufferToDevice()
				b.logger.Debug("TTY flush completed")
			}

			// TTY→BLE: TTY buffer → Lua script → BLE characteristics
			b.logger.Debug("Starting TTY→BLE transform")
			if err := b.engine.TransformTTYToBLE(); err != nil {
				b.handleTransformError(err, "TTY→BLE")
			} else {
				b.logger.Debug("TTY→BLE transform completed, flushing to BLE")
				// Flush BLE updates to BLE device
				b.flushBLEUpdates()
				b.logger.Debug("BLE flush completed")
			}
			b.logger.Debug("=== TRANSFORM CYCLE END ===")
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
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			b.logger.Debug("TTY I/O stopping")
			return
		case <-b.stopChan:
			b.logger.Debug("TTY I/O stopping")
			return
		case <-ticker.C:
			b.ptySlave.SetReadDeadline(time.Now().Add(10 * time.Millisecond))

			n, err := b.ptySlave.Read(buffer)
			if err != nil {
				if os.IsTimeout(err) {
					continue
				}
				if err.Error() != "EOF" {
					b.logger.WithError(err).Debug("PTY read error")
				}
				continue
			}

			if n > 0 {
				data := make([]byte, n)
				copy(data, buffer[:n])
				b.engine.GetBLEBuffer().Append(data)
				b.logger.WithField("bytes", n).Debug("Read data from PTY")
			}
		}
	}
}

// flushTTYBufferToDevice writes data from TTY buffer to TTY device
// This sends Lua-transformed data out to the TTY
func (b *Bridge) flushTTYBufferToDevice() {
	b.logger.Debug("=== TTY FLUSH START ===")
	ttyBuffer := b.engine.GetTTYBuffer()
	if ttyBuffer.Len() == 0 {
		b.logger.Debug("TTY buffer empty, skipping flush")
		return
	}

	data := ttyBuffer.Read(ttyBuffer.Len())
	if len(data) == 0 {
		b.logger.Debug("No data to flush")
		return
	}

	b.runMutex.RLock()
	device := b.ptySlave
	running := b.isRunning
	b.runMutex.RUnlock()

	if !running {
		b.logger.Debug("Bridge not running, skipping PTY write")
		return
	}
	if device == nil {
		b.logger.Debug("PTY device is nil, skipping write")
		return
	}

	b.logger.WithField("bytes", len(data)).Debug("About to write to PTY")
	if _, err := device.Write(data); err != nil {
		b.logger.WithError(err).Debug("Failed to write to PTY")
	} else {
		b.logger.WithField("bytes", len(data)).Debug("Wrote data to PTY")
	}
	b.logger.Debug("=== TTY FLUSH END ===")
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

	if b.ptyMaster != nil {
		return b.ptyMaster.Name()
	}
	return ""
}

// Stop stops the Lua bridge
func (b *Bridge) Stop() error {
	b.runMutex.Lock()

	if !b.isRunning {
		b.runMutex.Unlock()
		return nil
	}

	b.logger.Info("Stopping Lua bridge...")

	// Signal stop
	close(b.stopChan)

	// Close PTY devices first to unblock any pending writes
	// This prevents deadlock: transformation engine may be blocked on device.Write()
	// and can't respond to stop signal. Closing the file descriptors makes the
	// blocked write fail immediately, allowing goroutines to exit cleanly.
	if b.ptyMaster != nil {
		b.ptyMaster.Close()
		b.ptyMaster = nil
	}
	if b.ptySlave != nil {
		b.ptySlave.Close()
		b.ptySlave = nil
	}

	// Release lock before waiting to avoid deadlock
	b.runMutex.Unlock()

	// Wait for goroutines to stop (now that PTY is closed, writes will fail)
	<-b.stoppedChan
	<-b.stoppedChan

	// Re-acquire lock to finish cleanup
	b.runMutex.Lock()
	defer b.runMutex.Unlock()

	// Close Lua engine
	b.engine.Close()

	b.isRunning = false

	b.stopChan = make(chan struct{})
	b.stoppedChan = make(chan struct{}, 2)

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

	if b.ptyMaster != nil {
		stats["pty_name"] = b.ptyMaster.Name()
	}

	return stats
}

// Default Lua scripts for basic functionality

func defaultBLEToTTYScript() string {
	return `
-- BLE→TTY transformation function - prints all characteristics in human-readable format
function ble_to_tty()
    local chars = ble:list()
    if #chars == 0 then
        return nil, "no characteristics available"
    end

    local output = "\n=== BLE Characteristics ==="
    for i, uuid in ipairs(chars) do
        local char = ble.get(uuid)
        if char then
            output = output .. "\n[" .. i .. "] " .. uuid .. ":"

            -- Show properties
            local props = {}
            if char.properties then
                for prop, enabled in pairs(char.properties) do
                    if enabled then
                        table.insert(props, prop)
                    end
                end
            end
            if #props > 0 then
                output = output .. " (" .. table.concat(props, ", ") .. ")"
            end

            -- Show value in hex and ASCII
            if char.value and #char.value > 0 then
                local hex = ""
                local ascii = ""
                for j = 1, #char.value do
                    local byte = string.byte(char.value, j)
                    hex = hex .. string.format("%02x ", byte)
                    ascii = ascii .. (byte >= 32 and byte <= 126 and string.char(byte) or ".")
                end
                output = output .. "\n    Value: " .. hex .. "(" .. ascii .. ")"
            else
                output = output .. "\n    Value: (empty)"
            end

            -- Show descriptors if any
            if char.descriptors then
                local desc_count = 0
                for _ in pairs(char.descriptors) do desc_count = desc_count + 1 end
                if desc_count > 0 then
                    output = output .. "\n    Descriptors: " .. desc_count
                end
            end
        end
    end
    output = output .. "\n========================\n"

    buffer:append(output)
end
`
}

func defaultTTYToBLEScript() string {
	return `
-- TTY→BLE transformation function - echoes TTY input as hex to first writable characteristic
function tty_to_ble()
    local chunk = buffer:peek(1)
    if #chunk < 1 then
        return nil, "waiting for more data"
    end

    -- Find first writable characteristic
    local chars = ble:list()
    local target_uuid = nil

    for i, uuid in ipairs(chars) do
        local char = ble.get(uuid)
        if char and char.properties and (char.properties.write or char.properties.write_without_response) then
            target_uuid = uuid
            break
        end
    end

    if target_uuid then
        -- Convert input to hex representation and write to BLE
        local byte = string.byte(chunk, 1)
        local hex_str = string.format("TTY->BLE: 0x%02x (%s)\n", byte,
            (byte >= 32 and byte <= 126) and string.char(byte) or ".")
        ble.set(target_uuid, hex_str)
        buffer:consume(1)
    else
        return nil, "no writable characteristics found"
    end
end
`
}
