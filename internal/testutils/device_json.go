package testutils

import (
	"encoding/json"

	"github.com/srg/blim/internal/device"
)

type DeviceJSONFull struct {
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	Address          string        `json:"address"`
	RSSI             int           `json:"rssi"`
	TxPower          *int          `json:"tx_power,omitempty"`
	Connectable      bool          `json:"connectable"`
	LastSeen         int64         `json:"last_seen"`
	Services         []ServiceJSON `json:"services"`
	ManufacturerData interface{}   `json:"manufacturer_data,omitempty"`
	ServiceData      interface{}   `json:"service_data,omitempty"`
	DisplayName      string        `json:"display_name"`
}

type ServiceJSON struct {
	UUID            string               `json:"uuid"`
	Characteristics []CharacteristicJSON `json:"characteristics"`
}

type CharacteristicJSON struct {
	UUID        string           `json:"uuid"`
	Properties  string           `json:"properties"`
	Descriptors []DescriptorJSON `json:"descriptors"`
	Value       string           `json:"value"`
}

type DescriptorJSON struct {
	UUID string `json:"uuid"`
}

// DeviceToJSON converts a device. Device to JSON string
func DeviceToJSON(d device.Device) string {
	// Map Services - advertised services are now just UUIDs (no characteristics until connected)
	var services []ServiceJSON
	for _, serviceUUID := range d.GetAdvertisedServices() {
		services = append(services, ServiceJSON{
			UUID:            serviceUUID,            // serviceUUID is already a string
			Characteristics: []CharacteristicJSON{}, // Advertised services have no characteristics
		})
	}

	// Keep manufacturer and service data as byte arrays (closer to BLE format)
	// Convert []byte to []int to avoid base64 encoding
	var manufData interface{}
	if d.GetManufacturerData() != nil {
		byteData := d.GetManufacturerData()
		intData := make([]int, len(byteData))
		for i, b := range byteData {
			intData[i] = int(b)
		}
		manufData = intData
	}

	var serviceData interface{}
	if len(d.GetServiceData()) > 0 {
		svcData := make(map[string][]int)
		for k, v := range d.GetServiceData() {
			intData := make([]int, len(v))
			for i, b := range v {
				intData[i] = int(b)
			}
			svcData[k] = intData
		}
		serviceData = svcData
	}

	jsonStruct := DeviceJSONFull{
		ID:               d.GetID(),
		Name:             d.GetName(),
		Address:          d.GetAddress(),
		RSSI:             d.GetRSSI(),
		TxPower:          d.GetTxPower(),
		Connectable:      d.IsConnectable(),
		LastSeen:         d.GetLastSeen().Unix(),
		Services:         services,
		ManufacturerData: manufData,
		ServiceData:      serviceData,
		DisplayName:      d.DisplayName(),
	}

	b, err := json.Marshal(jsonStruct)
	if err != nil {
		panic(err)
	}

	return string(b) // convert []byte to string
}
