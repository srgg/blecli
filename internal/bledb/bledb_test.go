package bledb

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNormalizeUUID verifies that NormalizeUUID correctly handles various UUID formats
func TestNormalizeUUID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "16-bit short form",
			input:    "180d",
			expected: "180d",
		},
		{
			name:     "16-bit with 0x prefix",
			input:    "0x180d",
			expected: "180d",
		},
		{
			name:     "Full Bluetooth SIG UUID with dashes",
			input:    "0000180d-0000-1000-8000-00805f9b34fb",
			expected: "180d",
		},
		{
			name:     "Full Bluetooth SIG UUID without dashes",
			input:    "0000180d00001000800000805f9b34fb",
			expected: "180d",
		},
		{
			name:     "Custom 128-bit UUID (not SIG base)",
			input:    "6e400001-b5a3-f393-e0a9-e50e24dcca9e",
			expected: "6e400001b5a3f393e0a9e50e24dcca9e",
		},
		{
			name:     "UUID with braces",
			input:    "{0000180d-0000-1000-8000-00805f9b34fb}",
			expected: "180d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeUUID(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestLookupServiceWithFullUUID verifies that LookupService works with both short and full UUIDs
func TestLookupServiceWithFullUUID(t *testing.T) {
	tests := []struct {
		name     string
		uuid     string
		expected string
	}{
		{
			name:     "Heart Rate - short form",
			uuid:     "180d",
			expected: "Heart Rate",
		},
		{
			name:     "Heart Rate - with 0x prefix",
			uuid:     "0x180d",
			expected: "Heart Rate",
		},
		{
			name:     "Heart Rate - full Bluetooth SIG UUID with dashes",
			uuid:     "0000180d-0000-1000-8000-00805f9b34fb",
			expected: "Heart Rate",
		},
		{
			name:     "Heart Rate - full Bluetooth SIG UUID without dashes",
			uuid:     "0000180d00001000800000805f9b34fb",
			expected: "Heart Rate",
		},
		{
			name:     "Battery Service - short form",
			uuid:     "180f",
			expected: "Battery Service",
		},
		{
			name:     "Battery Service - full UUID",
			uuid:     "0000180f-0000-1000-8000-00805f9b34fb",
			expected: "Battery Service",
		},
		{
			name:     "Unknown UUID",
			uuid:     "ffff",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := LookupService(tt.uuid)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestLookupCharacteristicWithFullUUID verifies that LookupCharacteristic works with both short and full UUIDs
func TestLookupCharacteristicWithFullUUID(t *testing.T) {
	tests := []struct {
		name     string
		uuid     string
		expected string
	}{
		{
			name:     "Heart Rate Measurement - short form",
			uuid:     "2a37",
			expected: "Heart Rate Measurement",
		},
		{
			name:     "Heart Rate Measurement - full UUID",
			uuid:     "00002a37-0000-1000-8000-00805f9b34fb",
			expected: "Heart Rate Measurement",
		},
		{
			name:     "Battery Level - short form",
			uuid:     "2a19",
			expected: "Battery Level",
		},
		{
			name:     "Battery Level - full UUID",
			uuid:     "00002a19-0000-1000-8000-00805f9b34fb",
			expected: "Battery Level",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := LookupCharacteristic(tt.uuid)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestLookupDescriptorWithFullUUID verifies that LookupDescriptor works with both short and full UUIDs
func TestLookupDescriptorWithFullUUID(t *testing.T) {
	tests := []struct {
		name     string
		uuid     string
		expected string
	}{
		{
			name:     "Client Characteristic Configuration - short form",
			uuid:     "2902",
			expected: "Client Characteristic Configuration",
		},
		{
			name:     "Client Characteristic Configuration - full UUID",
			uuid:     "00002902-0000-1000-8000-00805f9b34fb",
			expected: "Client Characteristic Configuration",
		},
		{
			name:     "Characteristic User Descriptor - short form",
			uuid:     "2901",
			expected: "Characteristic User Descriptor",
		},
		{
			name:     "Characteristic User Descriptor - full UUID",
			uuid:     "00002901-0000-1000-8000-00805f9b34fb",
			expected: "Characteristic User Descriptor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := LookupDescriptor(tt.uuid)
			assert.Equal(t, tt.expected, result)
		})
	}
}
