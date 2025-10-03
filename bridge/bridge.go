package bridge

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/device"
	"github.com/srg/blim/internal/lua"
	"golang.org/x/term"
)

// Bridge represents a running BLE-PTY bridge with access to the device and PTY
type Bridge interface {
	GetDevice() device.Device // Access to connected BLE device
	GetPTYMaster() *os.File   // PTY master for script I/O
	GetPTYName() string       // PTY device name for display
}

// BridgeOptions contains all the configuration for running a bridge
type BridgeOptions struct {
	Address        string                    // BLE device address
	ConnectTimeout time.Duration             // Connection timeout
	Services       []device.SubscribeOptions // Services to subscribe to
	Logger         *logrus.Logger            // Logger instance
}

// ProgressCallback is called when the bridge phase changes
type ProgressCallback func(phase string)

// BridgeCallback is executed with the running bridge (mirrors InspectCallback)
type BridgeCallback[R any] func(Bridge) (R, error)

// OLD BRIDGE2 API - DEPRECATED - Use RunDeviceBridge instead
// This code is commented out but kept for reference during migration
/*
// Bridge2 represents the Lua-based BLE asynchronous bridge
type Bridge2 struct {
	connectionOpts *device.ConnectOptions

	// Pty
	ptyMaster *os.File // Master end of the PTY: program reads/writes here
	ptySlave  *os.File // Slave end of the PTY: acts as the process's stdin/stdout/stderr (controlling TTY)

	logger *logrus.Logger

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Lua
	scriptFile      string // Lua script file to load
	script          string // Lua script content (alternative to file)
	luaApi          *lua.BLEAPI2
	outputCollector *lua.LuaOutputCollector
	errorHandler    ErrorHandler // Pluggable error handler for Lua stderr

	// State
	mutex     sync.RWMutex
	isRunning bool
}

// ErrorHandler is a callback function invoked when Lua scripts output to stderr
// Parameters: timestamp of the error, error message
// Default behavior: formats and writes to PTY slave with "[ERROR]" prefix
type ErrorHandler func(timestamp time.Time, message string)

// Bridge2Config represents the configuration for Bridge2
type Bridge2Config struct {
	Address      string         // BLE device address
	ScriptFile   string         // Path to a Lua script file
	Script       string         // Lua script content (alternative to file)
	Logger       *logrus.Logger // Logger instance
	ErrorHandler ErrorHandler   // Optional custom error handler (defaults to formatted PTY write)
}

// NewBridge creates a new Lua-based subscription bridge
func NewBridge(logger *logrus.Logger, config Bridge2Config) (*Bridge2, error) {
	if config.Address == "" {
		return nil, fmt.Errorf("BLE device address is required")
	}

	if logger == nil {
		logger = logrus.New()
	}

	return &Bridge2{
		logger:       logger,
		scriptFile:   config.ScriptFile,
		script:       config.Script,
		errorHandler: config.ErrorHandler, // Will be set to default in Start() if nil

		connectionOpts: &device.ConnectOptions{
			Address: config.Address,
		},
	}, nil
}

func (b *Bridge2) device() device.Device {
	if b.luaApi == nil {
		return nil
	}

	return b.luaApi.GetDevice()
}

// Start initializes and starts the Lua subscription bridge
func (b *Bridge2) Start(ctx context.Context, opts *device.ConnectOptions) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	if b.isRunning {
		return fmt.Errorf("bridge is already running")
	}

	b.logger.Info("Starting BLE bridge...")

	// Create context for cancellation
	b.ctx, b.cancel = context.WithCancel(ctx)

	// Setup cleanup on error - will be skipped if success is set to true
	var success bool
	defer func() {
		if !success {
			// Clean up all resources on error
			if b.outputCollector != nil {
				_ = b.outputCollector.Stop()
				b.outputCollector = nil
			}
			if b.luaApi != nil {
				b.luaApi.Close()
				b.luaApi = nil
			}
			if b.device() != nil && b.device().IsConnected() {
				_ = b.device().Disconnect()
			}
			if b.ptyMaster != nil {
				_ = b.ptyMaster.Close()
				b.ptyMaster = nil
			}
			if b.ptySlave != nil {
				_ = b.ptySlave.Close()
				b.ptySlave = nil
			}
			if b.cancel != nil {
				b.cancel()
			}
		}
	}()

	// Create Lua API
	if err := copier.CopyWithOption(&b.connectionOpts, opts, copier.Option{DeepCopy: true}); err != nil {
		return fmt.Errorf("failed merging BLE connection options: %w", err)
	}

	if la, err := lua.LuaApiFactory(b.connectionOpts.Address, b.logger); err != nil {
		return fmt.Errorf("could not connect to BLE device: %w", err)
	} else {
		b.luaApi = la
	}

	// Load Lua script
	if err := b.loadScript(); err != nil {
		return fmt.Errorf("failed to load Lua script: %w", err)
	}

	// Establish BLE connection
	if b.device().IsConnected() {
		// DEBUG: Device connection state for troubleshooting bridge creation failure
		b.logger.WithFields(logrus.Fields{
			"device_address": b.connectionOpts.Address,
			"is_connected":   b.device().IsConnected(),
		}).Debug("DEBUG: BLE connection already established during bridge start")
		return fmt.Errorf("BLE connection already established")
	}

	if err := b.device().Connect(ctx, opts); err != nil {
		return fmt.Errorf("failed to connect to BLE device: %w", err)
	}

	// Create pty
	master, slave, err := pty.Open()
	if err != nil {
		return fmt.Errorf("failed to create PTY device: %w", err)
	}

	b.ptyMaster = master
	b.ptySlave = slave

	// Set PTY slave to raw mode to disable echo and prevent the bridge from reading its own output
	if _, err := term.MakeRaw(int(slave.Fd())); err != nil {
		return fmt.Errorf("failed to set PTY to raw mode: %w", err)
	}

	ttyName := b.ptySlave.Name() // Use slave name for external apps
	b.logger.WithField("tty", ttyName).Info("Created TTY device for event-driven bridge")

	// Set default error handler if none provided
	if b.errorHandler == nil {
		b.errorHandler = b.defaultErrorHandler
	}

	// Create and start output collector
	const outputBufferSize = 1000 // Buffer up to 1000 output records
	collector, err := lua.NewLuaOutputCollector(
		b.luaApi.OutputChannel(),
		outputBufferSize,
		func(err error) {
			b.logger.WithError(err).Error("Lua output collector error")
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create output collector: %w", err)
	}
	b.outputCollector = collector

	if err := b.outputCollector.Start(); err != nil {
		return fmt.Errorf("failed to start output collector: %w", err)
	}
	b.logger.Info("Lua output collector started")

	// Execute the script to set up subscriptions
	if err := b.luaApi.ExecuteScript(b.ctx, ""); err != nil {
		return fmt.Errorf("failed to execute Lua script: %w", err)
	}

	b.isRunning = true

	// Start monitoring goroutine
	b.wg.Add(1)
	go b.monitor()

	b.logger.Info("BLE subscription bridge started successfully")
	b.connectionOpts = opts

	// Mark success to skip cleanup
	success = true
	return nil
}

// Stop stops the Lua subscription bridge
func (b *Bridge2) Stop() error {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	if !b.isRunning {
		return fmt.Errorf("bridge is not running")
	}

	b.logger.Info("Stopping BLE subscription bridge...")

	if b.device().IsConnected() {
		if err := b.device().Disconnect(); err != nil {
			b.logger.Warnf("Failed to disconnect from BLE device: %v", err)
		}
	}

	// Cancel context to stop all goroutines first
	if b.cancel != nil {
		b.cancel()
	}

	// Wait for goroutines to finish (including monitor which consumes output)
	b.wg.Wait()

	// Stop output collector and drain remaining output
	if b.outputCollector != nil {
		// Stop the collector
		if err := b.outputCollector.Stop(); err != nil {
			b.logger.WithError(err).Warn("Failed to stop output collector")
		}

		// Log final metrics for observability
		metrics := b.outputCollector.GetMetrics()
		b.logger.WithFields(logrus.Fields{
			"records_processed":   metrics.RecordsProcessed,
			"records_overwritten": metrics.RecordsOverwritten,
			"errors_occurred":     metrics.ErrorsOccurred,
		}).Info("Output collector final metrics")

		// Drain any remaining buffered output to PTY before closing
		if b.ptyMaster != nil {
			b.consumeAndWriteOutput()
		}

		b.outputCollector = nil
	}

	// Close PTY devices after draining output
	if b.ptyMaster != nil {
		if err := b.ptyMaster.Close(); err != nil {
			b.logger.Warnf("Failed to close PTY master: %v", err)
		}
		b.ptyMaster = nil
	}

	if b.ptySlave != nil {
		if err := b.ptySlave.Close(); err != nil {
			b.logger.Warnf("Failed to close PTY slave: %v", err)
		}
		b.ptySlave = nil
	}

	// Close the Lua engine explicitly
	if b.luaApi != nil {
		b.luaApi.Close()
		b.luaApi = nil
	}

	b.isRunning = false
	b.logger.Info("BLE bridge stopped")

	return nil
}

// IsRunning returns whether the bridge is currently running
func (b *Bridge2) IsRunning() bool {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	return b.isRunning
}

// loadScript loads the Lua script from a file or a string with validation
func (b *Bridge2) loadScript() error {
	const maxScriptSize = 1024 * 1024 // 1MB limit

	if b.scriptFile == "" && b.script == "" {
		return fmt.Errorf("script file or content is required")
	}

	if b.scriptFile != "" {
		// Validate file path (basic security check)
		if err := validateScriptPath(b.scriptFile); err != nil {
			return fmt.Errorf("invalid script file path: %w", err)
		}

		// Check file size before loading
		info, err := os.Stat(b.scriptFile)
		if err != nil {
			return fmt.Errorf("cannot access script file: %w", err)
		}

		if info.Size() > maxScriptSize {
			return fmt.Errorf("script file too large: %d bytes (max %d bytes)", info.Size(), maxScriptSize)
		}

		b.logger.WithField("file", b.scriptFile).Info("Loading Lua script from file")
		return b.luaApi.LoadScriptFile(b.scriptFile)
	} else {
		// Validate script content size
		if len(b.script) > maxScriptSize {
			return fmt.Errorf("script content too large: %d bytes (max %d bytes)", len(b.script), maxScriptSize)
		}

		if len(b.script) == 0 {
			return fmt.Errorf("script content is empty")
		}

		b.logger.Info("Loading Lua script from string")
		return b.luaApi.LoadScript(b.script, "inline")
	}
}

// validateScriptPath performs basic security validation on script file paths
func validateScriptPath(path string) error {
	// Check for an empty path
	if path == "" {
		return fmt.Errorf("path is empty")
	}

	// Verify the file exists and is readable
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	// Ensure it's a regular file, not a directory or special file
	if !info.Mode().IsRegular() {
		return fmt.Errorf("path is not a regular file")
	}

	return nil
}

// monitor runs the main monitoring loop
func (b *Bridge2) monitor() {
	defer b.wg.Done()

	b.logger.Info("Starting bridge monitor")

	healthTicker := time.NewTicker(5 * time.Second)
	defer healthTicker.Stop()

	// Output consumption ticker - check frequently for low latency
	outputTicker := time.NewTicker(100 * time.Millisecond)
	defer outputTicker.Stop()

	for {
		select {
		case <-b.ctx.Done():
			b.logger.Info("Bridge monitor stopping due to context cancellation")
			return

		case <-healthTicker.C:
			// Periodic health check
			if !b.device().IsConnected() {
				b.logger.Warn("BLE connection lost")
				// In a production system, you might want to attempt reconnection here
			}

		case <-outputTicker.C:
			// Consume and write Lua output to PTY slave
			b.consumeAndWriteOutput()
		}
	}
}

// consumeAndWriteOutput drains the output collector buffer and writes to PTY slave
// It uses the pluggable error handler for stderr records and writes stdout directly
func (b *Bridge2) consumeAndWriteOutput() {
	if b.outputCollector == nil || b.ptyMaster == nil {
		return
	}

	// Custom consumer that differentiates between stdout and stderr
	consumer := func(record *lua.LuaOutputRecord) (string, error) {
		if record == nil {
			// No more records
			return "", nil
		}

		if record.Source == "stderr" {
			// Use pluggable error handler for stderr
			if b.errorHandler != nil {
				b.errorHandler(record.Timestamp, record.Content)
			}
		} else {
			// Write stdout directly to PTY
			if len(record.Content) > 0 {
				_, err := b.ptyMaster.Write([]byte(record.Content))
				if err != nil {
					b.logger.WithError(err).Error("Failed to write Lua stdout to PTY")
				}
			}
		}

		return "", nil // Continue processing more records
	}

	// Consume all buffered records
	_, err := lua.ConsumeRecords(b.outputCollector, consumer)
	if err != nil {
		b.logger.WithError(err).Error("Failed to consume Lua output records")
	}
}

// SetScript updates the script content (can only be called when the bridge is stopped)
func (b *Bridge2) SetScript(script string) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	if b.isRunning {
		return fmt.Errorf("cannot change script while bridge is running")
	}

	b.script = script
	b.scriptFile = "" // Clear file path when using string content
	return nil
}

// GetPTYMaster returns the PTY master for testing purposes
func (b *Bridge2) GetPTYMaster() *os.File {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.ptyMaster
}

// GetPTYSlave returns the PTY slave for testing purposes
func (b *Bridge2) GetPTYSlave() *os.File {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.ptySlave
}

// SetScriptFile updates the script file path (can only be called when the bridge is stopped)
func (b *Bridge2) SetScriptFile(filename string) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	if b.isRunning {
		return fmt.Errorf("cannot change script file while bridge is running")
	}

	b.scriptFile = filename
	b.script = "" // Clear string content when using file
	return nil
}

// ReloadScript reloads and re-executes the current script
// This can be used to update subscriptions without restarting the bridge
func (b *Bridge2) ReloadScript() error {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	if !b.isRunning || b.luaApi == nil {
		return fmt.Errorf("bridge is not running")
	}

	b.logger.Info("Reloading Lua script")

	// Reset the engine to clear existing subscriptions
	b.luaApi.Reset()

	// Reload the script
	if err := b.loadScript(); err != nil {
		return fmt.Errorf("failed to reload script: %w", err)
	}

	// Re-execute to set up new subscriptions
	if err := b.luaApi.ExecuteScript(b.ctx, ""); err != nil {
		return fmt.Errorf("failed to re-execute script: %w", err)
	}

	b.logger.Info("Lua script reloaded successfully")
	return nil
}

func (b *Bridge2) GetLuaApi() *lua.BLEAPI2 {
	return b.luaApi
}

// GetPTYName returns the TTY device name
func (b *Bridge2) GetPTYName() string {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	if b.ptySlave != nil {
		return b.ptySlave.Name()
	}

	return ""
}

func (b *Bridge2) GetServices() map[string]device.Service {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	return b.device().GetConnection().GetServices()
}

// defaultErrorHandler is the default implementation for handling Lua stderr output
// It formats errors with timestamp and "[ERROR]" prefix, logs them, and writes to PTY
func (b *Bridge2) defaultErrorHandler(timestamp time.Time, message string) {
	// Format error message with timestamp
	formatted := fmt.Sprintf("[ERROR %s] %s", timestamp.Format("15:04:05.000"), message)

	// Log to application logger
	b.logger.Error(message)

	// Write formatted error to PTY for external software
	if b.ptyMaster != nil {
		_, err := b.ptyMaster.Write([]byte(formatted))
		if err != nil {
			b.logger.WithError(err).Warn("Failed to write error to PTY")
		}
	}
}
*/
// END OF DEPRECATED BRIDGE2 CODE

// bridgeImpl implements the Bridge interface
type bridgeImpl struct {
	device    device.Device
	ptyMaster *os.File
	ptySlave  *os.File
}

func (b *bridgeImpl) GetDevice() device.Device {
	return b.device
}

func (b *bridgeImpl) GetPTYMaster() *os.File {
	return b.ptyMaster
}

func (b *bridgeImpl) GetPTYName() string {
	if b.ptySlave != nil {
		return b.ptySlave.Name()
	}
	return ""
}

// RunDeviceBridge connects to a BLE device, creates a PTY bridge, and executes the callback with the bridge.
// This function blocks until the context is canceled or an error occurs.
// It follows the same pattern as inspector.InspectDevice for consistency.
func RunDeviceBridge[R any](
	ctx context.Context,
	opts *BridgeOptions,
	progressCallback ProgressCallback,
	callback BridgeCallback[R],
) (R, error) {
	var zero R

	// Validate options
	if opts == nil {
		return zero, fmt.Errorf("bridge options are required")
	}
	if opts.Address == "" {
		return zero, fmt.Errorf("device address is required")
	}

	// Set defaults
	logger := opts.Logger
	if logger == nil {
		logger = logrus.New()
	}
	if progressCallback == nil {
		progressCallback = func(string) {} // No-op callback
	}
	if opts.ConnectTimeout == 0 {
		opts.ConnectTimeout = 30 * time.Second
	}

	// Create context for cancellation
	bridgeCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Setup cleanup on error
	var (
		luaApi          *lua.BLEAPI2
		ptyMaster       *os.File
		ptySlave        *os.File
		outputCollector *lua.LuaOutputCollector
		monitorWg       sync.WaitGroup
	)

	defer func() {
		// Stop monitor goroutine
		cancel()
		monitorWg.Wait()

		// Cleanup resources
		if outputCollector != nil {
			_ = outputCollector.Stop()
		}
		if luaApi != nil {
			if luaApi.GetDevice() != nil && luaApi.GetDevice().IsConnected() {
				_ = luaApi.GetDevice().Disconnect()
			}
			luaApi.Close()
		}
		if ptyMaster != nil {
			_ = ptyMaster.Close()
		}
		if ptySlave != nil {
			_ = ptySlave.Close()
		}
	}()

	// Report phase: Connecting
	progressCallback("Connecting")

	// Create Lua API (creates device)
	api, err := lua.LuaApiFactory(opts.Address, logger)
	if err != nil {
		return zero, fmt.Errorf("failed to create Lua API: %w", err)
	}
	luaApi = api

	// Connect to device
	connectOpts := &device.ConnectOptions{
		Address:        opts.Address,
		ConnectTimeout: opts.ConnectTimeout,
		Services:       opts.Services,
	}

	if err := luaApi.GetDevice().Connect(bridgeCtx, connectOpts); err != nil {
		return zero, fmt.Errorf("failed to connect to device: %w", err)
	}

	// Report phase: Connected
	progressCallback("Connected")

	// Report phase: Setting up PTY
	progressCallback("Setting up PTY")

	// Create PTY
	master, slave, err := pty.Open()
	if err != nil {
		return zero, fmt.Errorf("failed to create PTY: %w", err)
	}
	ptyMaster = master
	ptySlave = slave

	// Set PTY slave to raw mode
	if _, err := term.MakeRaw(int(slave.Fd())); err != nil {
		return zero, fmt.Errorf("failed to set PTY to raw mode: %w", err)
	}

	logger.WithField("tty", ptySlave.Name()).Info("Created PTY device")

	// Create output collector
	const outputBufferSize = 1000
	collector, err := lua.NewLuaOutputCollector(
		luaApi.OutputChannel(),
		outputBufferSize,
		func(err error) {
			logger.WithError(err).Error("Lua output collector error")
		},
	)
	if err != nil {
		return zero, fmt.Errorf("failed to create output collector: %w", err)
	}
	outputCollector = collector

	if err := outputCollector.Start(); err != nil {
		return zero, fmt.Errorf("failed to start output collector: %w", err)
	}

	// Start monitor goroutine to consume Lua output
	monitorWg.Add(1)
	go func() {
		defer monitorWg.Done()
		monitorBridgeOutput(bridgeCtx, outputCollector, ptyMaster, logger)
	}()

	// Report phase: Running
	progressCallback("Running")

	// Create bridge implementation
	bridge := &bridgeImpl{
		device:    luaApi.GetDevice(),
		ptyMaster: ptyMaster,
		ptySlave:  ptySlave,
	}

	// Execute callback with the bridge
	return callback(bridge)
}

// monitorBridgeOutput monitors and writes Lua output to PTY
func monitorBridgeOutput(ctx context.Context, collector *lua.LuaOutputCollector, ptyMaster *os.File, logger *logrus.Logger) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Final drain
			consumeBridgeOutput(collector, ptyMaster, logger)
			return
		case <-ticker.C:
			consumeBridgeOutput(collector, ptyMaster, logger)
		}
	}
}

// consumeBridgeOutput drains output collector and writes to PTY
func consumeBridgeOutput(collector *lua.LuaOutputCollector, ptyMaster *os.File, logger *logrus.Logger) {
	if collector == nil || ptyMaster == nil {
		return
	}

	consumer := func(record *lua.LuaOutputRecord) (string, error) {
		if record == nil {
			return "", nil
		}

		// Write both stdout and stderr to PTY (stderr with prefix)
		var content string
		if record.Source == "stderr" {
			content = fmt.Sprintf("[ERROR %s] %s", record.Timestamp.Format("15:04:05.000"), record.Content)
		} else {
			content = record.Content
		}

		if len(content) > 0 {
			if _, err := ptyMaster.Write([]byte(content)); err != nil {
				logger.WithError(err).Error("Failed to write to PTY")
			}
		}

		return "", nil
	}

	if _, err := lua.ConsumeRecords(collector, consumer); err != nil {
		logger.WithError(err).Error("Failed to consume output records")
	}
}
