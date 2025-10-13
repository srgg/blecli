package testutils

import (
	"encoding/json"
	"fmt"
	"time"

	blelib "github.com/go-ble/ble"
	"github.com/srg/blim/internal/testutils/mocks"
	"github.com/stretchr/testify/mock"
	"gopkg.in/yaml.v3"
)

// createMockUUID creates a ble.UUID from a string for testing
func createMockUUID(name string) blelib.UUID {
	// Parse as proper UUID - will panic if invalid, which is fine for tests
	return blelib.MustParse(name)
}

// DescriptorReadBehavior specifies error behavior when reading a descriptor
type DescriptorReadBehavior int

const (
	// DescriptorReadTimeout - read blocks and times out
	DescriptorReadTimeout DescriptorReadBehavior = iota + 1 // Start from 1, 0 means no error
	// DescriptorReadError - read returns an error
	DescriptorReadError
)

// UnmarshalYAML implements custom YAML unmarshaling for DescriptorReadBehavior.
// Converts YAML string values to enum constants:
//   - "timeout" → DescriptorReadTimeout
//   - any other non-empty string → DescriptorReadError
//   - empty string or omitted → 0 (no error, normal behavior)
func (d *DescriptorReadBehavior) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}

	switch s {
	case "timeout":
		*d = DescriptorReadTimeout
	case "":
		*d = 0 // Normal behavior (no error)
	default:
		// Any other non-empty string (like "permission denied") means error
		*d = DescriptorReadError
	}
	return nil
}

// DescriptorConfig represents a BLE descriptor configuration for mocking
type DescriptorConfig struct {
	UUID              string                 `json:"uuid" yaml:"uuid"`
	Value             []byte                 `json:"value,omitempty" yaml:"value,omitempty"`
	ReadErrorBehavior DescriptorReadBehavior `json:"-" yaml:"read_error,omitempty"` // Maps YAML read_error field to enum
}

// CharacteristicConfig represents a BLE characteristic configuration for mocking
type CharacteristicConfig struct {
	UUID        string             `json:"uuid" yaml:"uuid"`
	Properties  string             `json:"properties,omitempty" yaml:"properties,omitempty"` // e.g., "read,write,notify"
	Value       []byte             `json:"value,omitempty" yaml:"value,omitempty"`
	Descriptors []DescriptorConfig `json:"descriptors,omitempty" yaml:"descriptors,omitempty"`
}

// ServiceConfig represents a BLE service configuration for mocking
type ServiceConfig struct {
	UUID            string                 `json:"uuid" yaml:"service"`
	Characteristics []CharacteristicConfig `json:"characteristics,omitempty" yaml:"characteristics,omitempty"`
}

// DeviceProfileConfig represents the complete device profile for mocking
type DeviceProfileConfig struct {
	Services []ServiceConfig `json:"services"`
}

// PeripheralDeviceBuilder builds mocked BLE Device with full service/characteristic support.
//
// Supports building complex BLE device profiles with services, characteristics, and descriptors.
// Includes error simulation for descriptor reads (timeout, permission errors).
//
// # Basic Usage
//
//	builder := NewPeripheralDeviceBuilder()
//	builder.WithService("180d").
//	    WithCharacteristic("2a37", "read,notify", []byte{60}).
//	    WithDescriptor("2902", []byte{0x01, 0x00})
//	device := builder.Build()
//
// # Error Simulation
//
// Simulate descriptor read errors for testing error handling:
//
//	builder.WithService("180d").
//	    WithCharacteristic("2a37", "read,notify", []byte{}).
//	    WithDescriptorReadTimeout("2902")  // Simulates 10s timeout
//
//	builder.WithService("180d").
//	    WithCharacteristic("2a37", "read,notify", []byte{}).
//	    WithDescriptorReadError("2902")  // Returns permission denied error
//
// # YAML Integration
//
// The builder config structs support YAML/JSON unmarshaling with automatic error behavior conversion:
//
//	descriptors:
//	  - uuid: "2902"
//	    read_error: "timeout"  # Automatically converts to DescriptorReadTimeout
//	  - uuid: "2901"
//	    read_error: "permission denied"  # Converts to DescriptorReadError
//	  - uuid: "2900"
//	    value: [0x00, 0x00]  # Normal descriptor with value
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
		UUID:        uuid,
		Properties:  properties,
		Value:       value,
		Descriptors: []DescriptorConfig{},
	}
	b.profile.Services[lastServiceIdx].Characteristics = append(
		b.profile.Services[lastServiceIdx].Characteristics, char)
	return b
}

// addDescriptor is a helper to add a descriptor with validation
func (b *PeripheralDeviceBuilder) addDescriptor(desc DescriptorConfig) *PeripheralDeviceBuilder {
	if len(b.profile.Services) == 0 {
		panic("addDescriptor: no service added yet, call WithService first")
	}

	lastServiceIdx := len(b.profile.Services) - 1
	if len(b.profile.Services[lastServiceIdx].Characteristics) == 0 {
		panic("addDescriptor: no characteristic added yet, call WithCharacteristic first")
	}

	lastCharIdx := len(b.profile.Services[lastServiceIdx].Characteristics) - 1
	b.profile.Services[lastServiceIdx].Characteristics[lastCharIdx].Descriptors = append(
		b.profile.Services[lastServiceIdx].Characteristics[lastCharIdx].Descriptors, desc)
	return b
}

// WithDescriptor adds a descriptor to the last added characteristic
func (b *PeripheralDeviceBuilder) WithDescriptor(uuid string, value []byte) *PeripheralDeviceBuilder {
	return b.addDescriptor(DescriptorConfig{UUID: uuid, Value: value})
}

// WithDescriptorReadTimeout adds a descriptor that times out when read
func (b *PeripheralDeviceBuilder) WithDescriptorReadTimeout(uuid string) *PeripheralDeviceBuilder {
	return b.addDescriptor(DescriptorConfig{UUID: uuid, ReadErrorBehavior: DescriptorReadTimeout})
}

// WithDescriptorReadError adds a descriptor that returns an error when read
func (b *PeripheralDeviceBuilder) WithDescriptorReadError(uuid string) *PeripheralDeviceBuilder {
	return b.addDescriptor(DescriptorConfig{UUID: uuid, ReadErrorBehavior: DescriptorReadError})
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
			// Create descriptors for this characteristic
			var bleDescriptors []*blelib.Descriptor
			for descIdx, descConfig := range charConfig.Descriptors {
				bleDesc := &blelib.Descriptor{
					UUID:   createMockUUID(descConfig.UUID),
					Value:  descConfig.Value,
					Handle: uint16(0x0100 + descIdx), // Set a valid handle for testing (0 would be rejected immediately)
				}
				bleDescriptors = append(bleDescriptors, bleDesc)
			}

			bleChar := &blelib.Characteristic{
				UUID:        createMockUUID(charConfig.UUID),
				Property:    parseCharacteristicProperties(charConfig.Properties),
				Value:       charConfig.Value,
				Descriptors: bleDescriptors,
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
	for svcIdx, svc := range bleServices {
		svcConfig := b.profile.Services[svcIdx]
		for charIdx, char := range svc.Characteristics {
			charConfig := svcConfig.Characteristics[charIdx]

			mockClient.On("Subscribe", char, false, mock.Anything).Return(nil)
			mockClient.On("Unsubscribe", char, false).Return(nil)
			mockClient.On("Unsubscribe", char, true).Return(nil)

			// Add read expectations - return value only if characteristic supports reading
			if char.Property&blelib.CharRead != 0 {
				mockClient.On("ReadCharacteristic", char).Return(char.Value, nil)
			} else {
				mockClient.On("ReadCharacteristic", char).Return(nil, fmt.Errorf("characteristic does not support read"))
			}

			// Add descriptor read expectations based on ReadErrorBehavior
			for descIdx, desc := range char.Descriptors {
				descConfig := charConfig.Descriptors[descIdx]
				switch descConfig.ReadErrorBehavior {
				case DescriptorReadTimeout:
					// Timeout: sleep for 10 seconds then panic if timeout wasn't handled properly
					mockClient.On("ReadDescriptor", desc).Run(func(args mock.Arguments) {
						time.Sleep(10 * time.Second)
						panic("BUG: Descriptor read timeout was not handled! The code should have timed out before this panic.")
					}).Return(nil, fmt.Errorf("timeout"))
				case DescriptorReadError:
					// Error: return an error
					mockClient.On("ReadDescriptor", desc).Return(nil, fmt.Errorf("permission denied"))
				default:
					// Normal: return the value
					mockClient.On("ReadDescriptor", desc).Return(desc.Value, nil)
				}
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
