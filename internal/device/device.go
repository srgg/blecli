package device

import (
	"context"
	"time"

	"github.com/go-ble/ble"
	"github.com/sirupsen/logrus"
)

//nolint:revive // DeviceInfo name is intentional for clarity when used as a device.DeviceInfo
type DeviceInfo interface {
	GetID() string
	GetName() string
	GetAddress() string
	GetRSSI() int
	GetTxPower() *int
	IsConnectable() bool
	GetAdvertisedServices() []string
	GetManufacturerData() []byte
	GetServiceData() map[string][]byte
}

// Device defines the interface for all device types
type Device interface {
	DeviceInfo

	Connect(ctx context.Context, opts *ConnectOptions) error
	Disconnect() error
	IsConnected() bool
	Update(adv ble.Advertisement)
	GetConnection() Connection
}

// Connection represents a BLE connection interface
type Connection interface {
	GetServices() []Service
	GetService(uuid string) (Service, error)
	GetCharacteristic(service, uuid string) (*BLECharacteristic, error)
	Subscribe(opts []*SubscribeOptions, pattern StreamMode, maxRate time.Duration, callback func(*Record)) error
}

// Service represents a GATT service interface
type Service interface {
	GetUUID() string
	KnownName() string
	GetCharacteristics() []Characteristic
}

// Characteristic represents a GATT characteristic interface
type Characteristic interface {
	GetUUID() string
	KnownName() string
	GetProperties() Properties
	GetDescriptors() []Descriptor
}

// Descriptor represents a GATT descriptor interface
type Descriptor interface {
	GetUUID() string
	KnownName() string
}

// Property represents a single BLE characteristic property
type Property interface {
	Value() int
	KnownName() string
}

// Properties represent a collection of BLE characteristic properties
type Properties interface {
	Broadcast() Property
	Read() Property
	Write() Property
	WriteWithoutResponse() Property
	Notify() Property
	Indicate() Property
	AuthenticatedSignedWrites() Property
	ExtendedProperties() Property
}

// SubscribeOptions defined BLE Characteristics subscriptions
type SubscribeOptions struct {
	Service         string
	Characteristics []string // can be empty
}

// ConnectOptions defines BLE connection options
type ConnectOptions struct {
	Address        string
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
