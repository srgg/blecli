package inspector

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/device"
)

// InspectOptions defines options for inspecting a BLE device profile
type InspectOptions struct {
	ConnectTimeout time.Duration
}

// InspectCallback processes a connected device and produces output of type R
type InspectCallback[R any] func(device.Device) (R, error)

// InspectDevice connects to a device, discovers its profile, and executes the callback with the connected device.
// The device lifecycle (connection and disconnection) is managed automatically.
// The callback receives the connected device and can return any result type R along with an error.
func InspectDevice[R any](ctx context.Context, address string, opts *InspectOptions, logger *logrus.Logger, callback InspectCallback[R]) (R, error) {
	var zero R
	if opts == nil {
		opts = &InspectOptions{ConnectTimeout: 30 * time.Second}
	}
	if logger == nil {
		logger = logrus.New()
	}

	// Progress ticker for connecting phase
	connectStart := time.Now()
	stopProgress := make(chan bool)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopProgress:
				return
			case <-ticker.C:
				elapsed := time.Since(connectStart)
				seconds := int(elapsed.Seconds())
				if seconds > 0 {
					fmt.Printf("\rInspecting device %s (Connecting %ds)   ", address, seconds)
				}
			}
		}
	}()

	fmt.Printf("Inspecting device %s (Connecting)   ", address)

	// Create device and connect (reuses BLEConnection.Connect logic - no duplication!)
	dev := device.NewDeviceWithAddress(address, logger)
	connectOpts := &device.ConnectOptions{ConnectTimeout: opts.ConnectTimeout}

	err := dev.Connect(ctx, connectOpts)
	stopProgress <- true

	if err != nil {
		fmt.Print("\r\033[K") // Clear the line
		return zero, err
	}

	fmt.Print("\r\033[K") // Clear the progress line

	// Ensure the device is disconnected after the callback completes
	defer func(dev device.Device) {
		err := dev.Disconnect()
		if err != nil {
			logger.WithError(err).Error("failed to disconnect device")
		}
	}(dev)

	// Execute callback with a connected device
	return callback(dev)
}
