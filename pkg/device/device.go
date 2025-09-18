package device

import (
	"strings"
	"time"
	"unicode"

	"github.com/go-ble/ble"
)

// Device represents a discovered BLE device
type Device struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Address     string            `json:"address"`
	RSSI        int               `json:"rssi"`
	Services    []Service         `json:"services,omitempty"`
	ManufData   []byte            `json:"manufacturer_data,omitempty"`
	ServiceData map[string][]byte `json:"service_data,omitempty"`
	TxPower     *int              `json:"tx_power,omitempty"`
	Connectable bool              `json:"connectable"`
	LastSeen    time.Time         `json:"last_seen"`
}

// Service represents a GATT service
type Service struct {
	UUID            string           `json:"uuid"`
	Characteristics []Characteristic `json:"characteristics"`
}

// Characteristic represents a GATT characteristic (structure only)
type Characteristic struct {
	UUID        string       `json:"uuid"`
	Properties  string       `json:"properties"`
	Descriptors []Descriptor `json:"descriptors,omitempty"`
}

// Descriptor represents a GATT descriptor
type Descriptor struct {
	UUID string `json:"uuid"`
}

// NewDevice creates a Device from a BLE advertisement
func NewDevice(adv ble.Advertisement) *Device {
	dev := &Device{
		ID:          adv.Addr().String(),
		Name:        adv.LocalName(),
		Address:     adv.Addr().String(),
		RSSI:        adv.RSSI(),
		Services:    make([]Service, 0),
		ManufData:   adv.ManufacturerData(),
		ServiceData: make(map[string][]byte),
		Connectable: adv.Connectable(),
		LastSeen:    time.Now(),
	}

	// Convert service UUIDs into minimal Service entries (UUID only)
	for _, svc := range adv.Services() {
		dev.Services = append(dev.Services, Service{UUID: svc.String()})
	}

	// Convert service data
	for _, svcData := range adv.ServiceData() {
		dev.ServiceData[svcData.UUID.String()] = svcData.Data
	}

	// Extract TX power if available
	if adv.TxPowerLevel() != 127 { // 127 means TX power not available
		txPower := int(adv.TxPowerLevel())
		dev.TxPower = &txPower
	}

	// Try to extract name from manufacturer data if no local name
	if dev.Name == "" {
		if extractedName := dev.extractNameFromManufacturerData(adv.ManufacturerData()); extractedName != "" {
			dev.Name = extractedName
		}
	}

	return dev
}

// Update refreshes device information from a new advertisement
func (d *Device) Update(adv ble.Advertisement) {
	d.RSSI = adv.RSSI()
	d.LastSeen = time.Now()

	// Update name if it wasn't available before or changed
	if name := adv.LocalName(); name != "" {
		d.Name = name
	} else if d.Name == "" {
		// Try to extract name from manufacturer data if no local name
		if extractedName := d.extractNameFromManufacturerData(adv.ManufacturerData()); extractedName != "" {
			d.Name = extractedName
		}
	}

	// Update manufacturer data
	if manufData := adv.ManufacturerData(); len(manufData) > 0 {
		d.ManufData = manufData
	}

	// Merge advertised services (ensure UUID entries exist)
	for _, svc := range adv.Services() {
		u := svc.String()
		if !d.hasServiceUUID(u) {
			d.Services = append(d.Services, Service{UUID: u})
		}
	}

	// Update service data
	for _, svcData := range adv.ServiceData() {
		d.ServiceData[svcData.UUID.String()] = svcData.Data
	}

	// Update TX power
	if adv.TxPowerLevel() != 127 {
		txPower := int(adv.TxPowerLevel())
		d.TxPower = &txPower
	}
}

// DisplayName returns the best available name for the device
func (d *Device) DisplayName() string {
	if d.Name != "" {
		return d.Name
	}
	return d.Address
}

// IsExpired checks if the device hasn't been seen for a specified duration
func (d *Device) IsExpired(timeout time.Duration) bool {
	return time.Since(d.LastSeen) > timeout
}

// extractNameFromManufacturerData attempts to extract a device name from manufacturer data
func (d *Device) extractNameFromManufacturerData(data []byte) string {
	if len(data) < 4 {
		return ""
	}

	// Common patterns in manufacturer data that may contain device names:

	// Pattern 1: Look for readable ASCII strings longer than 3 characters
	// Many devices embed their name as ASCII text in manufacturer data
	for i := 0; i < len(data)-3; i++ {
		if isReadableASCII(data[i]) {
			// Found start of potential string, extract it
			var nameBytes []byte
			for j := i; j < len(data) && j < i+32; j++ { // Limit to 32 chars
				if isReadableASCII(data[j]) {
					nameBytes = append(nameBytes, data[j])
				} else {
					break
				}
			}

			if len(nameBytes) >= 3 { // Minimum meaningful name length
				name := strings.TrimSpace(string(nameBytes))
				if len(name) >= 3 && isValidDeviceName(name) {
					return name
				}
			}
		}
	}

	// Pattern 2: Apple iBeacon format - check for known manufacturer IDs
	if len(data) >= 2 {
		manufacturerID := uint16(data[0]) | uint16(data[1])<<8

		switch manufacturerID {
		case 0x004C: // Apple
			return d.parseAppleManufacturerData(data[2:])
		case 0x0006: // Microsoft
			return d.parseMicrosoftManufacturerData(data[2:])
		case 0x000F: // Broadcom
			return d.parseBroadcomManufacturerData(data[2:])
		}
	}

	return ""
}

// isReadableASCII checks if a byte represents a readable ASCII character
func isReadableASCII(b byte) bool {
	return b >= 32 && b <= 126 && unicode.IsPrint(rune(b))
}

// isValidDeviceName checks if a string looks like a valid device name
func isValidDeviceName(name string) bool {
	if len(name) < 3 || len(name) > 32 {
		return false
	}

	// Must contain at least one letter
	hasLetter := false
	for _, r := range name {
		if unicode.IsLetter(r) {
			hasLetter = true
			break
		}
	}

	return hasLetter
}

// parseAppleManufacturerData attempts to extract device names from Apple manufacturer data
func (d *Device) parseAppleManufacturerData(data []byte) string {
	// Apple devices sometimes include device type information
	// This is a simplified parser - real implementation would be more comprehensive
	return ""
}

// parseMicrosoftManufacturerData attempts to extract device names from Microsoft manufacturer data
func (d *Device) parseMicrosoftManufacturerData(data []byte) string {
	// Microsoft devices sometimes include device information
	return ""
}

// parseBroadcomManufacturerData attempts to extract device names from Broadcom manufacturer data
func (d *Device) parseBroadcomManufacturerData(data []byte) string {
	// Broadcom devices sometimes include device information
	return ""
}

// hasServiceUUID checks if Services already contains a service with the given UUID (case-insensitive)
func (d *Device) hasServiceUUID(uuid string) bool {
	for _, s := range d.Services {
		if strings.EqualFold(s.UUID, uuid) {
			return true
		}
	}
	return false
}
