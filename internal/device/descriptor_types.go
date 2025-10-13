package device

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Well-known GATT descriptor UUIDs (16-bit short form, normalized without dashes)
const (
	DescriptorExtendedProperties = "2900"
	DescriptorUserDescription    = "2901"
	DescriptorClientConfig       = "2902"
	DescriptorServerConfig       = "2903"
	DescriptorPresentationFormat = "2904"
	DescriptorAggregateFormat    = "2905"
	DescriptorValidRange         = "2906"
)

// DescriptorError represents a failed descriptor value read attempt
type DescriptorError struct {
	Reason string // "timeout", "read_error", "parse_error"
	Err    error  // underlying error if available
}

func (e *DescriptorError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Reason, e.Err)
	}
	return e.Reason
}

// ExtendedProperties represents the Characteristic Extended Properties descriptor (0x2900)
type ExtendedProperties struct {
	ReliableWrite       bool // Reliable Write enabled
	WritableAuxiliaries bool // Writable Auxiliaries enabled
}

// ClientConfig represents the Client Characteristic Configuration descriptor (0x2902)
type ClientConfig struct {
	Notifications bool // Notifications enabled
	Indications   bool // Indications enabled
}

// ServerConfig represents the Server Characteristic Configuration descriptor (0x2903)
type ServerConfig struct {
	Broadcasts bool // Broadcasts enabled
}

// PresentationFormat represents the Characteristic Presentation Format descriptor (0x2904)
type PresentationFormat struct {
	Format      uint8  // Format of the characteristic value (see FormatType constants)
	Exponent    int8   // Exponent field for numeric values (value = raw * 10^Exponent)
	Unit        uint16 // Unit UUID (e.g., 0x2700 = unitless)
	Namespace   uint8  // Namespace for description (0x01 = Bluetooth SIG)
	Description uint16 // Description identifier within namespace
}

// ValidRange represents the Valid Range descriptor (0x2906)
type ValidRange struct {
	MinValue []byte // Minimum value (format depends on characteristic)
	MaxValue []byte // Maximum value (format depends on characteristic)
}

// Format types for PresentationFormat.Format field
const (
	FormatBoolean  = 0x01
	FormatUint2    = 0x02
	FormatUint4    = 0x03
	FormatUint8    = 0x04
	FormatUint12   = 0x05
	FormatUint16   = 0x06
	FormatUint24   = 0x08
	FormatUint32   = 0x08
	FormatUint48   = 0x09
	FormatUint64   = 0x0A
	FormatUint128  = 0x0B
	FormatSint8    = 0x0C
	FormatSint12   = 0x0D
	FormatSint16   = 0x0E
	FormatSint24   = 0x0F
	FormatSint32   = 0x10
	FormatSint48   = 0x11
	FormatSint64   = 0x12
	FormatSint128  = 0x13
	FormatFloat32  = 0x14
	FormatFloat64  = 0x15
	FormatSFloat16 = 0x16
	FormatFloat16  = 0x17
	FormatDuint16  = 0x18
	FormatUTF8     = 0x19
	FormatUTF16    = 0x1A
	FormatStruct   = 0x1B
)

// ParseExtendedProperties parses the Characteristic Extended Properties descriptor value.
// The descriptor is 2 bytes: bit 0 = Reliable Write, bit 1 = Writable Auxiliaries.
func ParseExtendedProperties(data []byte) (*ExtendedProperties, error) {
	if len(data) != 2 {
		return nil, fmt.Errorf("invalid length for extended properties: expected 2, got %d", len(data))
	}
	value := binary.LittleEndian.Uint16(data)
	return &ExtendedProperties{
		ReliableWrite:       (value & 0x0001) != 0,
		WritableAuxiliaries: (value & 0x0002) != 0,
	}, nil
}

// ParseClientConfig parses the Client Characteristic Configuration descriptor value.
// The descriptor is 2 bytes: bit 0 = Notifications, bit 1 = Indications.
func ParseClientConfig(data []byte) (*ClientConfig, error) {
	if len(data) != 2 {
		return nil, fmt.Errorf("invalid length for client config: expected 2, got %d", len(data))
	}
	value := binary.LittleEndian.Uint16(data)
	return &ClientConfig{
		Notifications: (value & 0x0001) != 0,
		Indications:   (value & 0x0002) != 0,
	}, nil
}

// ParseServerConfig parses the Server Characteristic Configuration descriptor value.
// The descriptor is 2 bytes: bit 0 = Broadcasts.
func ParseServerConfig(data []byte) (*ServerConfig, error) {
	if len(data) != 2 {
		return nil, fmt.Errorf("invalid length for server config: expected 2, got %d", len(data))
	}
	value := binary.LittleEndian.Uint16(data)
	return &ServerConfig{
		Broadcasts: (value & 0x0001) != 0,
	}, nil
}

// ParseUserDescription parses the Characteristic User Description descriptor value.
// The descriptor contains a UTF-8 string that may be null-terminated.
func ParseUserDescription(data []byte) (string, error) {
	if len(data) == 0 {
		return "", nil
	}

	// Remove null termination if present
	str := string(data)
	str = strings.TrimRight(str, "\x00")

	// Validate UTF-8
	if !utf8.ValidString(str) {
		return "", fmt.Errorf("invalid UTF-8 in user description")
	}

	return str, nil
}

// ParsePresentationFormat parses the Characteristic Presentation Format descriptor value.
// The descriptor is 7 bytes: Format(1), Exponent(1), Unit(2), Namespace(1), Description(2).
func ParsePresentationFormat(data []byte) (*PresentationFormat, error) {
	if len(data) != 7 {
		return nil, fmt.Errorf("invalid length for presentation format: expected 7, got %d", len(data))
	}
	return &PresentationFormat{
		Format:      data[0],
		Exponent:    int8(data[1]),
		Unit:        binary.LittleEndian.Uint16(data[2:4]),
		Namespace:   data[4],
		Description: binary.LittleEndian.Uint16(data[5:7]),
	}, nil
}

// ParseValidRange parses the Valid Range descriptor value.
// The format depends on the characteristic value format.
// This function splits the data in half for min/max values.
func ParseValidRange(data []byte) (*ValidRange, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("invalid length for valid range: expected at least 2, got %d", len(data))
	}

	// The range is typically split evenly between min and max
	// For odd lengths, give the extra byte to max
	mid := len(data) / 2

	minValue := make([]byte, mid)
	maxValue := make([]byte, len(data)-mid)
	copy(minValue, data[:mid])
	copy(maxValue, data[mid:])

	return &ValidRange{
		MinValue: minValue,
		MaxValue: maxValue,
	}, nil
}

// ParseDescriptorValue parses a descriptor value based on its UUID.
// Returns the parsed value for well-known descriptors, or raw []byte for unknown descriptors.
// Returns (nil, nil) for empty data.
func ParseDescriptorValue(uuid string, data []byte) (interface{}, error) {
	// Early return for empty data - nothing to parse
	if len(data) == 0 {
		return nil, nil
	}

	// Normalize UUID for comparison (remove dashes, lowercase)
	normalizedUUID := NormalizeUUID(uuid)

	switch normalizedUUID {
	case DescriptorExtendedProperties:
		return ParseExtendedProperties(data)
	case DescriptorUserDescription:
		return ParseUserDescription(data)
	case DescriptorClientConfig:
		return ParseClientConfig(data)
	case DescriptorServerConfig:
		return ParseServerConfig(data)
	case DescriptorPresentationFormat:
		return ParsePresentationFormat(data)
	case DescriptorValidRange:
		return ParseValidRange(data)
	default:
		// Unknown descriptor UUID, return raw data
		return data, nil
	}
}
