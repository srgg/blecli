package devicefactory

import (
	ble "github.com/go-ble/ble"
	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/device"
	"github.com/srg/blim/internal/device/go-ble"
)

// DeviceFactory creates ble.Device instances for BLE operations.
// This is a variable so it can be overridden in tests.
var DeviceFactory = func() (ble.Device, error) {
	return goble.DeviceFactory()
}

// NewDevice creates a new BLE device with the specified address.
// This is the primary constructor for creating device instances.
func NewDevice(address string, logger *logrus.Logger) device.Device {
	return goble.NewBLEDeviceWithAddress(address, logger)
}

// NewDeviceFromAdvertisement creates a new BLE device from a BLE advertisement.
// This is used during scanning to create device instances from discovered advertisements.
func NewDeviceFromAdvertisement(adv ble.Advertisement, logger *logrus.Logger) device.Device {
	return goble.NewBLEDeviceFromAdvertisement(adv, logger)
}
