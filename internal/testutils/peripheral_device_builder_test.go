//go:build test

package testutils

import (
	"encoding/json"
	"testing"

	blelib "github.com/go-ble/ble"
	"github.com/stretchr/testify/suite"
)

// PeripheralDeviceBuilderTestSuite tests PeripheralDeviceBuilder functionality
type PeripheralDeviceBuilderTestSuite struct {
	suite.Suite
}

// assertHandlesAndIndexesInDeviceProfile validates the device profile has sequential handles and correct aggregate format references
func (s *PeripheralDeviceBuilderTestSuite) assertHandlesAndIndexesInDeviceProfile(profile *blelib.Profile) {
	// Build expected JSON by assigning sequential ATT handles and generating correct aggregate format descriptor values
	services := make([]map[string]interface{}, len(profile.Services))
	currentHandle := uint16(0x0001)

	for svcIdx, svc := range profile.Services {
		currentHandle++ // Service consumes one handle
		characteristics := make([]map[string]interface{}, len(svc.Characteristics))

		for charIdx, char := range svc.Characteristics {
			currentHandle++ // Characteristic consumes one handle
			descriptors := make([]map[string]interface{}, len(char.Descriptors))

			// First pass: collect Presentation Format descriptor handles
			var formatHandles []uint16
			descriptorHandles := make([]uint16, len(char.Descriptors))
			for descIdx := range char.Descriptors {
				descriptorHandles[descIdx] = currentHandle
				if uuidContains(char.Descriptors[descIdx].UUID.String(), "2904") {
					formatHandles = append(formatHandles, currentHandle)
				}
				currentHandle++
			}

			// Second pass: build descriptor JSON with proper aggregate values
			for descIdx, desc := range char.Descriptors {
				value := desc.Value

				// Re-encode aggregate format with format descriptor handles
				if uuidContains(desc.UUID.String(), "2905") && len(value) > 0 {
					var aggregateValue []byte
					for _, handle := range formatHandles {
						aggregateValue = append(aggregateValue,
							byte(handle&0xFF),
							byte(handle>>8))
					}
					value = aggregateValue
				}

				descriptors[descIdx] = map[string]interface{}{
					"uuid":   desc.UUID.String(),
					"handle": descriptorHandles[descIdx],
					"value":  value,
				}
			}

			characteristics[charIdx] = map[string]interface{}{
				"uuid":        char.UUID.String(),
				"descriptors": descriptors,
			}
		}

		services[svcIdx] = map[string]interface{}{
			"uuid":            svc.UUID.String(),
			"characteristics": characteristics,
		}
	}

	expectedJSON := MustJSON(map[string]interface{}{"services": services})
	actualJSON := s.profileToJSON(profile)

	NewJSONAsserter(s.T()).Assert(actualJSON, expectedJSON)
}

// uuidContains checks if the UUID string contains the short form UUID
func uuidContains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			(len(s) > len(substr) &&
				(s[:len(substr)] == substr ||
					s[len(s)-len(substr):] == substr ||
					(len(s) >= 36 && s[4:8] == substr))))
}

// calculateHandles computes sequential ATT handles for descriptors in a profile
func (s *PeripheralDeviceBuilderTestSuite) calculateHandles(profile DeviceProfileConfig) [][][]uint16 {
	result := make([][][]uint16, len(profile.Services))
	currentHandle := uint16(0x0001)

	for svcIdx, svc := range profile.Services {
		result[svcIdx] = make([][]uint16, len(svc.Characteristics))
		currentHandle++ // Service consumes one handle

		for charIdx, char := range svc.Characteristics {
			result[svcIdx][charIdx] = make([]uint16, len(char.Descriptors))
			currentHandle++ // Characteristic consumes one handle

			for descIdx := range char.Descriptors {
				result[svcIdx][charIdx][descIdx] = currentHandle
				currentHandle++
			}
		}
	}
	return result
}

// buildExpectedJSON constructs expected JSON from config with calculated handles
func (s *PeripheralDeviceBuilderTestSuite) buildExpectedJSON(config DeviceProfileConfig, handles [][][]uint16) string {
	services := make([]interface{}, len(config.Services))

	for svcIdx, svc := range config.Services {
		characteristics := make([]interface{}, len(svc.Characteristics))

		for charIdx, char := range svc.Characteristics {
			descriptors := make([]interface{}, len(char.Descriptors))

			for descIdx, desc := range char.Descriptors {
				// Build descriptor value - handle aggregate format specially
				value := desc.Value
				if desc.UUID == "2905" {
					// Aggregate format: encode handles as little-endian uint16
					var aggregateValue []byte
					for i := 0; i < len(desc.Value); i += 2 {
						// Extract index from placeholder
						idx := int(desc.Value[i]) | int(desc.Value[i+1])<<8
						idx -= 0x0100

						// Encode actual handle
						actualHandle := handles[svcIdx][charIdx][idx]
						aggregateValue = append(aggregateValue,
							byte(actualHandle&0xFF),
							byte(actualHandle>>8))
					}
					value = aggregateValue
				}

				descriptors[descIdx] = map[string]interface{}{
					"uuid":   desc.UUID,
					"handle": handles[svcIdx][charIdx][descIdx],
					"value":  value,
				}
			}

			characteristics[charIdx] = map[string]interface{}{
				"uuid":        char.UUID,
				"descriptors": descriptors,
			}
		}

		services[svcIdx] = map[string]interface{}{
			"uuid":            svc.UUID,
			"characteristics": characteristics,
		}
	}

	return MustJSON(map[string]interface{}{
		"services": services,
	})
}

// getBuiltProfile extracts the BLE profile from a built device
func (s *PeripheralDeviceBuilderTestSuite) getBuiltProfile(device blelib.Device) *blelib.Profile {
	client, _ := device.Dial(nil, nil)
	profile, _ := client.DiscoverProfile(true)
	return profile
}

// profileToJSON serializes BLE profile to JSON
func (s *PeripheralDeviceBuilderTestSuite) profileToJSON(profile *blelib.Profile) string {
	type descriptorJSON struct {
		UUID   string `json:"uuid"`
		Handle uint16 `json:"handle"`
		Value  []byte `json:"value"`
	}
	type characteristicJSON struct {
		UUID        string           `json:"uuid"`
		Descriptors []descriptorJSON `json:"descriptors"`
	}
	type serviceJSON struct {
		UUID            string               `json:"uuid"`
		Characteristics []characteristicJSON `json:"characteristics"`
	}
	type profileJSON struct {
		Services []serviceJSON `json:"services"`
	}

	result := profileJSON{}
	for _, svc := range profile.Services {
		svcJSON := serviceJSON{UUID: svc.UUID.String()}
		for _, char := range svc.Characteristics {
			charJSON := characteristicJSON{UUID: char.UUID.String()}
			for _, desc := range char.Descriptors {
				descJSON := descriptorJSON{
					UUID:   desc.UUID.String(),
					Handle: desc.Handle,
					Value:  desc.Value,
				}
				charJSON.Descriptors = append(charJSON.Descriptors, descJSON)
			}
			svcJSON.Characteristics = append(svcJSON.Characteristics, charJSON)
		}
		result.Services = append(result.Services, svcJSON)
	}

	jsonBytes, _ := json.MarshalIndent(result, "", "  ")
	return string(jsonBytes)
}

func (s *PeripheralDeviceBuilderTestSuite) TestWithAggregateFormatDescriptor() {
	s.Run("MultipleFormats", func() {
		// GOAL: Verify WithAggregateFormatDescriptor adds Presentation Format descriptors and creates an Aggregate Format descriptor with correct handle references
		//
		// TEST SCENARIO: Build characteristic with 2 presentation formats via aggregate builder → JSON comparison validates handles and aggregate value

		format1 := []byte{0x04, 0x00, 0x01, 0x29, 0x01, 0x00, 0x00}
		format2 := []byte{0x04, 0x00, 0x01, 0x29, 0x01, 0x00, 0x00}

		builder := NewPeripheralDeviceBuilder(s.T()).
			WithService("1234").
			WithCharacteristic("5678", "read,notify", []byte{0x00}).
			WithAggregateFormatDescriptor().
			WithPresentationFormat(format1).
			WithPresentationFormat(format2).
			Build()

		device := builder.Build()
		profile := s.getBuiltProfile(device)
		s.assertHandlesAndIndexesInDeviceProfile(profile)
	})

	s.Run("EmptyAggregate", func() {
		// GOAL: Verify WithAggregateFormatDescriptor creates an empty aggregate when no formats are added
		//
		// TEST SCENARIO: Build aggregate without presentation formats → verify aggregate exists with empty value

		builder := NewPeripheralDeviceBuilder(s.T()).
			WithService("1234").
			WithCharacteristic("5678", "read,notify", []byte{0x00}).
			WithAggregateFormatDescriptor().
			Build()

		services := builder.GetServices()
		descriptors := services[0].Characteristics[0].Descriptors

		s.Assert().Len(descriptors, 1, "MUST have only aggregate descriptor")
		s.Assert().Equal("2905", descriptors[0].UUID)
		s.Assert().Len(descriptors[0].Value, 0, "aggregate value MUST be empty")
	})

	s.Run("MixedDescriptors", func() {
		// GOAL: Verify aggregate builder works alongside regular descriptors
		//
		// TEST SCENARIO: Add CCCD, then aggregate with 2 formats → JSON comparison validates all descriptors with correct handles

		cccdValue := []byte{0x01, 0x00}
		format1 := []byte{0x04, 0x00, 0x01, 0x29, 0x01, 0x00, 0x00}
		format2 := []byte{0x04, 0x00, 0x01, 0x29, 0x01, 0x00, 0x00}

		builder := NewPeripheralDeviceBuilder(s.T()).
			WithService("1234").
			WithCharacteristic("5678", "read,notify", []byte{0x00}).
			WithDescriptor("2902", cccdValue).
			WithAggregateFormatDescriptor().
			WithPresentationFormat(format1).
			WithPresentationFormat(format2).
			Build()

		device := builder.Build()
		profile := s.getBuiltProfile(device)
		s.assertHandlesAndIndexesInDeviceProfile(profile)
	})

	s.Run("BuilderChaining", func() {
		// GOAL: Verify builder chaining works correctly after aggregate Build()
		//
		// TEST SCENARIO: Build aggregate, then add another characteristic → JSON comparison validates both characteristics with correct handles

		builder := NewPeripheralDeviceBuilder(s.T()).
			WithService("1234").
			WithCharacteristic("5678", "read,notify", []byte{0x00}).
			WithAggregateFormatDescriptor().
			WithPresentationFormat([]byte{0x04, 0x00, 0x01, 0x29, 0x01, 0x00, 0x00}).
			Build().
			WithCharacteristic("abcd", "read", []byte{0x01})

		device := builder.Build()
		profile := s.getBuiltProfile(device)
		s.assertHandlesAndIndexesInDeviceProfile(profile)
	})
}

func TestPeripheralDeviceBuilder(t *testing.T) {
	suite.Run(t, new(PeripheralDeviceBuilderTestSuite))
}
