package inspector

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/device"
)

// ProgressCallback is called when the inspection phase changes
type ProgressCallback func(phase string)

// InspectOptions defines options for inspecting a BLE device profile
type InspectOptions struct {
	ConnectTimeout time.Duration
}

// InspectCallback processes a connected device and produces output of type R
type InspectCallback[R any] func(device.Device) (R, error)

// InspectDevice connects to a device, discovers its profile, and executes the callback with the connected device.
// The device lifecycle (connection and disconnection) is managed automatically.
// The callback receives the connected device and can return any result type R along with an error.
// Optional progressCallback can be provided for connection progress updates.
func InspectDevice[R any](ctx context.Context, address string, opts *InspectOptions, logger *logrus.Logger, progressCallback ProgressCallback, callback InspectCallback[R]) (R, error) {
	var zero R
	if opts == nil {
		opts = &InspectOptions{ConnectTimeout: 30 * time.Second}
	}
	if logger == nil {
		logger = logrus.New()
	}
	if progressCallback == nil {
		progressCallback = func(string) {} // No-op callback
	}

	// Report phase change: starting connection
	progressCallback("Connecting")

	// Create device and connect (reuses BLEConnection.Connect logic - no duplication!)
	dev := device.NewDeviceWithAddress(address, logger)
	connectOpts := &device.ConnectOptions{ConnectTimeout: opts.ConnectTimeout}

	err := dev.Connect(ctx, connectOpts)

	if err != nil {
		progressCallback("Failed")
		return zero, err
	}

	// Report phase change: connected
	progressCallback("Connected")

	// Ensure the device is disconnected after the callback completes
	defer func(dev device.Device) {
		err := dev.Disconnect()
		if err != nil {
			logger.WithError(err).Error("failed to disconnect device")
		}
	}(dev)

	// Report phase change: processing results
	progressCallback("Processing results")

	// Execute callback with a connected device
	return callback(dev)
}
