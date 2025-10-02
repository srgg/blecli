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

// InspectDevice connects to a device, discovers its profile and returns the connected device
// Use GetConnection().GetServices() to access discovered services and characteristics
func InspectDevice(ctx context.Context, address string, opts *InspectOptions, logger *logrus.Logger) (device.Device, error) {
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
		return nil, err
	}

	fmt.Print("\r\033[K") // Clear the progress line
	return dev, nil
}
