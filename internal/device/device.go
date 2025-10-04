package device

import (
	"context"
	"time"

	"github.com/go-ble/ble"
	"github.com/sirupsen/logrus"
)

type DeviceInfo interface {
	GetID() string
	GetName() string
	GetAddress() string
	GetRSSI() int
	GetTxPower() *int
	IsConnectable() bool
	GetLastSeen() time.Time
	GetAdvertisedServices() []string
	GetManufacturerData() []byte
	GetServiceData() map[string][]byte
	DisplayName() string
	IsExpired(timeout time.Duration) bool
}

type DeviceInfoJSON struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	Address            string            `json:"address"`
	RSSI               int               `json:"rssi"`
	TxPower            *int              `json:"txPower,omitempty"`
	Connectable        bool              `json:"connectable"`
	LastSeen           time.Time         `json:"lastSeen"`
	AdvertisedServices []string          `json:"advertisedServices,omitempty"`
	ManufacturerData   []byte            `json:"manufData,omitempty"`
	ServiceData        map[string][]byte `json:"serviceData,omitempty"`
}

func deviceInfo2DeviceInfoJson(d DeviceInfo) DeviceInfoJSON {
	// Convert []Service to []string
	svcStrUUIDS := make([]string, len(d.GetAdvertisedServices()))
	for i, s := range d.GetAdvertisedServices() {
		svcStrUUIDS[i] = s
	}

	return DeviceInfoJSON{
		ID:                 d.GetID(),
		Name:               d.GetName(),
		Address:            d.GetAddress(),
		RSSI:               d.GetRSSI(),
		TxPower:            d.GetTxPower(),
		Connectable:        d.IsConnectable(),
		LastSeen:           d.GetLastSeen(),
		AdvertisedServices: svcStrUUIDS,
		ManufacturerData:   d.GetManufacturerData(),
		ServiceData:        d.GetServiceData(),
	}
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
	GetServices() map[string]Service
	GetCharacteristic(service, uuid string) (*BLECharacteristic, error)
	Subscribe(opts []*SubscribeOptions, pattern StreamMode, maxRate time.Duration, callback func(*Record)) error
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
	//GetValue() []byte
	//SetValue([]byte)
}

// Descriptor represents a GATT descriptor interface
type Descriptor interface {
	GetUUID() string
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
