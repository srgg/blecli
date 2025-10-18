//go:build test

package device_test

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/device"
	"github.com/srg/blim/internal/devicefactory"
	"github.com/srg/blim/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

func TestNewDevice(t *testing.T) {
	helper := testutils.NewTestHelper(t)
	ja := testutils.NewJSONAsserter(t)

	t.Run("creates device with all advertisement data", func(t *testing.T) {
		device := testutils.CreateMockAdvertisementFromJSON(`{
			"name": "Test Device",
			"address": "AA:BB:CC:DD:EE:FF",
			"rssi": -45,
			"services": ["180F", "180A"],
			"manufacturerData": [76,0,1,2],
			"serviceData": {"180F":[100]},
			"txPower": 4,
			"connectable": true
		}`).BuildDevice(helper.Logger)

		actualJSON := testutils.DeviceToJSON(device)

		const expectedJSON = `{
			"id": "AA:BB:CC:DD:EE:FF",
			"name": "Test Device",
			"address": "AA:BB:CC:DD:EE:FF",
			"rssi": -45,
			"tx_power": 4,
			"connectable": true,
			"manufacturer_data": [76,0,1,2],
			"service_data": {"180f": [100]},
			"services": [{"uuid": "180a", "characteristics": []}, {"uuid": "180f", "characteristics": []}]
		}`

		ja.Assert(actualJSON, expectedJSON)
	})

	t.Run("handles missing optional data", func(t *testing.T) {
		//device := helper.createDevice("", "11:22:33:44:55:66", -70).
		//	withConnectable(false).
		//	build()
		device := testutils.CreateMockAdvertisementFromJSON(`{
			"name": null,
			"address": "11:22:33:44:55:66",
			"rssi": -70,
			"manufacturerData": null,
			"serviceData": null,
			"services": null,
			"txPower": null,
			"connectable": false
		}`).BuildDevice(helper.Logger)

		actualJSON := testutils.DeviceToJSON(device)
		ja.Assert(actualJSON, `{
			"id": "11:22:33:44:55:66",
			"name": "11:22:33:44:55:66",
			"rssi": -70,
			"connectable": false,
			"manufacturer_data": null,
			"service_data": null,
			"services": [],
			"tx_power": null,
			"address": "11:22:33:44:55:66"
		}`)
	})
}

func TestDevice_Update(t *testing.T) {
	ja := testutils.NewJSONAsserter(t)

	// Create initial device
	// Note: All BLE advertisement fields must be present because device creation
	// always calls all advertisement methods. Empty values ([], {}, null) represent
	// the default behavior when real BLE devices don't advertise that data.
	initialAdv := testutils.CreateMockAdvertisementFromJSON(`{
			"name": "Initial Name",
			"address": "AA:BB:CC:DD:EE:FF",
			"rssi": -50,
			"manufacturerData": [1],
			"serviceData": {},
			"services": [],
			"txPower": 0,
			"connectable": true
		}`).Build()

	logger := logrus.New()
	device := devicefactory.NewDeviceFromAdvertisement(initialAdv, logger)
	initialAdv.AssertExpectations(t)

	// Create update advertisement
	updateAdv := testutils.CreateMockAdvertisementFromJSON(`{
		"name": "Updated Name",
		"rssi": -40,
		"manufacturerData": [2, 3],
		"services": [],
		"serviceData": {"180F": [80]},
		"txPower": 8
	}`).Build()

	// Update device
	device.Update(updateAdv)

	// Verify updates
	//actualJSON := testutils.DeviceToJSON(device)
	ja.AssertDevice(device, `{
			"id": "AA:BB:CC:DD:EE:FF",
			"name": "Updated Name",
			"address": "AA:BB:CC:DD:EE:FF",
			"rssi": -40,
			"manufacturer_data": [2, 3],
			"service_data": {"180f": [80]},
			"services": [],
			"tx_power": 8,
			"connectable": true
	}`)

	updateAdv.AssertExpectations(t)
}

func TestDevice_ExtractNameFromManufacturerData(t *testing.T) {
	tests := []struct {
		name         string
		manufData    []byte
		expectedName string
	}{
		{
			name:         "extracts simple ASCII device name",
			manufData:    []byte{0x4C, 0x00, 'T', 'e', 's', 't', 'D', 'e', 'v', 'i', 'c', 'e'},
			expectedName: "TestDevice",
		},
		{
			name:         "extracts name with spaces",
			manufData:    []byte{0x00, 0x01, 'M', 'y', ' ', 'D', 'e', 'v', 'i', 'c', 'e'},
			expectedName: "My Device",
		},
		{
			name:         "ignores short strings",
			manufData:    []byte{0x00, 0x01, 'A', 'B'},
			expectedName: "AA:BB:CC:DD:EE:FF",
		},
		{
			name:         "ignores data without letters",
			manufData:    []byte{0x00, 0x01, '1', '2', '3', '4', '5'},
			expectedName: "AA:BB:CC:DD:EE:FF",
		},
		{
			name:         "extracts name from middle of data",
			manufData:    []byte{0x4C, 0x00, 0x01, 0x02, 'D', 'e', 'v', 'i', 'c', 'e', 'X', 0x00},
			expectedName: "DeviceX",
		},
		{
			name:         "handles empty manufacturer data",
			manufData:    []byte{},
			expectedName: "AA:BB:CC:DD:EE:FF",
		},
		{
			name:         "handles short manufacturer data",
			manufData:    []byte{0x4C},
			expectedName: "AA:BB:CC:DD:EE:FF",
		},
		{
			name:         "extracts name from real device data",
			manufData:    []byte{0x4C, 0x00, 'Z', 'c', 'm', 0x00, 0x01, 0x02},
			expectedName: "Zcm",
		},
		{
			name:         "limits name length",
			manufData:    append([]byte{0x00, 0x01}, []byte("VeryLongDeviceNameThatShouldBeLimited1234567890")...),
			expectedName: "VeryLongDeviceNameThatShouldBeLi",
		},
		{
			name:         "ignores non-printable characters",
			manufData:    []byte{0x00, 0x01, 'T', 'e', 's', 't', 0x00, 0x01, 'D', 'e', 'v'},
			expectedName: "Test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adv := testutils.CreateMockAdvertisementFromJSON(`{
				"name": null,
				"address": "AA:BB:CC:DD:EE:FF",
				"rssi": -50,
				"manufacturerData": %s,
				"serviceData": null,
				"services": [],
				"txPower": 127,
				"connectable": true
			}`, testutils.MustJSON(tt.manufData)).Build()

			logger := logrus.New()
			dev := devicefactory.NewDeviceFromAdvertisement(adv, logger)

			//bleDevice := dev.(*BLEDevice)
			//result := bleDevice.extractNameFromManufacturerData(tt.manufData)
			assert.Equal(t, tt.expectedName, dev.Name())
		})
	}
}

func TestDevice_NameResolutionPrecedence(t *testing.T) {
	tests := []struct {
		name         string
		localName    string
		manufData    []byte
		expectedName string
		description  string
	}{
		{
			name:         "LocalName takes precedence over manufacturer data",
			localName:    "OfficialName",
			manufData:    []byte{0x00, 0x01, 'M', 'a', 'n', 'u', 'f', 'N', 'a', 'm', 'e'},
			expectedName: "OfficialName",
			description:  "Local name should override manufacturer data name",
		},
		{
			name:         "Uses manufacturer data when no LocalName",
			localName:    "",
			manufData:    []byte{0x00, 0x01, 'E', 'x', 't', 'r', 'a', 'c', 't', 'e', 'd'},
			expectedName: "Extracted",
			description:  "Should extract from manufacturer data when no local name",
		},
		{
			name:         "Uses address when no name available",
			localName:    "",
			manufData:    []byte{0x00, 0x01, 0x02, 0x03}, // No readable name
			expectedName: "AA:BB:CC:DD:EE:FF",
			description:  "Should fall back to address when no name available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock advertisement
			adv := testutils.CreateMockAdvertisementFromJSON(`{
				"name": %s,
				"address": "AA:BB:CC:DD:EE:FF",
				"rssi": -50,
				"manufacturerData": %s,
				"serviceData": null,
				"services": [],
				"txPower": 127,
				"connectable": true
			}`,
				testutils.MustJSON(tt.localName),
				testutils.MustJSON(tt.manufData),
			).Build()

			logger := logrus.New()
			dev := devicefactory.NewDeviceFromAdvertisement(adv, logger)
			assert.Equal(t, tt.expectedName, dev.Name(), tt.description)
		})
	}
}

func TestDevice_NameUpdateBehavior(t *testing.T) {
	ja := testutils.NewJSONAsserter(t)

	// Create an initial device with no name
	adv1 := testutils.CreateMockAdvertisementFromJSON(`{
				"name": "",
				"address": "AA:BB:CC:DD:EE:FF",
				"rssi": -50,
				"manufacturerData": %s,
				"serviceData": null,
				"services": [],
				"txPower": 127,
				"connectable": true
			}`, testutils.MustJSON([]byte{0x00, 0x01, 'E', 'x', 't', 'r', 'a', 'c', 't', 'e', 'd'})).Build()

	logger := logrus.New()
	dev := devicefactory.NewDeviceFromAdvertisement(adv1, logger)
	device := dev
	assert.Equal(t, "Extracted", device.Name(), "Should extract name from manufacturer data initially")

	// Update with advertisement that has LocalName
	adv2 := testutils.CreateMockAdvertisementFromJSON(`{
				"name": "OfficialName",
				"rssi": -45,
				"manufacturerData": %s,
				"serviceData": null,
				"services": [],
				"txPower": 127
			}`,
		testutils.MustJSON([]byte{0x00, 0x01, 'D', 'i', 'f', 'f', 'e', 'r', 'e', 'n', 't'})).Build()

	device.Update(adv2)

	ja.AssertDevice(device, `{
		"name": "OfficialName",
		"rssi": -45
	}`)

	// Update with advertisement that has no LocalName
	adv3 := testutils.CreateMockAdvertisementFromJSON(`{
				"name": "",
				"rssi": -40,
				"manufacturerData": %s,
				"serviceData": null,
				"services": [],
				"txPower": 127
			}`,
		testutils.MustJSON([]byte{0x00, 0x01, 'N', 'e', 'w', 'N', 'a', 'm', 'e'})).Build()

	device.Update(adv3)
	ja.AssertDevice(device, `{
		"name": "OfficialName",
		"rssi": -40
	}`)
}

// DeviceErrorTestSuite tests device-level error scenarios using MockBLEPeripheralSuite
type DeviceErrorTestSuite struct {
	DeviceTestSuite
}

func (suite *DeviceErrorTestSuite) TestDeviceWriteToCharacteristicErrors() {
	// GOAL: Verify characteristic write returns appropriate errors for invalid operations
	//
	// TEST SCENARIO: Various error conditions → proper device errors returned → error types match expectations

	suite.Run("write while not connected returns ErrNotConnected", func() {
		// GOAL: Verify ErrNotConnected is returned when writing to a disconnected device
		//
		// TEST SCENARIO: Disconnect device → attempt characteristic write → ErrNotConnected returned

		// Get writable characteristic while connected
		char, err := suite.connection.GetCharacteristic("180d", "2a39")
		suite.Require().NoError(err, "MUST find characteristic")

		// Disconnect the device
		err = suite.device.Disconnect()
		suite.Require().NoError(err, "disconnect MUST succeed")

		// Attempt to write while disconnected
		err = char.Write([]byte{0x01}, true, 5*time.Second)

		suite.Assert().Error(err, "write MUST fail when not connected")
		suite.Assert().ErrorIs(err, device.ErrNotConnected, "error MUST be ErrNotConnected")
		suite.Assert().Contains(err.Error(), "2a39", "error message MUST contain characteristic UUID")
	})

	suite.Run("get non-existent characteristic returns NotFoundError", func() {
		// GOAL: Verify NotFoundError is returned for non-existent characteristic
		//
		// TEST SCENARIO: Get invalid characteristic UUID → NotFoundError returned → error identifies missing resource

		_, err := suite.connection.GetCharacteristic("180d", "ffff")

		suite.Assert().Error(err, "GetCharacteristic MUST fail for non-existent characteristic")

		var notFoundErr *device.NotFoundError
		suite.Assert().ErrorAs(err, &notFoundErr, "error MUST be NotFoundError")
		suite.Assert().Equal("characteristic", notFoundErr.Resource, "resource type MUST be 'characteristic'")
		suite.Assert().Contains(notFoundErr.UUIDs, "ffff", "UUIDs MUST contain characteristic UUID")
	})
}

// TestDeviceErrorTestSuite runs the test suite
func TestDeviceErrorTestSuite(t *testing.T) {
	suite.Run(t, new(DeviceErrorTestSuite))
}
