package device

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// NotFoundError represents an error when a BLE resource is not found
type NotFoundError struct {
	Resource string   // "service", "characteristic", "descriptor"
	UUIDs    []string // One or more UUIDs (e.g., [serviceUUID] or [serviceUUID, charUUID])
}

func (e *NotFoundError) Error() string {
	if len(e.UUIDs) == 0 {
		return fmt.Sprintf("%s not found", e.Resource)
	}
	if len(e.UUIDs) == 1 {
		return fmt.Sprintf("%s %q not found", e.Resource, e.UUIDs[0])
	}
	// Multiple UUIDs (e.g., characteristic in service, descriptor in characteristic)
	// For BLE hierarchy: characteristic is in service, descriptor is in characteristic
	parentResource := "service"
	if e.Resource == "descriptor" {
		parentResource = "characteristic"
	}
	return fmt.Sprintf("%s %q not found in %s %q", e.Resource, e.UUIDs[len(e.UUIDs)-1], parentResource, e.UUIDs[0])
}

// ConnectionState represents the specific kind of connection state failure
type ConnectionState string

const (
	NotConnected     ConnectionState = "not_connected"
	AlreadyConnected ConnectionState = "already_connected"
	NotInitialized   ConnectionState = "not_initialized"
)

// ConnectionError represents any connection-related problem
type ConnectionError struct {
	State ConnectionState
	Msg   string
}

// Error implements the error interface
func (e *ConnectionError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Msg == "" {
		return string(e.State)
	}
	return fmt.Sprintf("%s: %s", e.State, e.Msg)
}

// Is allows errors.Is to compare ConnectionError values by State
func (e *ConnectionError) Is(target error) bool {
	if e == nil {
		return false
	}
	t, ok := target.(*ConnectionError)
	if !ok {
		return false
	}
	return e.State == t.State
}

// Predefined sentinel errors for connection states
var (
	ErrNotConnected     = &ConnectionError{State: NotConnected}
	ErrAlreadyConnected = &ConnectionError{State: AlreadyConnected}
	ErrNotInitialized   = &ConnectionError{State: NotInitialized}
)

// Operation errors
var (
	ErrTimeout     = errors.New("timeout")
	ErrUnsupported = errors.New("unsupported")
)

// NormalizeError maps known go-ble error strings to structured ConnectionError types.
// It ensures consistent handling even if the upstream library changes messages slightly.
// Returns wrapped errors to preserve original context.
func NormalizeError(err error) error {
	if err == nil {
		return nil
	}

	msg := err.Error()
	switch {
	case containsIgnoreCase(msg, "device not connected"):
		return fmt.Errorf("%w: %v", ErrNotConnected, err)
	case containsIgnoreCase(msg, "device already connected"):
		return fmt.Errorf("%w: %v", ErrAlreadyConnected, err)
	case containsIgnoreCase(msg, "connection is not initialized"):
		return fmt.Errorf("%w: %v", ErrNotInitialized, err)
	default:
		return err
	}
}

// containsIgnoreCase checks substring case-insensitively
func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// IsConnectionState reports whether err is a ConnectionError with the given state
func IsConnectionState(err error, state ConnectionState) bool {
	var cerr *ConnectionError
	if errors.As(err, &cerr) {
		return cerr.State == state
	}
	return false
}

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

// CharacteristicInfo represents characteristic metadata
type CharacteristicInfo interface {
	UUID() string
	KnownName() string
	GetProperties() Properties
	GetDescriptors() []Descriptor
}

// DescriptorInfo represents descriptor metadata
type DescriptorInfo interface {
	UUID() string
	KnownName() string
	Value() []byte            // Returns raw descriptor value bytes, nil if read failed or skipped
	ParsedValue() interface{} // Returns parsed value, *DescriptorError if read failed, nil if skipped
}

// CharacteristicReader provides read operations
type CharacteristicReader interface {
	Read(timeout time.Duration) ([]byte, error)
}

// CharacteristicWriter provides write operations
type CharacteristicWriter interface {
	Write(data []byte, withResponse bool, timeout time.Duration) error
}

// Characteristic combines info + operations
type Characteristic interface {
	CharacteristicInfo
	CharacteristicReader
	CharacteristicWriter
}

// Descriptor combines descriptor information (writes deferred to future implementation)
type Descriptor interface {
	DescriptorInfo
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
