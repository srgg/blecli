package bridge

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/device"
	"github.com/srg/blim/internal/devicefactory"
	"github.com/srg/blim/internal/lua"
	"github.com/srg/blim/internal/ptyio"
)

const (
	// DefaultPtyStdoutBufferSize is the default size, in bytes, of the ring buffer used for PTY stdout input.
	DefaultPtyStdoutBufferSize = 1000

	// DefaultPtyStdinBufferSize is the default size, in bytes, of the ring buffer used for PTY stdin input.
	DefaultPtyStdinBufferSize = 1000
)

// Bridge represents a running BLE-PTY bridge with access to the device and PTY
type Bridge interface {
	GetLuaAPI() *lua.LuaAPI
	GetTTYName() string                 // TTY device name for display
	GetTTYSymlink() string              // Symlink path (empty if not created)
	GetPTY() io.ReadWriter              // PTY I/O as a standard Go interface (for Lua exposure)
	GetPTYIO() ptyio.PTY                // PTY I/O interface (never nil)
	SetPTYReadCallback(cb func([]byte)) // Set callback for PTY data arrival (nil to unregister)
}

// BridgeOptions contains all the configuration for running a bridge
type BridgeOptions struct {
	BleAddress               string                    // BLE device address
	BleConnectTimeout        time.Duration             // BLE Connection timeout
	BleDescriptorReadTimeout time.Duration             // Timeout for reading descriptor values (0 = skip reads)
	BleSubscribeOptions      []device.SubscribeOptions // BLE subscribe options
	Logger                   *logrus.Logger            // Logger instance
	PtyStdinBufferSize       int                       // PTY stdin ring buffer size in bytes (0 = use default)
	PtyStdoutBufferSize      int                       // PTY stdout ring buffer size in bytes (0 = use default)
	TTYSymlinkPath           string                    // Optional tty symlink path for PTY slave (e.g., /tmp/ble-device)
}

// ProgressCallback is called when the bridge phase changes
type ProgressCallback func(phase string)

// BridgeCallback is executed with the running bridge (mirrors InspectCallback)
type BridgeCallback[R any] func(Bridge) (R, error)

// bridgeImpl implements the Bridge interface
type bridgeImpl struct {
	luaApi         *lua.LuaAPI
	ttySymlinkPath string    // TTY Symlink (empty if not created)
	pty            ptyio.PTY // PTY I/O interface for async monitoring
}

func (b *bridgeImpl) GetLuaAPI() *lua.LuaAPI {
	return b.luaApi
}

func (b *bridgeImpl) GetTTYName() string {
	if b.pty != nil {
		return b.pty.TTYName()
	}
	return ""
}

func (b *bridgeImpl) GetTTYSymlink() string {
	return b.ttySymlinkPath
}

func (b *bridgeImpl) GetPTY() io.ReadWriter {
	return b.pty
}

func (b *bridgeImpl) GetPTYIO() ptyio.PTY {
	return b.pty
}

func (b *bridgeImpl) SetPTYReadCallback(cb func([]byte)) {
	if b.pty != nil {
		b.pty.SetReadCallback(cb)
	}
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
	if opts.BleAddress == "" {
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
	if opts.BleConnectTimeout == 0 {
		opts.BleConnectTimeout = 30 * time.Second
	}

	// Create context for cancellation
	bridgeCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Setup cleanup on error
	var (
		luaApi         *lua.LuaAPI
		ttySymlinkPath string
		pty            ptyio.PTY
	)

	defer func() {
		// Remove tty symlink before closing PTY (cleanup order matters)
		if ttySymlinkPath != "" {
			if err := os.Remove(ttySymlinkPath); err != nil {
				logger.WithError(err).WithField("ttySymlink", ttySymlinkPath).Warn("Failed to remove tty symlink")
			} else {
				logger.WithField("ttySymlink", ttySymlinkPath).Debug("Removed tty symlink")
			}
		}

		// Close PTY I/O strategy (stops background monitoring and closes master/slave)
		if pty != nil {
			_ = pty.Close()
		}

		if luaApi != nil {
			if luaApi.GetDevice() != nil && luaApi.GetDevice().IsConnected() {
				_ = luaApi.GetDevice().Disconnect()
			}
			luaApi.Close()
		}
	}()

	// Report phase: Connecting
	progressCallback("Connecting")

	// Create Lua API (creates device)
	dev := devicefactory.NewDevice(opts.BleAddress, logger)
	luaApi = lua.NewBLEAPI2(dev, logger)

	// Connect to device
	connectOpts := &device.ConnectOptions{
		Address:               opts.BleAddress,
		ConnectTimeout:        opts.BleConnectTimeout,
		DescriptorReadTimeout: opts.BleDescriptorReadTimeout,
		Services:              opts.BleSubscribeOptions,
	}

	if err := luaApi.GetDevice().Connect(bridgeCtx, connectOpts); err != nil {
		progressCallback("Failed")
		return zero, fmt.Errorf("failed to connect to device %s: %w", opts.BleAddress, err)
	}

	// Report phase: Connected
	progressCallback("Connected")

	// Report phase: Setting up PTY
	progressCallback("Setting up PTY")

	// Create and configure PTY

	outputBufferSize := opts.PtyStdoutBufferSize
	if outputBufferSize == 0 {
		outputBufferSize = DefaultPtyStdoutBufferSize
	}
	inputBufferSize := opts.PtyStdinBufferSize
	if inputBufferSize == 0 {
		inputBufferSize = DefaultPtyStdinBufferSize
	}

	// Create PTY I/O strategy with background slave monitoring
	var err error
	pty, err = ptyio.NewPty(inputBufferSize, outputBufferSize, logger)
	if err != nil {
		return zero, err
	}

	logger.WithField("tty", pty.TTYName()).Info("Created PTY device")

	// Create symlink to PTY slave if requested
	if opts.TTYSymlinkPath != "" {
		if err := os.Symlink(pty.TTYName(), opts.TTYSymlinkPath); err != nil {
			return zero, fmt.Errorf("failed to create tty symlink %s -> %s: %w", opts.TTYSymlinkPath, pty.TTYName(), err)
		}
		ttySymlinkPath = opts.TTYSymlinkPath
		logger.WithFields(logrus.Fields{
			"ttySymlink": ttySymlinkPath,
			"target":     pty.TTYName(),
		}).Info("Created PTY symlink")
	}

	// Report phase: Running
	progressCallback("Running")

	// Create bridge implementation
	bridge := &bridgeImpl{
		luaApi:         luaApi,
		ttySymlinkPath: ttySymlinkPath,
		pty:            pty,
	}

	// Set bridge info on Lua API (enables pty_write/pty_read via strategy)
	luaApi.SetBridge(bridge)

	// Execute callback with the bridge
	return callback(bridge)
}
