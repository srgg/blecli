package testutils

import (
	"encoding/json"
	"fmt"

	"github.com/go-ble/ble"
	"github.com/sirupsen/logrus"
	"github.com/srg/blecli/internal/testutils/mocks"
	"github.com/srg/blecli/pkg/device"
)

// AdvertisementBuilder builds mocked BLE Advertisement
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

// NewAdvertisementBuilder creates a new builder
func NewAdvertisementBuilder() *AdvertisementBuilder {
	return &AdvertisementBuilder{
		serviceData: make(map[string][]byte),
		connectable: true,
	}
}

// Builder setters
func (b *AdvertisementBuilder) WithName(name string) *AdvertisementBuilder {
	b.name = name
	b.nameSet = true
	return b
}

func (b *AdvertisementBuilder) WithAddress(addr string) *AdvertisementBuilder {
	b.address = addr
	b.addressSet = true
	return b
}

func (b *AdvertisementBuilder) WithRSSI(rssi int) *AdvertisementBuilder {
	b.rssi = rssi
	b.rssiSet = true
	return b
}

func (b *AdvertisementBuilder) WithServices(uuids ...string) *AdvertisementBuilder {
	b.services = append(b.services, uuids...)
	b.servicesSet = true
	return b
}

func (b *AdvertisementBuilder) WithManufacturerData(data []byte) *AdvertisementBuilder {
	b.manufData = data
	b.manufDataSet = true
	return b
}

func (b *AdvertisementBuilder) WithServiceData(uuid string, data []byte) *AdvertisementBuilder {
	b.serviceData[uuid] = data
	b.serviceDataSet = true
	return b
}

func (b *AdvertisementBuilder) WithTxPower(power int) *AdvertisementBuilder {
	b.txPower = &power
	b.txPowerSet = true
	return b
}

func (b *AdvertisementBuilder) WithConnectable(c bool) *AdvertisementBuilder {
	b.connectable = c
	b.connectableSet = true
	return b
}

// FromJSON fills builder fields from a JSON string
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

//func (b *AdvertisementBuilder) FromJSON(jsonStr string) *AdvertisementBuilder {
//	// First, detect which fields are present in the JSON (even if null)
//	var fieldPresence map[string]interface{}
//	if err := json.Unmarshal([]byte(jsonStr), &fieldPresence); err != nil {
//		panic(err)
//	}
//
//	// Then unmarshal into typed struct
//	var data struct {
//		Name             *string           `json:"name"`
//		Address          *string           `json:"address"`
//		RSSI             *int              `json:"rssi"`
//		Services         []string          `json:"services"`
//		ManufacturerData []byte            `json:"manufacturerData"`
//		ServiceData      map[string][]byte `json:"serviceData"`
//		TxPower          *int              `json:"txPower"`
//		Connectable      *bool             `json:"connectable"`
//	}
//
//	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
//		panic(err)
//	}
//
//	// Set flags based on field presence, not just non-nil values
//	if _, exists := fieldPresence["name"]; exists {
//		if data.Name != nil {
//			b.name = *data.Name
//		} else {
//			b.name = ""
//		}
//		b.nameSet = true
//	}
//	if _, exists := fieldPresence["address"]; exists {
//		if data.Address != nil {
//			b.address = *data.Address
//		} else {
//			b.address = ""
//		}
//		b.addressSet = true
//	}
//	if _, exists := fieldPresence["rssi"]; exists {
//		if data.RSSI != nil {
//			b.rssi = *data.RSSI
//		} else {
//			b.rssi = -50 // default
//		}
//		b.rssiSet = true
//	}
//	if _, exists := fieldPresence["services"]; exists {
//		b.services = data.Services // nil becomes empty slice
//		b.servicesSet = true
//	}
//	if _, exists := fieldPresence["manufacturerData"]; exists {
//		b.manufData = data.ManufacturerData // nil becomes empty slice
//		b.manufDataSet = true
//	}
//	if _, exists := fieldPresence["serviceData"]; exists {
//		if data.ServiceData != nil {
//			b.serviceData = data.ServiceData
//		} else {
//			b.serviceData = make(map[string][]byte)
//		}
//		b.serviceDataSet = true
//	}
//	if _, exists := fieldPresence["txPower"]; exists {
//		b.txPower = data.TxPower // can be nil
//		b.txPowerSet = true
//	}
//	if _, exists := fieldPresence["connectable"]; exists {
//		if data.Connectable != nil {
//			b.connectable = *data.Connectable
//		} else {
//			b.connectable = true // default
//		}
//		b.connectableSet = true
//	}
//	return b
//}

// Build creates a MockAdvertisement
func (b *AdvertisementBuilder) Build() *mocks.MockAdvertisement {
	adv := &mocks.MockAdvertisement{}

	// Convert string UUIDs to ble.UUID
	var bleServices []ble.UUID
	for _, s := range b.services {
		bleServices = append(bleServices, ble.MustParse(s))
	}

	// Convert service data
	var bleServiceData []ble.ServiceData
	for uuid, data := range b.serviceData {
		bleServiceData = append(bleServiceData, ble.ServiceData{
			UUID: ble.MustParse(uuid),
			Data: data,
		})
	}

	// Setup mock expectations only for fields that were explicitly set
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
			adv.On("TxPowerLevel").Return(127)
		}
	}

	return adv
}

func (b *AdvertisementBuilder) BuildDevice(logger *logrus.Logger) device.Device {
	adv := b.Build()

	return device.NewDevice(adv, logger)
}
