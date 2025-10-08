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

// Bridge represents a running BLE-PTY bridge with access to the device and PTY
type Bridge interface {
	GetLuaAPI() *lua.BLEAPI2
	GetPTYMaster() *os.File // PTY master for script I/O
	GetPTYName() string     // PTY device name for display
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

// bridgeImpl implements the Bridge interface
type bridgeImpl struct {
	luaApi    *lua.BLEAPI2
	ptyMaster *os.File
	ptySlave  *os.File
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
		luaApi    *lua.BLEAPI2
		ptyMaster *os.File
		ptySlave  *os.File
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

	//// Create an output collector
	//const outputBufferSize = 1000
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
		luaApi:    luaApi,
		ptyMaster: ptyMaster,
		ptySlave:  ptySlave,
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
