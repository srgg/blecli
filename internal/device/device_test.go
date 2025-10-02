package device_test

import (
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/device"
	"github.com/srg/blim/internal/testutils"
	"github.com/stretchr/testify/assert"
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
			"services": [{"uuid": "180f", "characteristics": []}, {"uuid": "180a", "characteristics": []}],
			"display_name": "Test Device"
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
			"name": "",
			"rssi": -70,
			"connectable": false,
			"manufacturer_data": null,
			"service_data": null,
			"services": [],
			"tx_power": null,
			"address": "11:22:33:44:55:66",
			"display_name": "11:22:33:44:55:66"
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
	device := device.NewDevice(initialAdv, logger)
	initialAdv.AssertExpectations(t)

	initialTime := device.GetLastSeen()

	// Wait a bit to ensure time difference
	time.Sleep(10 * time.Millisecond)

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
			"connectable": true,
			"display_name": "Updated Name"
	}`)

	assert.True(t, device.GetLastSeen().After(initialTime))

	updateAdv.AssertExpectations(t)
}

func TestDevice_DisplayName(t *testing.T) {
	helper := testutils.NewTestHelper(t)

	t.Run("returns device name when available", func(t *testing.T) {
		device := testutils.CreateMockAdvertisementFromJSON(`{
			"name": "My BLE Device",
			"address": "AA:BB:CC:DD:EE:FF",
			"rssi": -50,
			"manufacturerData": null,
			"serviceData": null,
			"services": [],
			"txPower": null,
			"connectable": true
		}`).BuildDevice(helper.Logger)

		result := device.DisplayName()
		assert.Equal(t, "My BLE Device", result)
	})

	t.Run("returns address when name is empty", func(t *testing.T) {
		device := testutils.CreateMockAdvertisementFromJSON(`{
			"name": "",
			"address": "11:22:33:44:55:66",
			"rssi": -50,
			"manufacturerData": null,
			"serviceData": null,
			"services": [],
			"txPower": null,
			"connectable": true
		}`).BuildDevice(helper.Logger)

		assert.Equal(t, "11:22:33:44:55:66", device.DisplayName())
	})
}

func TestDevice_IsExpired(t *testing.T) {
	helper := testutils.NewTestHelper(t)
	now := time.Now()
	//fiveMinutesAgo := now.Add(-5 * time.Minute)

	t.Run("device is expired when lastSeen exceeds timeout", func(t *testing.T) {
		device := testutils.CreateMockAdvertisementFromJSON(`{
			"name": "Test Device",
			"address": "AA:BB:CC:DD:EE:FF",
			"rssi": -50,
			"manufacturerData": null,
			"serviceData": null,
			"services": [],
			"txPower": null,
			"connectable": true
		}`).BuildDevice(helper.Logger)

		// Update the lastSeen time to 5 minutes ago using reflection
		v := reflect.ValueOf(device).Elem()
		lastSeenField := v.FieldByName("lastSeen")
		ptrToLastSeen := unsafe.Pointer(lastSeenField.UnsafeAddr())
		realLastSeen := (*time.Time)(ptrToLastSeen)
		*realLastSeen = now.Add(-5 * time.Minute)

		// Device should expire after 3 minutes
		assert.True(t, device.IsExpired(3*time.Minute))
	})

	t.Run("device is not expired when lastSeen is within timeout", func(t *testing.T) {
		device := testutils.CreateMockAdvertisementFromJSON(`{
			"name": "Test Device",
			"address": "AA:BB:CC:DD:EE:FF",
			"rssi": -50,
			"manufacturerData": null,
			"serviceData": null,
			"services": [],
			"txPower": null,
			"connectable": true
		}`).BuildDevice(helper.Logger)

		// Device should not be expired after 10 minutes
		assert.False(t, device.IsExpired(10*time.Minute))
	})
}

//	func BenchmarkNewDevice(b *testing.B) {
//		adv := &MockAdvertisement{}
//		addr := &MockAddr{"AA:BB:CC:DD:EE:FF"}
//		uuid, _ := ble.Parse("180F")
//
//		adv.On("Addr").Return(addr)
//		adv.On("LocalName").Return("Benchmark Device")
//		adv.On("RSSI").Return(-50)
//		adv.On("ManufacturerData").Return([]byte{0x01, 0x02, 0x03, 0x04})
//		adv.On("ServiceData").Return([]ble.ServiceData{
//			{UUID: uuid, Data: []byte{0x64}},
//		})
//		adv.On("Services").Return([]ble.UUID{uuid})
//		adv.On("TxPowerLevel").Return(4)
//		adv.On("Connectable").Return(true)
//
//		b.ResetTimer()
//		for i := 0; i < b.N; i++ {
//			logger := logrus.New()
//			_ = NewDevice(adv, logger)
//		}
//	}
//
//	func BenchmarkDevice_Update(b *testing.B) {
//		// Create initial device
//		initialAdv := &MockAdvertisement{}
//		addr := &MockAddr{"AA:BB:CC:DD:EE:FF"}
//		initialAdv.On("Addr").Return(addr)
//		initialAdv.On("LocalName").Return("Device")
//		initialAdv.On("RSSI").Return(-50)
//		initialAdv.On("ManufacturerData").Return([]byte{})
//		initialAdv.On("ServiceData").Return([]ble.ServiceData{})
//		initialAdv.On("Services").Return([]ble.UUID{})
//		initialAdv.On("TxPowerLevel").Return(127)
//		initialAdv.On("Connectable").Return(true)
//
//		logger := logrus.New()
//		device := NewDevice(initialAdv, logger)
//
//		// Create update advertisement
//		updateAdv := &MockAdvertisement{}
//		updateAdv.On("LocalName").Return("Updated Device")
//		updateAdv.On("RSSI").Return(-45)
//		updateAdv.On("ManufacturerData").Return([]byte{0x01, 0x02})
//		updateAdv.On("ServiceData").Return([]ble.ServiceData{})
//		updateAdv.On("Services").Return([]ble.UUID{})
//		updateAdv.On("TxPowerLevel").Return(4)
//
//		b.ResetTimer()
//		for i := 0; i < b.N; i++ {
//			device.Update(updateAdv)
//		}
//	}
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
			dev := device.NewDevice(adv, logger)

			//bleDevice := dev.(*BLEDevice)
			//result := bleDevice.extractNameFromManufacturerData(tt.manufData)
			assert.Equal(t, tt.expectedName, dev.DisplayName())
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
			dev := device.NewDevice(adv, logger)
			assert.Equal(t, tt.expectedName, dev.DisplayName(), tt.description)
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
	dev := device.NewDevice(adv1, logger)
	device := dev
	assert.Equal(t, "Extracted", device.GetName(), "Should extract name from manufacturer data initially")

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

//func TestIsValidDeviceName(t *testing.T) {
//	tests := []struct {
//		name     string
//		input    string
//		expected bool
//	}{
//		{"valid device name", "MyDevice", true},
//		{"valid with spaces", "My Device", true},
//		{"valid with numbers", "Device123", true},
//		{"too short", "AB", false},
//		{"too long", strings.Repeat("A", 35), false},
//		{"no letters", "123456", false},
//		{"empty string", "", false},
//		{"only spaces", "   ", false},
//		{"valid minimal", "ABC", true},
//		{"valid with special chars", "Device-X_1", true},
//	}
//
//	for _, tt := range tests {
//		t.Run(tt.name, func(t *testing.T) {
//			result := device.isValidDeviceName(tt.input)
//			assert.Equal(t, tt.expected, result)
//		})
//	}
//}

//func TestIsReadableASCII(t *testing.T) {
//	tests := []struct {
//		name     string
//		input    byte
//		expected bool
//	}{
//		{"space", ' ', true},
//		{"letter A", 'A', true},
//		{"letter z", 'z', true},
//		{"number 0", '0', true},
//		{"number 9", '9', true},
//		{"special char", '!', true},
//		{"tilde", '~', true},
//		{"null", 0, false},
//		{"control char", 31, false},
//		{"delete", 127, false},
//		{"high byte", 200, false},
//	}
//
//	for _, tt := range tests {
//		t.Run(tt.name, func(t *testing.T) {
//			result := isReadableASCII(tt.input)
//			assert.Equal(t, tt.expected, result)
//		})
//	}
//}
