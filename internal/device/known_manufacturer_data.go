package device

import (
	"encoding/binary"
	"fmt"
)

const (
	// UnknownCompanyID is a sentinel value indicating the company ID should be
	// extracted from the raw manufacturer data (first 2 bytes, little-endian).
	// Use this when the manufacturer/vendor is not known in advance.
	UnknownCompanyID uint16 = 0
)

// ManufacturerDataParser parses company-specific manufacturer data
// Matches the characteristic parser pattern
type ManufacturerDataParser func([]byte) (interface{}, error)

// VendorInfo interface allows parsed manufacturer data to expose vendor information.
// Implement this interface to provide vendor ID and name to the Lua API.
type VendorInfo interface {
	VendorID() uint16
	VendorName() string
}

// manufacturerDataParsers maps company IDs to their parser functions
var manufacturerDataParsers = map[uint16]ManufacturerDataParser{
	0xFFFE: parseBlimManufacturerData, // BLIMCo (test/internal use)
}

// ParseManufacturerData parses BLE manufacturer data for a specific company.
//
// Parameters:
//   - companyID: The Bluetooth SIG assigned company identifier. If UnknownCompanyID (0),
//     the company ID will be extracted from the first 2 bytes of rawData (little-endian).
//     This is useful when the manufacturer is not known in advance.
//   - rawData: The raw manufacturer-specific data bytes
//
// Returns:
//   - Parsed manufacturer data (type depends on company), or nil for unknown companies
//   - Error if data is malformed or too short
//   - (nil, nil) for unknown/unsupported company IDs (not an error)
//
// Workaround for unknown company ID:
//
//	When a manufacturer is not known externally (from device metadata, prior knowledge, etc.),
//	pass UnknownCompanyID, and the function will attempt to extract the company ID from
//	rawData[0:2] following the BLE convention. Note: Not all manufacturers follow this
//	convention, as the manufacturer's data format is not standardized.
func ParseManufacturerData(companyID uint16, rawData []byte) (interface{}, error) {
	var id uint16

	if companyID == UnknownCompanyID {
		// Company ID unknown - attempt extraction from raw data (BLE convention)
		if len(rawData) < 2 {
			return nil, fmt.Errorf("manufacturer data too short: %d bytes", len(rawData))
		}
		id = binary.LittleEndian.Uint16(rawData[0:2])
	} else {
		// Company ID known - use provided value
		id = companyID
	}

	// Try to parse company-specific data
	parser, exists := manufacturerDataParsers[id]
	if !exists {
		// Unknown company ID, return nil (not an error)
		return nil, nil
	}

	return parser(rawData)
}

// IsParsableManufacturerData returns true if a parser exists for the company ID
func IsParsableManufacturerData(companyID uint16) bool {
	_, exists := manufacturerDataParsers[companyID]
	return exists
}

// -----------------------------------------------------------------------------
// Blim (BLIMCo) Manufacturer Data
// -----------------------------------------------------------------------------

// BlimDeviceType represents known Blim device types
type BlimDeviceType uint8

const (
	BlimDeviceTypeBLETest BlimDeviceType = 0x00 // Test peripheral with all supported BLE features
	BlimDeviceTypeIMU     BlimDeviceType = 0x01 // IMU Streamer
)

// String returns human-readable device type name
func (t BlimDeviceType) String() string {
	switch t {
	case BlimDeviceTypeBLETest:
		return "BLE Test Device"
	case BlimDeviceTypeIMU:
		return "IMU Streamer"
	default:
		return fmt.Sprintf("Unknown (0x%02X)", uint8(t))
	}
}

// BlimManufacturerData represents parsed Blim manufacturer data
//
// Format (7 bytes):
//   - Bytes 0-1: Company ID (0xFFFE = BLIMCo test/internal use)
//   - Byte 2:    Device Type (0x00 = BLE Test Device, 0x01 = IMU Streamer)
//   - Byte 3:    Hardware Version (0x10 = 1.0, high nibble = major, low nibble = minor)
//   - Bytes 4-6: Firmware Version (Major.Minor.Patch)
type BlimManufacturerData struct {
	DeviceType      BlimDeviceType
	HardwareVersion string // e.g., "1.0"
	FirmwareVersion string // e.g., "2.1.3"
}

// VendorID implements VendorInfo interface
func (b *BlimManufacturerData) VendorID() uint16 {
	return 0xFFFE // BLIMCo company ID
}

// VendorName implements VendorInfo interface
func (b *BlimManufacturerData) VendorName() string {
	return "BLIMCo"
}

// parseBlimManufacturerData parses Blim (BLIMCo) manufacturer data
func parseBlimManufacturerData(data []byte) (interface{}, error) {
	if len(data) < 7 {
		return nil, fmt.Errorf("blim manufacturer data too short: %d bytes, expected 7", len(data))
	}

	// Byte 2: Device Type
	deviceType := BlimDeviceType(data[2])

	// Byte 3: Hardware Version (high nibble = major, low nibble = minor)
	hwMajor := (data[3] >> 4) & 0x0F
	hwMinor := data[3] & 0x0F
	hardwareVersion := fmt.Sprintf("%d.%d", hwMajor, hwMinor)

	// Bytes 4-6: Firmware Version
	fwMajor := data[4]
	fwMinor := data[5]
	fwPatch := data[6]
	firmwareVersion := fmt.Sprintf("%d.%d.%d", fwMajor, fwMinor, fwPatch)

	return &BlimManufacturerData{
		DeviceType:      deviceType,
		HardwareVersion: hardwareVersion,
		FirmwareVersion: firmwareVersion,
	}, nil
}
