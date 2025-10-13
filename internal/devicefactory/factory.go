package devicefactory

import (
	"context"

	ble "github.com/go-ble/ble"
	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/device"
	"github.com/srg/blim/internal/device/go-ble"
)

// bleScanningDevice wraps ble.Device to implement a device.ScanningDevice interface
type bleScanningDevice struct {
	dev ble.Device
}

// Scan wraps the raw ble.Device.Scan to convert ble.Advertisement to the device.Advertisement
func (s *bleScanningDevice) Scan(ctx context.Context, allowDup bool, handler func(device.Advertisement)) error {
	// Adapter: convert a handler expecting a device.Advertisement to the one expecting ble.Advertisement
	bleHandler := func(adv ble.Advertisement) {
		handler(goble.NewBLEAdvertisement(adv))
	}
	return s.dev.Scan(ctx, allowDup, bleHandler)
}

// DeviceFactory creates a device.ScanningDevice instances for BLE scanning operations.
// This is a variable so that it can be overridden in tests.
var DeviceFactory = func() (device.ScanningDevice, error) {
	dev, err := goble.DeviceFactory()
	if err != nil {
		return nil, err
	}
	return &bleScanningDevice{dev: dev}, nil
}

// NewDevice creates a new BLE device with the specified address.
// This is the primary constructor for creating device instances.
func NewDevice(address string, logger *logrus.Logger) device.Device {
	return goble.NewBLEDeviceWithAddress(address, logger)
}

// NewDeviceFromAdvertisement creates a new BLE device from a device.Advertisement.
// This is used during scanning to create device instances from discovered advertisements.
func NewDeviceFromAdvertisement(adv device.Advertisement, logger *logrus.Logger) device.Device {
	return goble.NewBLEDeviceFromAdvertisement(adv, logger)
}
