package testutils

import (
	"encoding/json"
	"fmt"

	"github.com/go-ble/ble"
	"github.com/sirupsen/logrus"
	"github.com/srg/blecli/internal/testutils/mocks"
	"github.com/srg/blecli/pkg/device"
)

// AdvertisementBuilder builds mocked BLE advertisements for testing.
// It provides a fluent API for configuring mock ble.Advertisement instances
// with explicit field tracking to ensure only set fields have mock expectations.
type AdvertisementBuilder struct {
	name        string
	address     string
	rssi        int
	services    []string
	manufData   []byte
	serviceData map[string][]byte
	txPower     *int
	connectable bool
	logger      *logrus.Logger

	// Track which fields were explicitly set
	nameSet        bool
	addressSet     bool
	rssiSet        bool
	servicesSet    bool
	manufDataSet   bool
	serviceDataSet bool
	txPowerSet     bool
	connectableSet bool
}

// NewAdvertisementBuilder creates a new AdvertisementBuilder with default values.
// The builder starts with connectable=true and empty serviceData map.
func NewAdvertisementBuilder() *AdvertisementBuilder {
	return &AdvertisementBuilder{
		serviceData: make(map[string][]byte),
		connectable: true,
	}
}

// WithName sets the local name for the advertisement.
func (b *AdvertisementBuilder) WithName(name string) *AdvertisementBuilder {
	b.name = name
	b.nameSet = true
	return b
}

// WithAddress sets the device address for the advertisement.
func (b *AdvertisementBuilder) WithAddress(addr string) *AdvertisementBuilder {
	b.address = addr
	b.addressSet = true
	return b
}

// WithRSSI sets the signal strength for the advertisement.
func (b *AdvertisementBuilder) WithRSSI(rssi int) *AdvertisementBuilder {
	b.rssi = rssi
	b.rssiSet = true
	return b
}

// WithServices adds service UUIDs to the advertisement.
// UUIDs can be in short form (e.g., "180D") or full form.
func (b *AdvertisementBuilder) WithServices(uuids ...string) *AdvertisementBuilder {
	b.services = append(b.services, uuids...)
	b.servicesSet = true
	return b
}

// WithManufacturerData sets the manufacturer-specific data.
func (b *AdvertisementBuilder) WithManufacturerData(data []byte) *AdvertisementBuilder {
	b.manufData = data
	b.manufDataSet = true
	return b
}

// WithServiceData adds service-specific data for the given service UUID.
func (b *AdvertisementBuilder) WithServiceData(uuid string, data []byte) *AdvertisementBuilder {
	b.serviceData[uuid] = data
	b.serviceDataSet = true
	return b
}

// WithNoServiceData explicitly sets service data to nil.
// Use this to distinguish between unset and empty service data.
func (b *AdvertisementBuilder) WithNoServiceData() *AdvertisementBuilder {
	b.serviceDataSet = true
	b.serviceData = nil
	return b
}

// WithTxPower sets the transmission power level.
func (b *AdvertisementBuilder) WithTxPower(power int) *AdvertisementBuilder {
	b.txPower = &power
	b.txPowerSet = true
	return b
}

// WithConnectable sets whether the device accepts connections.
func (b *AdvertisementBuilder) WithConnectable(c bool) *AdvertisementBuilder {
	b.connectable = c
	b.connectableSet = true
	return b
}

// FromJSON fills builder fields from a JSON string with format support.
// Panics on invalid JSON as this is intended for test data setup.
func (b *AdvertisementBuilder) FromJSON(jsonStrFmt string, args ...interface{}) *AdvertisementBuilder {
	jsonStr := fmt.Sprintf(jsonStrFmt, args...)

	// First, detect which fields are present in the JSON (even if null)
	var fieldPresence map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &fieldPresence); err != nil {
		panic(fmt.Sprintf("FromJSON: failed to unmarshal field presence: %v", err))
	}

	// Then unmarshal into typed struct
	var data struct {
		Name             *string           `json:"name"`
		Address          *string           `json:"address"`
		RSSI             *int              `json:"rssi"`
		Services         []string          `json:"services"`
		ManufacturerData []byte            `json:"manufacturerData"`
		ServiceData      map[string][]byte `json:"serviceData"`
		TxPower          *int              `json:"txPower"`
		Connectable      *bool             `json:"connectable"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		panic(err)
	}

	// Set flags based on field presence, not just non-nil values
	if _, exists := fieldPresence["name"]; exists {
		if data.Name != nil {
			b.name = *data.Name
		} else {
			b.name = ""
		}
		b.nameSet = true
	}
	if _, exists := fieldPresence["address"]; exists {
		if data.Address != nil {
			b.address = *data.Address
		} else {
			b.address = ""
		}
		b.addressSet = true
	}
	if _, exists := fieldPresence["rssi"]; exists {
		if data.RSSI != nil {
			b.rssi = *data.RSSI
		} else {
			b.rssi = -50 // default
		}
		b.rssiSet = true
	}
	if _, exists := fieldPresence["services"]; exists {
		b.services = data.Services // nil becomes empty slice
		b.servicesSet = true
	}
	if _, exists := fieldPresence["manufacturerData"]; exists {
		b.manufData = data.ManufacturerData // nil becomes empty slice
		b.manufDataSet = true
	}
	if _, exists := fieldPresence["serviceData"]; exists {
		if data.ServiceData != nil {
			b.serviceData = data.ServiceData
		} else {
			b.serviceData = make(map[string][]byte)
		}
		b.serviceDataSet = true
	}
	if _, exists := fieldPresence["txPower"]; exists {
		b.txPower = data.TxPower // can be nil
		b.txPowerSet = true
	}
	if _, exists := fieldPresence["connectable"]; exists {
		if data.Connectable != nil {
			b.connectable = *data.Connectable
		} else {
			b.connectable = true // default
		}
		b.connectableSet = true
	}
	return b
}

// Build creates a MockAdvertisement that implements ble.Advertisement interface.
// All mock expectations are set based on explicitly configured fields only.
// Following testify best practices for mock setup.
func (b *AdvertisementBuilder) Build() *mocks.MockAdvertisement {
	adv := &mocks.MockAdvertisement{}

	// Convert string UUIDs to ble.UUID with proper error handling
	var bleServices []ble.UUID
	for _, s := range b.services {
		bleServices = append(bleServices, ble.MustParse(s))
	}

	// Convert service data with proper UUID parsing
	var bleServiceData []ble.ServiceData
	for uuid, data := range b.serviceData {
		bleServiceData = append(bleServiceData, ble.ServiceData{
			UUID: ble.MustParse(uuid),
			Data: data,
		})
	}

	// Setup mock expectations using testify best practices
	// Only set expectations for explicitly configured fields
	if b.addressSet {
		addr := &mocks.MockAddr{}
		addr.On("String").Return(b.address)
		adv.On("Addr").Return(addr)
	}
	if b.nameSet {
		adv.On("LocalName").Return(b.name)
	}
	if b.rssiSet {
		adv.On("RSSI").Return(b.rssi)
	}
	if b.manufDataSet {
		adv.On("ManufacturerData").Return(b.manufData)
	}
	if b.serviceDataSet {
		adv.On("ServiceData").Return(bleServiceData)
	}
	if b.servicesSet {
		adv.On("Services").Return(bleServices)
	}
	if b.connectableSet {
		adv.On("Connectable").Return(b.connectable)
	}
	if b.txPowerSet {
		if b.txPower != nil {
			adv.On("TxPowerLevel").Return(*b.txPower)
		} else {
			adv.On("TxPowerLevel").Return(127) // BLE spec default for unavailable
		}
	}

	return adv
}

// BuildDevice creates a device.Device from the built advertisement.
// Convenience method for creating Device instances in tests.
func (b *AdvertisementBuilder) BuildDevice(logger *logrus.Logger) device.Device {
	adv := b.Build()
	return device.NewDevice(adv, logger)
}

// AdvertisementArrayBuilder builds arrays of ble.Advertisement with generic parent support.
// It provides a fluent API for creating multiple advertisements with different configurations
// and supports returning to parent builders through the generic type parameter T.
//
// The builder supports two main patterns:
//   - WithAdvertisements(ads...) adds pre-existing ble.Advertisement(s) to the array
//   - WithNewAdvertisement() returns an AdvertisementBuilder for fluent configuration
//
// Type Parameter:
//
//	T: The type to return from Build(). Common values:
//	  - []ble.Advertisement for standalone usage
//	  - *PeripheralDeviceBuilder for integration with device builders
//
// Example usage with pre-existing advertisements:
//
//	// Create advertisements separately
//	ad1 := NewAdvertisementBuilder().WithName("Device1").Build()
//	ad2 := NewAdvertisementBuilder().WithName("Device2").Build()
//
//	// Build array with mix of pre-existing and new advertisements
//	advertisements := NewAdvertisementArrayBuilder[[]ble.Advertisement]().
//	    WithAdvertisements(ad1, ad2). // Add multiple at once
//	    WithNewAdvertisement().
//	        WithName("HeartRate3").
//	        WithAddress("11:22:33:44:55:66").
//	        WithRSSI(-55).
//	        Build().
//	    Build() // Returns []ble.Advertisement
//
// Integration with PeripheralDeviceBuilder:
//
//	// Create advertisements separately
//	existingAds := []ble.Advertisement{ad1, ad2}
//
//	peripheral := NewPeripheralDeviceBuilder().
//	    WithScanAdvertisements().
//	        WithAdvertisements(existingAds...). // Spread slice
//	        WithNewAdvertisement().WithName("Device3").Build().
//	        Build(). // Returns *PeripheralDeviceBuilder
//	    WithService("180D").
//	    Build()
type AdvertisementArrayBuilder[T any] struct {
	advertisements []ble.Advertisement
	parent         T
	buildFunc      func(T, []ble.Advertisement) T
}

// NewAdvertisementArrayBuilder creates a new array builder with the specified generic type.
func NewAdvertisementArrayBuilder[T any]() *AdvertisementArrayBuilder[T] {
	return &AdvertisementArrayBuilder[T]{
		advertisements: make([]ble.Advertisement, 0),
	}
}

// WithAdvertisements adds pre-existing Advertisements to the array and returns the array builder for chaining.
// Supports adding multiple advertisements in a single call.
func (ab *AdvertisementArrayBuilder[T]) WithAdvertisements(ads ...ble.Advertisement) *AdvertisementArrayBuilder[T] {
	ab.advertisements = append(ab.advertisements, ads...)
	return ab
}

// WithNewAdvertisement adds a new advertisement to the array and returns an AdvertisementBuilder.
// When Build() is called on the returned AdvertisementBuilder, it will add the advertisement
// to the array and return the AdvertisementArrayBuilder for method chaining.
func (ab *AdvertisementArrayBuilder[T]) WithNewAdvertisement() *AdvertisementArrayBuilderItem[T] {
	builder := NewAdvertisementBuilder()

	// Create a custom builder that knows about its parent array builder
	customBuilder := &AdvertisementBuilder{
		serviceData: make(map[string][]byte),
		connectable: true,
		name:        builder.name,
		address:     builder.address,
		rssi:        builder.rssi,
		services:    builder.services,
		manufData:   builder.manufData,
		txPower:     builder.txPower,
		logger:      builder.logger,
	}

	return &AdvertisementArrayBuilderItem[T]{
		AdvertisementBuilder: customBuilder,
		parent:               ab,
	}
}

// Build returns the parent if it exists and has a buildFunc, otherwise returns the array
func (ab *AdvertisementArrayBuilder[T]) Build() T {
	if ab.buildFunc != nil {
		return ab.buildFunc(ab.parent, ab.advertisements)
	}
	// If no buildFunc, cast advertisements to T (this works for []*mocks.MockAdvertisement)
	var result interface{} = ab.advertisements
	return result.(T)
}

// AdvertisementArrayBuilderItem wraps AdvertisementBuilder to provide array functionality.
// It embeds AdvertisementBuilder and adds the capability to return to the parent array builder.
type AdvertisementArrayBuilderItem[T any] struct {
	*AdvertisementBuilder
	parent *AdvertisementArrayBuilder[T]
}

// Build adds the advertisement to the parent array and returns the array builder
func (abi *AdvertisementArrayBuilderItem[T]) Build() *AdvertisementArrayBuilder[T] {
	advertisement := abi.AdvertisementBuilder.Build()
	abi.parent.advertisements = append(abi.parent.advertisements, advertisement)
	return abi.parent
}
