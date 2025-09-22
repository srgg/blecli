package device

import (
	"context"
	"time"

	"github.com/go-ble/ble"
	"github.com/sirupsen/logrus"
)

// Device defines the interface for all device types
type Device interface {
	GetID() string
	GetName() string
	GetAddress() string
	GetRSSI() int
	GetTxPower() *int
	IsConnectable() bool
	GetLastSeen() time.Time
	GetAdvertisedServices() []Service
	GetManufacturerData() []byte
	GetServiceData() map[string][]byte
	DisplayName() string
	IsExpired(timeout time.Duration) bool

	// Connection methods
	Connect(ctx context.Context, opts *ConnectOptions) error
	Disconnect() error
	IsConnected() bool

	// Update methods
	Update(adv ble.Advertisement)

	// BLE-specific methods (for devices that support them)
	WriteToCharacteristic(uuid string, data []byte) error
	GetCharacteristics() ([]Characteristic, error)
	SetDataHandler(f func(uuid string, data []byte))
}

// Service represents a GATT service interface
type Service interface {
	GetUUID() string
	GetCharacteristics() []Characteristic
}

// Characteristic represents a GATT characteristic interface
type Characteristic interface {
	GetUUID() string
	GetProperties() string
	GetDescriptors() []Descriptor
	GetValue() []byte
	SetValue([]byte)
}

// Descriptor represents a GATT descriptor interface
type Descriptor interface {
	GetUUID() string
}

// SubscribeOptions defined BLE Characteristics subscriptions
type SubscribeOptions struct {
	ServiceUUID     string
	Characteristics []string // can be empty
}

// ConnectOptions defines BLE connection options
type ConnectOptions struct {
	ConnectTimeout time.Duration
	Services       []SubscribeOptions
}

// NewDevice creates a Device from a BLE advertisement
func NewDevice(adv ble.Advertisement, logger *logrus.Logger) Device {
	return NewBLEDeviceFromAdvertisement(adv, logger)
}

// NewDeviceWithAddress creates a Device with the specified address
func NewDeviceWithAddress(address string, logger *logrus.Logger) Device {
	return NewBLEDeviceWithAddress(address, logger)
}
