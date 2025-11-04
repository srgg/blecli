//go:build test

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

// AdvertisementToJSON converts a device.Advertisement to JSON string
func AdvertisementToJSON(adv device.Advertisement) string {
	// Convert manufacturer data from []byte to hex string
	var manufDataStr string
	if adv.ManufacturerData() != nil {
		manufDataStr = bytesToHex(adv.ManufacturerData())
	}

	// Convert service data
	var serviceDataMap map[string]string
	if adv.ServiceData() != nil && len(adv.ServiceData()) > 0 {
		serviceDataMap = make(map[string]string)
		for _, sd := range adv.ServiceData() {
			serviceDataMap[sd.UUID] = bytesToHex(sd.Data)
		}
	}

	// Convert TxPowerLevel to pointer for omitempty
	var txPower *int
	if adv.TxPowerLevel() != 0 {
		val := adv.TxPowerLevel()
		txPower = &val
	}

	jsonStruct := struct {
		Address          string            `json:"address"`
		Name             string            `json:"name"`
		RSSI             int               `json:"rssi"`
		Connectable      bool              `json:"connectable"`
		ManufacturerData string            `json:"manufacturer_data,omitempty"`
		ServiceData      map[string]string `json:"service_data,omitempty"`
		TxPower          *int              `json:"tx_power,omitempty"`
		Services         []string          `json:"services,omitempty"`
	}{
		Address:          adv.Addr(),
		Name:             adv.LocalName(),
		RSSI:             adv.RSSI(),
		Connectable:      adv.Connectable(),
		ManufacturerData: manufDataStr,
		ServiceData:      serviceDataMap,
		TxPower:          txPower,
		Services:         adv.Services(),
	}

	b, err := json.Marshal(jsonStruct)
	if err != nil {
		panic(err)
	}

	return string(b)
}

// bytesToHex converts []byte to lowercase hex string
func bytesToHex(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	hexStr := ""
	for _, b := range data {
		hexStr += string("0123456789abcdef"[b>>4]) + string("0123456789abcdef"[b&0x0f])
	}
	return hexStr
}

// DeviceToJSON converts a device. Device to JSON string
func DeviceToJSON(d device.Device) string {
	// Map Services - advertised services are now just UUIDs (no characteristics until connected)
	var services []ServiceJSON
	for _, serviceUUID := range d.AdvertisedServices() {
		services = append(services, ServiceJSON{
			UUID:            serviceUUID,            // serviceUUID is already a string
			Characteristics: []CharacteristicJSON{}, // Advertised services have no characteristics
		})
	}

	// Keep manufacturer and service data as byte arrays (closer to BLE format)
	// Convert []byte to []int to avoid base64 encoding
	var manufData interface{}
	if d.ManufacturerData() != nil {
		byteData := d.ManufacturerData()
		intData := make([]int, len(byteData))
		for i, b := range byteData {
			intData[i] = int(b)
		}
		manufData = intData
	}

	var serviceData interface{}
	if len(d.ServiceData()) > 0 {
		svcData := make(map[string][]int)
		for k, v := range d.ServiceData() {
			intData := make([]int, len(v))
			for i, b := range v {
				intData[i] = int(b)
			}
			svcData[k] = intData
		}
		serviceData = svcData
	}

	jsonStruct := DeviceJSONFull{
		ID:               d.ID(),
		Name:             d.Name(),
		Address:          d.Address(),
		RSSI:             d.RSSI(),
		TxPower:          d.TxPower(),
		Connectable:      d.IsConnectable(),
		Services:         services,
		ManufacturerData: manufData,
		ServiceData:      serviceData,
	}

	b, err := json.Marshal(jsonStruct)
	if err != nil {
		panic(err)
	}

	return string(b) // convert []byte to string
}
