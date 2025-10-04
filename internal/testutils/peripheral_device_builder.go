package testutils

import (
	"encoding/json"
	"fmt"

	blelib "github.com/go-ble/ble"
	"github.com/srg/blim/internal/testutils/mocks"
	"github.com/stretchr/testify/mock"
)

// createMockUUID creates a ble.UUID from a string for testing
func createMockUUID(name string) blelib.UUID {
	// Parse as proper UUID - will panic if invalid, which is fine for tests
	return blelib.MustParse(name)
}

// CharacteristicConfig represents a BLE characteristic configuration for mocking
type CharacteristicConfig struct {
	UUID       string `json:"uuid"`
	Properties string `json:"properties,omitempty"` // e.g., "read,write,notify"
	Value      []byte `json:"value,omitempty"`
}

// ServiceConfig represents a BLE service configuration for mocking
type ServiceConfig struct {
	UUID            string                 `json:"uuid"`
	Characteristics []CharacteristicConfig `json:"characteristics,omitempty"`
}

// DeviceProfileConfig represents the complete device profile for mocking
type DeviceProfileConfig struct {
	Services []ServiceConfig `json:"services"`
}

// PeripheralDeviceBuilder builds mocked BLE Device with full service/characteristic support
type PeripheralDeviceBuilder struct {
	profile            DeviceProfileConfig
	scanAdvertisements []blelib.Advertisement
}

// NewPeripheralDeviceBuilder creates a new peripheral device builder
func NewPeripheralDeviceBuilder() *PeripheralDeviceBuilder {
	return &PeripheralDeviceBuilder{
		profile: DeviceProfileConfig{
			Services: []ServiceConfig{},
		},
	}
}

// WithService adds a service to the device profile
func (b *PeripheralDeviceBuilder) WithService(uuid string) *PeripheralDeviceBuilder {
	b.profile.Services = append(b.profile.Services, ServiceConfig{
		UUID:            uuid,
		Characteristics: []CharacteristicConfig{},
	})
	return b
}

// WithCharacteristic adds a characteristic to the last added service
func (b *PeripheralDeviceBuilder) WithCharacteristic(uuid, properties string, value []byte) *PeripheralDeviceBuilder {
	if len(b.profile.Services) == 0 {
		panic("WithCharacteristic: no service added yet, call WithService first")
	}

	lastServiceIdx := len(b.profile.Services) - 1
	char := CharacteristicConfig{
		UUID:       uuid,
		Properties: properties,
		Value:      value,
	}
	b.profile.Services[lastServiceIdx].Characteristics = append(
		b.profile.Services[lastServiceIdx].Characteristics, char)
	return b
}

// FromJSON fills the device profile from JSON
func (b *PeripheralDeviceBuilder) FromJSON(jsonStrFmt string, args ...interface{}) *PeripheralDeviceBuilder {
	jsonStr := fmt.Sprintf(jsonStrFmt, args...)

	var config DeviceProfileConfig
	if err := json.Unmarshal([]byte(jsonStr), &config); err != nil {
		panic(fmt.Sprintf("PeripheralDeviceBuilder.FromJSON: failed to unmarshal: %v", err))
	}

	b.profile = config
	return b
}

// WithScanAdvertisements returns an AdvertisementArrayBuilder that will return this PeripheralDeviceBuilder on Build()
func (b *PeripheralDeviceBuilder) WithScanAdvertisements() *AdvertisementArrayBuilder[*PeripheralDeviceBuilder] {
	arrayBuilder := NewAdvertisementArrayBuilder[*PeripheralDeviceBuilder]()
	arrayBuilder.parent = b
	arrayBuilder.buildFunc = func(parent *PeripheralDeviceBuilder, ads []blelib.Advertisement) *PeripheralDeviceBuilder {
		// Add ble.Advertisements directly to scan advertisements
		parent.scanAdvertisements = append(parent.scanAdvertisements, ads...)
		return parent
	}
	return arrayBuilder
}

// parseCharacteristicProperties converts property string to ble.Property flags
func parseCharacteristicProperties(props string) blelib.Property {
	if props == "" {
		return blelib.CharRead | blelib.CharWrite | blelib.CharNotify // default
	}

	var property blelib.Property
	// Simple parsing - can be enhanced as needed
	switch props {
	case "read":
		property = blelib.CharRead
	case "write":
		property = blelib.CharWrite
	case "notify":
		property = blelib.CharNotify
	case "read,write":
		property = blelib.CharRead | blelib.CharWrite
	case "read,notify":
		property = blelib.CharRead | blelib.CharNotify
	case "write,notify":
		property = blelib.CharWrite | blelib.CharNotify
	case "read,write,notify":
		property = blelib.CharRead | blelib.CharWrite | blelib.CharNotify
	default:
		property = blelib.CharRead | blelib.CharWrite | blelib.CharNotify // default
	}
	return property
}

// Build creates a mocked ble.Device with the configured profile
func (b *PeripheralDeviceBuilder) Build() blelib.Device {
	mockDevice := &mocks.MockDevice{}
	mockClient := &mocks.MockClient{}

	// Create the BLE profile with services and characteristics
	var bleServices []*blelib.Service
	for _, svcConfig := range b.profile.Services {
		bleService := &blelib.Service{
			UUID: createMockUUID(svcConfig.UUID),
		}

		var bleCharacteristics []*blelib.Characteristic
		for _, charConfig := range svcConfig.Characteristics {
			bleChar := &blelib.Characteristic{
				UUID:     createMockUUID(charConfig.UUID),
				Property: parseCharacteristicProperties(charConfig.Properties),
				Value:    charConfig.Value,
			}
			bleCharacteristics = append(bleCharacteristics, bleChar)
		}
		bleService.Characteristics = bleCharacteristics
		bleServices = append(bleServices, bleService)
	}

	// Create the profile that will be returned by DiscoverProfile
	mockProfile := &blelib.Profile{
		Services: bleServices,
	}

	// Set up mock expectations
	mockDevice.On("Dial", mock.Anything, mock.Anything).Return(mockClient, nil)
	mockClient.On("DiscoverProfile", true).Return(mockProfile, nil)
	mockClient.On("CancelConnection").Return(nil)

	// Set up subscription expectations for all characteristics
	for _, svc := range bleServices {
		for _, char := range svc.Characteristics {
			mockClient.On("Subscribe", char, false, mock.Anything).Return(nil)
			mockClient.On("Unsubscribe", char, false).Return(nil)
			mockClient.On("Unsubscribe", char, true).Return(nil)

			// Add read expectations - return value only if characteristic supports reading
			if char.Property&blelib.CharRead != 0 {
				mockClient.On("ReadCharacteristic", char).Return(char.Value, nil)
			} else {
				mockClient.On("ReadCharacteristic", char).Return(nil, fmt.Errorf("characteristic does not support read"))
			}
		}
	}

	// Set up scan expectations - simulate discovering the configured advertisements
	mockDevice.On("Scan", mock.Anything, mock.Anything, mock.MatchedBy(func(handler blelib.AdvHandler) bool {
		// Simulate discovering all configured advertisements
		for _, adv := range b.scanAdvertisements {
			handler(adv)
		}
		return true
	})).Return(nil)

	return mockDevice
}

// GetServices returns the configured services for use in creating connection options
func (b *PeripheralDeviceBuilder) GetServices() []ServiceConfig {
	return b.profile.Services
}
