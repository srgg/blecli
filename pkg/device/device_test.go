package device

import (
	"strings"
	"testing"
	"time"

	"github.com/go-ble/ble"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockAdvertisement implements ble.Advertisement for testing
type MockAdvertisement struct {
	mock.Mock
}

func (m *MockAdvertisement) LocalName() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockAdvertisement) ManufacturerData() []byte {
	args := m.Called()
	return args.Get(0).([]byte)
}

func (m *MockAdvertisement) ServiceData() []ble.ServiceData {
	args := m.Called()
	return args.Get(0).([]ble.ServiceData)
}

func (m *MockAdvertisement) Services() []ble.UUID {
	args := m.Called()
	return args.Get(0).([]ble.UUID)
}

func (m *MockAdvertisement) OverflowService() []ble.UUID {
	args := m.Called()
	return args.Get(0).([]ble.UUID)
}

func (m *MockAdvertisement) TxPowerLevel() int {
	args := m.Called()
	return args.Int(0)
}

func (m *MockAdvertisement) Connectable() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockAdvertisement) SolicitedService() []ble.UUID {
	args := m.Called()
	return args.Get(0).([]ble.UUID)
}

func (m *MockAdvertisement) RSSI() int {
	args := m.Called()
	return args.Int(0)
}

func (m *MockAdvertisement) Addr() ble.Addr {
	args := m.Called()
	return args.Get(0).(ble.Addr)
}

// MockAddr implements ble.Addr for testing
type MockAddr struct {
	address string
}

func (m *MockAddr) String() string {
	return m.address
}

func TestNewDevice(t *testing.T) {
	tests := []struct {
		name      string
		setupMock func(*MockAdvertisement)
		expected  func(*Device)
	}{
		{
			name: "creates device with all advertisement data",
			setupMock: func(adv *MockAdvertisement) {
				addr := &MockAddr{"AA:BB:CC:DD:EE:FF"}
				uuid1, _ := ble.Parse("180F") // Battery Service
				uuid2, _ := ble.Parse("180A") // Device Information

				adv.On("Addr").Return(addr)
				adv.On("LocalName").Return("Test Device")
				adv.On("RSSI").Return(-45)
				adv.On("ManufacturerData").Return([]byte{0x4C, 0x00, 0x01, 0x02})
				adv.On("ServiceData").Return([]ble.ServiceData{
					{UUID: uuid1, Data: []byte{0x64}}, // Battery level 100%
				})
				adv.On("Services").Return([]ble.UUID{uuid1, uuid2})
				adv.On("TxPowerLevel").Return(4)
				adv.On("Connectable").Return(true)
			},
			expected: func(dev *Device) {
				assert.Equal(t, "AA:BB:CC:DD:EE:FF", dev.ID)
				assert.Equal(t, "Test Device", dev.Name)
				assert.Equal(t, "AA:BB:CC:DD:EE:FF", dev.Address)
				assert.Equal(t, -45, dev.RSSI)
				assert.True(t, dev.Connectable)
				assert.Equal(t, []byte{0x4C, 0x00, 0x01, 0x02}, dev.ManufData)
				assert.Len(t, dev.Services, 2)
				uuids := make([]string, 0, len(dev.Services))
				for _, s := range dev.Services {
					uuids = append(uuids, strings.ToLower(s.UUID))
				}
				assert.Contains(t, uuids, "180f")
				assert.Contains(t, uuids, "180a")
				assert.Equal(t, []byte{0x64}, dev.ServiceData["180f"])
				assert.NotNil(t, dev.TxPower)
				assert.Equal(t, 4, *dev.TxPower)
				assert.WithinDuration(t, time.Now(), dev.LastSeen, time.Second)
			},
		},
		{
			name: "handles missing optional data",
			setupMock: func(adv *MockAdvertisement) {
				addr := &MockAddr{"11:22:33:44:55:66"}
				adv.On("Addr").Return(addr)
				adv.On("LocalName").Return("")
				adv.On("RSSI").Return(-70)
				adv.On("ManufacturerData").Return([]byte{})
				adv.On("ServiceData").Return([]ble.ServiceData{})
				adv.On("Services").Return([]ble.UUID{})
				adv.On("TxPowerLevel").Return(127) // Not available
				adv.On("Connectable").Return(false)
			},
			expected: func(dev *Device) {
				assert.Equal(t, "11:22:33:44:55:66", dev.ID)
				assert.Equal(t, "", dev.Name)
				assert.Equal(t, -70, dev.RSSI)
				assert.False(t, dev.Connectable)
				assert.Empty(t, dev.ManufData)
				assert.Empty(t, dev.Services)
				assert.Empty(t, dev.ServiceData)
				assert.Nil(t, dev.TxPower)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adv := &MockAdvertisement{}
			tt.setupMock(adv)

			device := NewDevice(adv)
			tt.expected(device)

			adv.AssertExpectations(t)
		})
	}
}

func TestDevice_Update(t *testing.T) {
	// Create initial device
	initialAdv := &MockAdvertisement{}
	addr := &MockAddr{"AA:BB:CC:DD:EE:FF"}
	initialAdv.On("Addr").Return(addr)
	initialAdv.On("LocalName").Return("Initial Name")
	initialAdv.On("RSSI").Return(-50)
	initialAdv.On("ManufacturerData").Return([]byte{0x01})
	initialAdv.On("ServiceData").Return([]ble.ServiceData{})
	initialAdv.On("Services").Return([]ble.UUID{})
	initialAdv.On("TxPowerLevel").Return(0)
	initialAdv.On("Connectable").Return(true)

	device := NewDevice(initialAdv)
	initialTime := device.LastSeen

	// Wait a bit to ensure time difference
	time.Sleep(10 * time.Millisecond)

	// Create update advertisement
	updateAdv := &MockAdvertisement{}
	uuid, _ := ble.Parse("180F")
	updateAdv.On("LocalName").Return("Updated Name")
	updateAdv.On("RSSI").Return(-40)
	updateAdv.On("ManufacturerData").Return([]byte{0x02, 0x03})
	updateAdv.On("Services").Return([]ble.UUID{uuid})
	updateAdv.On("ServiceData").Return([]ble.ServiceData{
		{UUID: uuid, Data: []byte{0x50}},
	})
	updateAdv.On("TxPowerLevel").Return(8)

	// Update device
	device.Update(updateAdv)

	// Verify updates
	assert.Equal(t, "Updated Name", device.Name)
	assert.Equal(t, -40, device.RSSI)
	assert.Equal(t, []byte{0x02, 0x03}, device.ManufData)
	assert.Equal(t, []byte{0x50}, device.ServiceData["180f"])
	assert.NotNil(t, device.TxPower)
	assert.Equal(t, 8, *device.TxPower)
	assert.True(t, device.LastSeen.After(initialTime))

	updateAdv.AssertExpectations(t)
}

func TestDevice_DisplayName(t *testing.T) {
	tests := []struct {
		name         string
		deviceName   string
		address      string
		expectedName string
	}{
		{
			name:         "returns device name when available",
			deviceName:   "My BLE Device",
			address:      "AA:BB:CC:DD:EE:FF",
			expectedName: "My BLE Device",
		},
		{
			name:         "returns address when name is empty",
			deviceName:   "",
			address:      "11:22:33:44:55:66",
			expectedName: "11:22:33:44:55:66",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := &Device{
				Name:    tt.deviceName,
				Address: tt.address,
			}

			result := device.DisplayName()
			assert.Equal(t, tt.expectedName, result)
		})
	}
}

func TestDevice_IsExpired(t *testing.T) {
	now := time.Now()
	device := &Device{
		LastSeen: now.Add(-5 * time.Minute),
	}

	// Device should be expired after 3 minutes
	assert.True(t, device.IsExpired(3*time.Minute))

	// Device should not be expired after 10 minutes
	assert.False(t, device.IsExpired(10*time.Minute))
}

func BenchmarkNewDevice(b *testing.B) {
	adv := &MockAdvertisement{}
	addr := &MockAddr{"AA:BB:CC:DD:EE:FF"}
	uuid, _ := ble.Parse("180F")

	adv.On("Addr").Return(addr)
	adv.On("LocalName").Return("Benchmark Device")
	adv.On("RSSI").Return(-50)
	adv.On("ManufacturerData").Return([]byte{0x01, 0x02, 0x03, 0x04})
	adv.On("ServiceData").Return([]ble.ServiceData{
		{UUID: uuid, Data: []byte{0x64}},
	})
	adv.On("Services").Return([]ble.UUID{uuid})
	adv.On("TxPowerLevel").Return(4)
	adv.On("Connectable").Return(true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewDevice(adv)
	}
}

func BenchmarkDevice_Update(b *testing.B) {
	// Create initial device
	initialAdv := &MockAdvertisement{}
	addr := &MockAddr{"AA:BB:CC:DD:EE:FF"}
	initialAdv.On("Addr").Return(addr)
	initialAdv.On("LocalName").Return("Device")
	initialAdv.On("RSSI").Return(-50)
	initialAdv.On("ManufacturerData").Return([]byte{})
	initialAdv.On("ServiceData").Return([]ble.ServiceData{})
	initialAdv.On("Services").Return([]ble.UUID{})
	initialAdv.On("TxPowerLevel").Return(127)
	initialAdv.On("Connectable").Return(true)

	device := NewDevice(initialAdv)

	// Create update advertisement
	updateAdv := &MockAdvertisement{}
	updateAdv.On("LocalName").Return("Updated Device")
	updateAdv.On("RSSI").Return(-45)
	updateAdv.On("ManufacturerData").Return([]byte{0x01, 0x02})
	updateAdv.On("ServiceData").Return([]ble.ServiceData{})
	updateAdv.On("TxPowerLevel").Return(4)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		device.Update(updateAdv)
	}
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
			expectedName: "",
		},
		{
			name:         "ignores data without letters",
			manufData:    []byte{0x00, 0x01, '1', '2', '3', '4', '5'},
			expectedName: "",
		},
		{
			name:         "extracts name from middle of data",
			manufData:    []byte{0x4C, 0x00, 0x01, 0x02, 'D', 'e', 'v', 'i', 'c', 'e', 'X', 0x00},
			expectedName: "DeviceX",
		},
		{
			name:         "handles empty manufacturer data",
			manufData:    []byte{},
			expectedName: "",
		},
		{
			name:         "handles short manufacturer data",
			manufData:    []byte{0x4C},
			expectedName: "",
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
			// Create a device to test the method
			device := &Device{}
			result := device.extractNameFromManufacturerData(tt.manufData)
			assert.Equal(t, tt.expectedName, result)
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
			adv := &MockAdvertisement{}
			addr := &MockAddr{"AA:BB:CC:DD:EE:FF"}
			adv.On("Addr").Return(addr)
			adv.On("LocalName").Return(tt.localName)
			adv.On("RSSI").Return(-50)
			adv.On("ManufacturerData").Return(tt.manufData)
			adv.On("ServiceData").Return([]ble.ServiceData{})
			adv.On("Services").Return([]ble.UUID{})
			adv.On("TxPowerLevel").Return(127)
			adv.On("Connectable").Return(true)

			device := NewDevice(adv)
			result := device.DisplayName()
			assert.Equal(t, tt.expectedName, result, tt.description)
		})
	}
}

func TestDevice_NameUpdateBehavior(t *testing.T) {
	// Create initial device with no name
	adv1 := &MockAdvertisement{}
	addr := &MockAddr{"AA:BB:CC:DD:EE:FF"}
	adv1.On("Addr").Return(addr)
	adv1.On("LocalName").Return("")
	adv1.On("RSSI").Return(-50)
	adv1.On("ManufacturerData").Return([]byte{0x00, 0x01, 'E', 'x', 't', 'r', 'a', 'c', 't', 'e', 'd'})
	adv1.On("ServiceData").Return([]ble.ServiceData{})
	adv1.On("Services").Return([]ble.UUID{})
	adv1.On("TxPowerLevel").Return(127)
	adv1.On("Connectable").Return(true)

	device := NewDevice(adv1)
	assert.Equal(t, "Extracted", device.Name, "Should extract name from manufacturer data initially")

	// Update with advertisement that has LocalName
	adv2 := &MockAdvertisement{}
	adv2.On("LocalName").Return("OfficialName")
	adv2.On("RSSI").Return(-45)
	adv2.On("ManufacturerData").Return([]byte{0x00, 0x01, 'D', 'i', 'f', 'f', 'e', 'r', 'e', 'n', 't'})
	adv2.On("ServiceData").Return([]ble.ServiceData{})
	adv2.On("TxPowerLevel").Return(127)
	adv2.On("Services").Return([]ble.UUID{})

	device.Update(adv2)
	assert.Equal(t, "OfficialName", device.Name, "Should update to LocalName")

	// Update with advertisement that has no LocalName
	adv3 := &MockAdvertisement{}
	adv3.On("LocalName").Return("")
	adv3.On("RSSI").Return(-40)
	adv3.On("ManufacturerData").Return([]byte{0x00, 0x01, 'N', 'e', 'w', 'N', 'a', 'm', 'e'})
	adv3.On("ServiceData").Return([]ble.ServiceData{})
	adv3.On("TxPowerLevel").Return(127)
	adv3.On("Services").Return([]ble.UUID{})

	device.Update(adv3)
	assert.Equal(t, "OfficialName", device.Name, "Should keep existing name when no LocalName provided")
}

func TestIsValidDeviceName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"valid device name", "MyDevice", true},
		{"valid with spaces", "My Device", true},
		{"valid with numbers", "Device123", true},
		{"too short", "AB", false},
		{"too long", strings.Repeat("A", 35), false},
		{"no letters", "123456", false},
		{"empty string", "", false},
		{"only spaces", "   ", false},
		{"valid minimal", "ABC", true},
		{"valid with special chars", "Device-X_1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidDeviceName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsReadableASCII(t *testing.T) {
	tests := []struct {
		name     string
		input    byte
		expected bool
	}{
		{"space", ' ', true},
		{"letter A", 'A', true},
		{"letter z", 'z', true},
		{"number 0", '0', true},
		{"number 9", '9', true},
		{"special char", '!', true},
		{"tilde", '~', true},
		{"null", 0, false},
		{"control char", 31, false},
		{"delete", 127, false},
		{"high byte", 200, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isReadableASCII(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
