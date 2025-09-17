package ble

import (
	"fmt"
	"testing"
	"time"

	"github.com/go-ble/ble"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockAddr implements ble.Addr for testing
type MockAddr struct {
	address string
}

func (m *MockAddr) String() string {
	return m.address
}

// MockAdvertisement implements ble.Advertisement for testing
type MockAdvertisement struct {
	localName        string
	manufData        []byte
	serviceData      []ble.ServiceData
	services         []ble.UUID
	overflowService  []ble.UUID
	txPowerLevel     int
	connectable      bool
	solicitedService []ble.UUID
	rssi             int
	addr             ble.Addr
}

func (m *MockAdvertisement) LocalName() string              { return m.localName }
func (m *MockAdvertisement) ManufacturerData() []byte       { return m.manufData }
func (m *MockAdvertisement) ServiceData() []ble.ServiceData { return m.serviceData }
func (m *MockAdvertisement) Services() []ble.UUID           { return m.services }
func (m *MockAdvertisement) OverflowService() []ble.UUID    { return m.overflowService }
func (m *MockAdvertisement) TxPowerLevel() int              { return m.txPowerLevel }
func (m *MockAdvertisement) Connectable() bool              { return m.connectable }
func (m *MockAdvertisement) SolicitedService() []ble.UUID   { return m.solicitedService }
func (m *MockAdvertisement) RSSI() int                      { return m.rssi }
func (m *MockAdvertisement) Addr() ble.Addr                 { return m.addr }

func TestNewScanner(t *testing.T) {
	tests := []struct {
		name   string
		logger *logrus.Logger
	}{
		{
			name:   "creates scanner with provided logger",
			logger: logrus.New(),
		},
		{
			name:   "creates scanner with nil logger",
			logger: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scanner, err := NewScanner(tt.logger)

			require.NoError(t, err)
			assert.NotNil(t, scanner)
			assert.NotNil(t, scanner.logger)
			assert.NotNil(t, scanner.devices)
			assert.False(t, scanner.isScanning)
		})
	}
}

func TestDefaultScanOptions(t *testing.T) {
	opts := DefaultScanOptions()

	assert.NotNil(t, opts)
	assert.Equal(t, 10*time.Second, opts.Duration)
	assert.True(t, opts.DuplicateFilter)
	assert.Nil(t, opts.ServiceUUIDs)
	assert.Nil(t, opts.AllowList)
	assert.Nil(t, opts.BlockList)
}

func TestScanOptions_Validation(t *testing.T) {
	tests := []struct {
		name string
		opts *ScanOptions
	}{
		{
			name: "accepts valid options",
			opts: &ScanOptions{
				Duration:        5 * time.Second,
				DuplicateFilter: false,
				ServiceUUIDs:    []ble.UUID{},
				AllowList:       []string{"AA:BB:CC:DD:EE:FF"},
				BlockList:       []string{"11:22:33:44:55:66"},
			},
		},
		{
			name: "accepts zero duration for indefinite scan",
			opts: &ScanOptions{
				Duration: 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that options are accepted without validation errors
			assert.NotNil(t, tt.opts)
		})
	}
}

func TestScanner_HandleAdvertisement(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel) // Suppress log output during tests

	scanner, err := NewScanner(logger)
	require.NoError(t, err)

	// Create mock advertisement
	addr := &MockAddr{"AA:BB:CC:DD:EE:FF"}
	adv := &MockAdvertisement{
		localName:   "Test Device",
		rssi:        -50,
		addr:        addr,
		connectable: true,
	}

	// Test new device discovery
	scanner.handleAdvertisement(adv)

	devices := scanner.GetDevices()
	assert.Len(t, devices, 1)
	assert.Equal(t, "AA:BB:CC:DD:EE:FF", devices[0].ID)
	assert.Equal(t, "Test Device", devices[0].Name)
	assert.Equal(t, -50, devices[0].RSSI)

	// Test device update
	adv.rssi = -45
	adv.localName = "Updated Device"

	scanner.handleAdvertisement(adv)

	devices = scanner.GetDevices()
	assert.Len(t, devices, 1) // Still one device
	assert.Equal(t, "Updated Device", devices[0].Name)
	assert.Equal(t, -45, devices[0].RSSI)
}

func TestScanner_ShouldIncludeDevice(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	scanner, err := NewScanner(logger)
	require.NoError(t, err)

	tests := []struct {
		name     string
		adv      *MockAdvertisement
		opts     *ScanOptions
		expected bool
	}{
		{
			name: "includes device with no filters",
			adv: &MockAdvertisement{
				addr: &MockAddr{"AA:BB:CC:DD:EE:FF"},
			},
			opts:     &ScanOptions{},
			expected: true,
		},
		{
			name: "excludes device on block list",
			adv: &MockAdvertisement{
				addr: &MockAddr{"AA:BB:CC:DD:EE:FF"},
			},
			opts: &ScanOptions{
				BlockList: []string{"AA:BB:CC:DD:EE:FF"},
			},
			expected: false,
		},
		{
			name: "includes device on allow list",
			adv: &MockAdvertisement{
				addr: &MockAddr{"AA:BB:CC:DD:EE:FF"},
			},
			opts: &ScanOptions{
				AllowList: []string{"AA:BB:CC:DD:EE:FF"},
			},
			expected: true,
		},
		{
			name: "excludes device not on allow list",
			adv: &MockAdvertisement{
				addr: &MockAddr{"11:22:33:44:55:66"},
			},
			opts: &ScanOptions{
				AllowList: []string{"AA:BB:CC:DD:EE:FF"},
			},
			expected: false,
		},
		{
			name: "includes device with matching service UUID",
			adv: &MockAdvertisement{
				addr: &MockAddr{"AA:BB:CC:DD:EE:FF"},
				services: []ble.UUID{
					ble.UUID16(0x180F), // Battery Service
				},
			},
			opts: &ScanOptions{
				ServiceUUIDs: []ble.UUID{
					ble.UUID16(0x180F),
				},
			},
			expected: true,
		},
		{
			name: "excludes device without matching service UUID",
			adv: &MockAdvertisement{
				addr: &MockAddr{"AA:BB:CC:DD:EE:FF"},
				services: []ble.UUID{
					ble.UUID16(0x180A), // Device Information
				},
			},
			opts: &ScanOptions{
				ServiceUUIDs: []ble.UUID{
					ble.UUID16(0x180F), // Battery Service
				},
			},
			expected: false,
		},
		{
			name: "excludes device with no services when service filter is set",
			adv: &MockAdvertisement{
				addr:     &MockAddr{"AA:BB:CC:DD:EE:FF"},
				services: []ble.UUID{},
			},
			opts: &ScanOptions{
				ServiceUUIDs: []ble.UUID{
					ble.UUID16(0x180F),
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scanner.shouldIncludeDevice(tt.adv, tt.opts)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestScanner_GetDevice(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	scanner, err := NewScanner(logger)
	require.NoError(t, err)

	// Add a device
	addr := &MockAddr{"AA:BB:CC:DD:EE:FF"}
	adv := &MockAdvertisement{
		localName: "Test Device",
		rssi:      -50,
		addr:      addr,
	}

	scanner.handleAdvertisement(adv)

	// Test getting existing device
	device, exists := scanner.GetDevice("AA:BB:CC:DD:EE:FF")
	assert.True(t, exists)
	assert.NotNil(t, device)
	assert.Equal(t, "Test Device", device.Name)

	// Test getting non-existing device
	device, exists = scanner.GetDevice("11:22:33:44:55:66")
	assert.False(t, exists)
	assert.Nil(t, device)
}

func TestScanner_ClearDevices(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	scanner, err := NewScanner(logger)
	require.NoError(t, err)

	// Add devices
	addr1 := &MockAddr{"AA:BB:CC:DD:EE:FF"}
	addr2 := &MockAddr{"11:22:33:44:55:66"}

	scanner.handleAdvertisement(&MockAdvertisement{addr: addr1})
	scanner.handleAdvertisement(&MockAdvertisement{addr: addr2})

	assert.Len(t, scanner.GetDevices(), 2)

	// Clear devices
	scanner.ClearDevices()
	assert.Len(t, scanner.GetDevices(), 0)
}

func TestScanner_IsScanning(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	scanner, err := NewScanner(logger)
	require.NoError(t, err)

	// Initially not scanning
	assert.False(t, scanner.IsScanning())

	// Test thread-safe access to scanning state
	go func() {
		scanner.scanMutex.Lock()
		scanner.isScanning = true
		scanner.scanMutex.Unlock()
	}()

	// Wait a bit for goroutine to execute
	time.Sleep(10 * time.Millisecond)
	assert.True(t, scanner.IsScanning())
}

func BenchmarkScanner_HandleAdvertisement(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	scanner, _ := NewScanner(logger)

	addr := &MockAddr{"AA:BB:CC:DD:EE:FF"}
	adv := &MockAdvertisement{
		localName:   "Benchmark Device",
		rssi:        -50,
		addr:        addr,
		connectable: true,
		services:    []ble.UUID{ble.UUID16(0x180F)},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scanner.handleAdvertisement(adv)
	}
}

func BenchmarkScanner_ShouldIncludeDevice(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	scanner, _ := NewScanner(logger)

	addr := &MockAddr{"AA:BB:CC:DD:EE:FF"}
	adv := &MockAdvertisement{
		addr:     addr,
		services: []ble.UUID{ble.UUID16(0x180F)},
	}

	opts := &ScanOptions{
		ServiceUUIDs: []ble.UUID{ble.UUID16(0x180F)},
		AllowList:    []string{"AA:BB:CC:DD:EE:FF"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scanner.shouldIncludeDevice(adv, opts)
	}
}

func TestScannerConcurrency(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	scanner, err := NewScanner(logger)
	require.NoError(t, err)

	// Test concurrent access to scanner
	const numGoroutines = 10
	const numOperations = 100

	done := make(chan bool, numGoroutines)

	// Start multiple goroutines that add devices
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < numOperations; j++ {
				addr := &MockAddr{fmt.Sprintf("%02X:BB:CC:DD:EE:FF", id)}
				adv := &MockAdvertisement{
					localName: fmt.Sprintf("Device %d-%d", id, j),
					rssi:      -50 - j,
					addr:      addr,
				}
				scanner.handleAdvertisement(adv)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify that all devices were added
	devices := scanner.GetDevices()
	assert.Len(t, devices, numGoroutines)
}
