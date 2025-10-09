package bridge

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/creack/pty"
	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/device"
	"github.com/srg/blim/internal/lua"
	"golang.org/x/term"
)

const (
	// DefaultOutputBufferSize is the default buffer size for Lua output collection
	// Increase for high-throughput scripts, decrease for memory-constrained environments
	DefaultOutputBufferSize = 1000
)

// Bridge represents a running BLE-PTY bridge with access to the device and PTY
type Bridge interface {
	GetLuaAPI() *lua.BLEAPI2
	GetPTYMaster() *os.File // PTY master for script I/O
	GetPTYName() string     // PTY device name for display
	GetSymlinkPath() string // Symlink path (empty if not created)
}

// BridgeOptions contains all the configuration for running a bridge
type BridgeOptions struct {
	Address          string                    // BLE device address
	ConnectTimeout   time.Duration             // Connection timeout
	Services         []device.SubscribeOptions // Services to subscribe to
	Logger           *logrus.Logger            // Logger instance
	OutputBufferSize int                       // Lua output buffer size (0 = use default)
	SymlinkPath      string                    // Optional symlink path for PTY slave (e.g., /tmp/ble-device)
}

// ProgressCallback is called when the bridge phase changes
type ProgressCallback func(phase string)

// BridgeCallback is executed with the running bridge (mirrors InspectCallback)
type BridgeCallback[R any] func(Bridge) (R, error)

// bridgeImpl implements the Bridge interface
type bridgeImpl struct {
	luaApi      *lua.BLEAPI2
	ptyMaster   *os.File
	ptySlave    *os.File
	symlinkPath string // Symlink to PTY slave (empty if not created)
}

func (b *bridgeImpl) GetLuaAPI() *lua.BLEAPI2 {
	return b.luaApi
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

func (b *bridgeImpl) GetSymlinkPath() string {
	return b.symlinkPath
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
		return zero, fmt.Errorf("failed to execute bridge: options are required")
	}
	if opts.Address == "" {
		return zero, fmt.Errorf("failed to execute bridge: device address is required")
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
	outputBufferSize := opts.OutputBufferSize
	if outputBufferSize == 0 {
		outputBufferSize = DefaultOutputBufferSize
	}

	// Create context for cancellation
	bridgeCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Setup cleanup on error
	var (
		luaApi      *lua.BLEAPI2
		ptyMaster   *os.File
		ptySlave    *os.File
		symlinkPath string
		//outputCollector *lua.LuaOutputCollector
		//monitorWg       sync.WaitGroup
	)

	defer func() {
		//// Stop monitor goroutine
		//cancel()
		//monitorWg.Wait()

		//// Cleanup resources
		//if outputCollector != nil {
		//	_ = outputCollector.Stop()
		//}

		// Remove symlink before closing PTY (cleanup order matters)
		if symlinkPath != "" {
			if err := os.Remove(symlinkPath); err != nil {
				logger.WithError(err).WithField("symlink", symlinkPath).Warn("Failed to remove symlink")
			} else {
				logger.WithField("symlink", symlinkPath).Debug("Removed symlink")
			}
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
	dev := device.NewDeviceWithAddress(opts.Address, logger)
	luaApi = lua.NewBLEAPI2(dev, logger)

	// Connect to device
	connectOpts := &device.ConnectOptions{
		Address:        opts.Address,
		ConnectTimeout: opts.ConnectTimeout,
		Services:       opts.Services,
	}

	if err := luaApi.GetDevice().Connect(bridgeCtx, connectOpts); err != nil {
		progressCallback("Failed")
		return zero, fmt.Errorf("failed to connect to device %s: %w", opts.Address, err)
	}

	// Report phase: Connected
	progressCallback("Connected")

	// Report phase: Setting up PTY
	progressCallback("Setting up PTY")

	// Create and configure PTY
	// Note: createPTY() handles cleanup on error, closing any opened file descriptors
	master, slave, err := createPTY()
	if err != nil {
		return zero, err
	}
	ptyMaster = master
	ptySlave = slave

	logger.WithField("tty", ptySlave.Name()).Info("Created PTY device")

	// Create symlink to PTY slave if requested
	if opts.SymlinkPath != "" {
		if err := os.Symlink(ptySlave.Name(), opts.SymlinkPath); err != nil {
			return zero, fmt.Errorf("failed to create symlink %s -> %s: %w", opts.SymlinkPath, ptySlave.Name(), err)
		}
		symlinkPath = opts.SymlinkPath
		logger.WithFields(logrus.Fields{
			"symlink": symlinkPath,
			"target":  ptySlave.Name(),
		}).Info("Created PTY symlink")
	}

	//// Create an output collector
	//collector, err := lua.NewLuaOutputCollector(
	//	luaApi.OutputChannel(),
	//	outputBufferSize,
	//	func(err error) {
	//		logger.WithError(err).Error("Lua output collector error")
	//	},
	//)
	//if err != nil {
	//	return zero, fmt.Errorf("failed to create output collector: %w", err)
	//}
	//outputCollector = collector
	//
	//if err := outputCollector.Start(); err != nil {
	//	return zero, fmt.Errorf("failed to start output collector: %w", err)
	//}

	//// Start monitor goroutine to consume Lua output
	//monitorWg.Add(1)
	//go func() {
	//	defer monitorWg.Done()
	//	monitorBridgeOutput(bridgeCtx, outputCollector, ptyMaster, logger)
	//}()

	// Report phase: Running
	progressCallback("Running")

	// Create bridge implementation
	bridge := &bridgeImpl{
		luaApi:      luaApi,
		ptyMaster:   ptyMaster,
		ptySlave:    ptySlave,
		symlinkPath: symlinkPath,
	}

	// Execute callback with the bridge
	return callback(bridge)
}

// monitorBridgeOutput monitors and writes Lua output to PTY
//func monitorBridgeOutput(ctx context.Context, collector *lua.LuaOutputCollector, ptyMaster *os.File, logger *logrus.Logger) {
//	ticker := time.NewTicker(100 * time.Millisecond)
//	defer ticker.Stop()
//
//	for {
//		select {
//		case <-ctx.Done():
//			// Final drain
//			consumeBridgeOutput(collector, ptyMaster, logger)
//			return
//		case <-ticker.C:
//			consumeBridgeOutput(collector, ptyMaster, logger)
//		}
//	}
//}

//// consumeBridgeOutput drains output collector and writes to PTY
//func consumeBridgeOutput(collector *lua.LuaOutputCollector, ptyMaster *os.File, logger *logrus.Logger) {
//	if collector == nil || ptyMaster == nil {
//		return
//	}
//
//	consumer := func(record *lua.LuaOutputRecord) (string, error) {
//		if record == nil {
//			return "", nil
//		}
//
//		// Write both stdout and stderr to PTY (stderr with prefix)
//		var content string
//		if record.Source == "stderr" {
//			content = fmt.Sprintf("[ERROR %s] %s", record.Timestamp.Format("15:04:05.000"), record.Content)
//		} else {
//			content = record.Content
//		}
//
//		if len(content) > 0 {
//			if _, err := ptyMaster.Write([]byte(content)); err != nil {
//				logger.WithError(err).Error("Failed to write to PTY")
//			}
//		}
//
//		return "", nil
//	}
//
//	if _, err := lua.ConsumeRecords(collector, consumer); err != nil {
//		logger.WithError(err).Error("Failed to consume output records")
//	}
//}

// createPTY creates a pseudo-terminal and configures it for raw mode.
// Returns clear error messages for common failure scenarios including permission issues.
func createPTY() (master *os.File, slave *os.File, err error) {
	master, slave, err = pty.Open()
	if err != nil {
		// Enhance error message for common permission/resource issues
		return nil, nil, fmt.Errorf("failed to create PTY (check permissions and available PTY devices): %w", err)
	}

	// Set PTY slave to raw mode for proper terminal behavior
	if _, err := term.MakeRaw(int(slave.Fd())); err != nil {
		ptyPath := slave.Name()
		_ = master.Close()
		_ = slave.Close()
		return nil, nil, fmt.Errorf("failed to set PTY %s to raw mode: %w", ptyPath, err)
	}

	return master, slave, nil
}
