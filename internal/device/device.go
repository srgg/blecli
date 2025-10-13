package device

import (
	"context"
	"time"
)

// ScanningDevice represents a BLE device capable of scanning for advertisements
type ScanningDevice interface {
	Scan(ctx context.Context, allowDup bool, handler func(Advertisement)) error
}

type Advertisement interface {
	LocalName() string
	ManufacturerData() []byte
	ServiceData() []struct {
		UUID string
		Data []byte
	}

	Services() []string
	OverflowService() []string
	TxPowerLevel() int
	Connectable() bool
	SolicitedService() []string

	RSSI() int
	Addr() string
}

//nolint:revive // DeviceInfo name is intentional for clarity when used as a device.DeviceInfo
type DeviceInfo interface {
	ID() string
	Name() string
	Address() string
	RSSI() int
	TxPower() *int
	IsConnectable() bool
	AdvertisedServices() []string
	ManufacturerData() []byte
	ServiceData() map[string][]byte
}

// Device defines the interface for all device types
type Device interface {
	DeviceInfo

	Connect(ctx context.Context, opts *ConnectOptions) error
	Disconnect() error
	IsConnected() bool
	Update(adv Advertisement)
	GetConnection() Connection
}

type PeripheralDevice interface {
	Device
	ScanningDevice
}

// Connection represents a BLE connection interface
type Connection interface {
	Services() []Service
	GetService(uuid string) (Service, error)
	GetCharacteristic(service, uuid string) (Characteristic, error)
	Subscribe(opts []*SubscribeOptions, pattern StreamMode, maxRate time.Duration, callback func(*Record)) error
}

// Service represents a GATT service interface
type Service interface {
	UUID() string
	KnownName() string
	GetCharacteristics() []Characteristic
}

// Characteristic represents a GATT characteristic interface
type Characteristic interface {
	UUID() string
	KnownName() string
	GetProperties() Properties
	GetDescriptors() []Descriptor
}

// Descriptor represents a GATT descriptor interface
type Descriptor interface {
	UUID() string
	KnownName() string
	Value() []byte            // Returns raw descriptor value bytes, nil if read failed or skipped
	ParsedValue() interface{} // Returns parsed value, *DescriptorError if read failed, nil if skipped
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
	Address               string
	ConnectTimeout        time.Duration
	DescriptorReadTimeout time.Duration // Timeout for reading descriptor values (0 = skip reads)
	Services              []SubscribeOptions
}

// StreamMode defines how subscription data is delivered
type StreamMode int

const (
	StreamEveryUpdate StreamMode = iota
	StreamBatched
	StreamAggregated
)

// Record represents a subscription notification record
type Record struct {
	TsUs        int64
	Seq         uint64
	Values      map[string][]byte   // Single value per characteristic (EveryUpdate/Aggregated modes)
	BatchValues map[string][][]byte // Multiple values per characteristic (Batched mode)
	Flags       uint32
}
