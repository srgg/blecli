//go:build test

package testutils

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	blelib "github.com/go-ble/ble"
	"github.com/srg/blim/internal/device"
	blemocks "github.com/srg/blim/internal/testutils/mocks/goble"
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

// UnmarshalYAML implements custom YAML unmarshalling for DescriptorReadBehavior.
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
	ReadDelay   time.Duration      `json:"-" yaml:"-"` // Delay before returning read response (for timeout testing)
	WriteDelay  time.Duration      `json:"-" yaml:"-"` // Delay before returning write response (for timeout testing)
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

// PeripheralDeviceBuilder builds a mocked BLE Device with full service/characteristic support.
//
// Supports building complex BLE device profiles with services, characteristics, and descriptors.
// Includes error simulation for descriptor reads (timeout, permission errors).
//
// # Basic Usage
//
//	builder := NewPeripheralDeviceBuilder(t)
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
	scanAdvertisements []device.Advertisement
	t                  *testing.T    // Testing instance for automatic cleanup registration
	disconnectChan     chan struct{} // Disconnect channel for graceful disconnect testing
}

// NewPeripheralDeviceBuilder creates a new peripheral device builder.
// The testing instance t is required for automatic cleanup of disconnect channels via t.Cleanup().
func NewPeripheralDeviceBuilder(t *testing.T) *PeripheralDeviceBuilder {
	return &PeripheralDeviceBuilder{
		profile: DeviceProfileConfig{
			Services: []ServiceConfig{},
		},
		t: t,
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

// CharacteristicOption is a functional option for configuring characteristics
type CharacteristicOption func(*CharacteristicConfig)

// WithReadDelay sets the read delay for timeout testing
func WithReadDelay(delay time.Duration) CharacteristicOption {
	return func(c *CharacteristicConfig) {
		c.ReadDelay = delay
	}
}

// WithWriteDelay sets the writing delay for timeout testing
func WithWriteDelay(delay time.Duration) CharacteristicOption {
	return func(c *CharacteristicConfig) {
		c.WriteDelay = delay
	}
}

// WithCharacteristic adds a characteristic to the last added service
func (b *PeripheralDeviceBuilder) WithCharacteristic(uuid, properties string, value []byte, opts ...CharacteristicOption) *PeripheralDeviceBuilder {
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

	// Apply functional options
	for _, opt := range opts {
		opt(&char)
	}

	b.profile.Services[lastServiceIdx].Characteristics = append(
		b.profile.Services[lastServiceIdx].Characteristics, char)
	return b
}

// addDescriptor adds a descriptor to the last added characteristic (internal helper)
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
	arrayBuilder.buildFunc = func(parent *PeripheralDeviceBuilder, ads []device.Advertisement) *PeripheralDeviceBuilder {
		// Add ble.Advertisements are directly to scan advertisements
		parent.scanAdvertisements = append(parent.scanAdvertisements, ads...)
		return parent
	}
	return arrayBuilder
}

// parseCharacteristicProperties converts the property string to BLE.Property flags
func parseCharacteristicProperties(props string) blelib.Property {
	if props == "" {
		return blelib.CharRead | blelib.CharWrite | blelib.CharNotify // default
	}

	var property blelib.Property

	// Split by comma, trim whitespace, and parse each property individually
	parts := strings.Split(props, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		switch part {
		case "read":
			property |= blelib.CharRead
		case "write":
			property |= blelib.CharWrite
		case "write-without-response":
			property |= blelib.CharWriteNR
		case "notify":
			property |= blelib.CharNotify
		case "indicate":
			property |= blelib.CharIndicate
		default:
			// FAIL FAST on unknown properties to catch configuration errors
			panic(fmt.Sprintf("parseCharacteristicProperties: unknown property '%s' in '%s'", part, props))
		}
	}

	// If no valid properties were parsed, return default
	if property == 0 {
		return blelib.CharRead | blelib.CharWrite | blelib.CharNotify
	}

	return property
}

// Build creates a mocked ble.Device with the configured profile.
// Automatically registers cleanup via b.t.Cleanup() for the disconnect channel created by this call.
func (b *PeripheralDeviceBuilder) Build() blelib.Device {
	mockDevice := &blemocks.MockDevice{}
	mockClient := &blemocks.MockClient{}

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

	// Set up disconnect channel expectation for graceful disconnect handling.
	// Each Build() creates a new disconnect channel to support the monitoring goroutine
	// in connection.go (lines 300-316), which detects CoreBluetooth disconnections.
	// Store the channel in the builder for test access via GetDisconnectChannel().
	// Register cleanup to close the channel and prevent goroutine leak.
	b.disconnectChan = make(chan struct{})
	b.t.Cleanup(func() {
		// Close channel if not already closed (idempotent cleanup)
		select {
		case <-b.disconnectChan:
			// Channel already closed
		default:
			close(b.disconnectChan)
		}
	})
	mockClient.On("Disconnected").Return((<-chan struct{})(b.disconnectChan))

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
				if charConfig.ReadDelay > 0 {
					// Add delay for timeout testing
					mockClient.On("ReadCharacteristic", char).Run(func(args mock.Arguments) {
						time.Sleep(charConfig.ReadDelay)
					}).Return(char.Value, nil)
				} else {
					mockClient.On("ReadCharacteristic", char).Return(char.Value, nil)
				}
			} else {
				mockClient.On("ReadCharacteristic", char).Return(nil, fmt.Errorf("characteristic does not support read"))
			}

			// Add write expectations - accept writes if characteristic supports writing
			if char.Property&blelib.CharWrite != 0 || char.Property&blelib.CharWriteNR != 0 {
				if charConfig.WriteDelay > 0 {
					// Add delay for timeout testing
					mockClient.On("WriteCharacteristic", char, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
						time.Sleep(charConfig.WriteDelay)
					}).Return(nil)
				} else {
					mockClient.On("WriteCharacteristic", char, mock.Anything, mock.Anything).Return(nil)
				}
			} else {
				mockClient.On("WriteCharacteristic", char, mock.Anything, mock.Anything).Return(fmt.Errorf("characteristic does not support write"))
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

	// Set up scan expectations using configured advertisements
	mockDevice.On("Scan", mock.Anything, mock.Anything, mock.MatchedBy(func(handler blelib.AdvHandler) bool {
		// Simulate discovering all configured advertisements
		for _, devAdv := range b.scanAdvertisements {
			// Create an inline adapter that wraps the device.Advertisement as ble.Advertisement
			mockAddr := &blemocks.MockAddr{}
			mockAddr.On("String").Return(devAdv.Addr())

			// Create ble.Advertisement adapter
			adapter := &blemocks.MockAdvertisement{}
			adapter.On("LocalName").Return(devAdv.LocalName())
			adapter.On("ManufacturerData").Return(devAdv.ManufacturerData())
			adapter.On("TxPowerLevel").Return(devAdv.TxPowerLevel())
			adapter.On("Connectable").Return(devAdv.Connectable())
			adapter.On("RSSI").Return(devAdv.RSSI())
			adapter.On("Addr").Return(mockAddr)

			// Convert ServiceData
			deviceServiceData := devAdv.ServiceData()
			bleServiceData := make([]blelib.ServiceData, len(deviceServiceData))
			for i, sd := range deviceServiceData {
				bleServiceData[i] = blelib.ServiceData{
					UUID: blelib.MustParse(sd.UUID),
					Data: sd.Data,
				}
			}
			adapter.On("ServiceData").Return(bleServiceData)

			// Convert Services
			deviceServices := devAdv.Services()
			bleServices := make([]blelib.UUID, len(deviceServices))
			for i, svc := range deviceServices {
				bleServices[i] = blelib.MustParse(svc)
			}
			adapter.On("Services").Return(bleServices)

			// Convert OverflowService and SolicitedService
			deviceOverflow := devAdv.OverflowService()
			bleOverflow := make([]blelib.UUID, len(deviceOverflow))
			for i, svc := range deviceOverflow {
				bleOverflow[i] = blelib.MustParse(svc)
			}
			adapter.On("OverflowService").Return(bleOverflow)

			deviceSolicited := devAdv.SolicitedService()
			bleSolicited := make([]blelib.UUID, len(deviceSolicited))
			for i, svc := range deviceSolicited {
				bleSolicited[i] = blelib.MustParse(svc)
			}
			adapter.On("SolicitedService").Return(bleSolicited)

			handler(adapter)
		}
		return true
	})).Return(nil)

	return mockDevice
}

// GetServices returns the configured services for use in creating connection options
func (b *PeripheralDeviceBuilder) GetServices() []ServiceConfig {
	return b.profile.Services
}

// GetDisconnectChannel returns the disconnect channel created by Build().
// This channel can be closed by tests to simulate a graceful disconnect from CoreBluetooth.
// Returns nil if Build() has not been called yet.
func (b *PeripheralDeviceBuilder) GetDisconnectChannel() chan struct{} {
	return b.disconnectChan
}
