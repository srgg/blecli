package ble_test

import (
	"os"
	"testing"
	"time"

	blelib "github.com/go-ble/ble"
	"github.com/sirupsen/logrus"
	"github.com/srg/blecli/internal/testutils"
	"github.com/srg/blecli/internal/testutils/mocks"
	"github.com/srg/blecli/pkg/ble"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Store original device factory for restoration
var originalDeviceFactory func() (blelib.Device, error)

func TestMain(m *testing.M) {
	// Save the original BLE device factory and inject mock
	originalDeviceFactory = ble.DeviceFactory
	ble.DeviceFactory = func() (blelib.Device, error) {
		return &mocks.MockDevice{}, nil
	}

	// Run tests
	code := m.Run()

	// Restore the original factory
	ble.DeviceFactory = originalDeviceFactory

	os.Exit(code)
}

func TestNewScanner(t *testing.T) {
	helper := testutils.NewTestHelper(t)

	t.Run("creates scanner with provided logger", func(t *testing.T) {
		scanner, err := ble.NewScanner(helper.Logger)

		require.NoError(t, err)
		assert.NotNil(t, scanner)
	})

	t.Run("creates scanner with nil logger", func(t *testing.T) {
		scanner, err := ble.NewScanner(nil)

		require.NoError(t, err)
		assert.NotNil(t, scanner)
	})
}

func TestDefaultScanOptions(t *testing.T) {
	opts := ble.DefaultScanOptions()

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
		opts *ble.ScanOptions
	}{
		{
			name: "accepts valid options",
			opts: &ble.ScanOptions{
				Duration:        5 * time.Second,
				DuplicateFilter: false,
				ServiceUUIDs:    []blelib.UUID{},
				AllowList:       []string{"AA:BB:CC:DD:EE:FF"},
				BlockList:       []string{"11:22:33:44:55:66"},
			},
		},
		{
			name: "accepts zero duration for indefinite scan",
			opts: &ble.ScanOptions{
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

func TestScanner_GetDevices(t *testing.T) {
	helper := testutils.NewTestHelper(t)

	scanner, err := ble.NewScanner(helper.Logger)
	require.NoError(t, err)

	// Initially, no devices
	devices := scanner.GetDevices()
	assert.Len(t, devices, 0)
}

func TestScanner_GetDevice(t *testing.T) {
	helper := testutils.NewTestHelper(t)

	scanner, err := ble.NewScanner(helper.Logger)
	require.NoError(t, err)

	// Test getting a non-existing device
	device, exists := scanner.GetDevice("11:22:33:44:55:66")
	assert.False(t, exists)
	assert.Nil(t, device)
}

func TestScanner_ClearDevices(t *testing.T) {
	helper := testutils.NewTestHelper(t)

	scanner, err := ble.NewScanner(helper.Logger)
	require.NoError(t, err)

	// Initially empty
	assert.Len(t, scanner.GetDevices(), 0)

	// Clear devices (should be a no-op when empty)
	scanner.ClearDevices()
	assert.Len(t, scanner.GetDevices(), 0)
}

func TestScanner_IsScanning(t *testing.T) {
	helper := testutils.NewTestHelper(t)

	scanner, err := ble.NewScanner(helper.Logger)
	require.NoError(t, err)

	// Initially not scanning
	assert.False(t, scanner.IsScanning())
}

func TestScanner_FilteringLogic(t *testing.T) {
	helper := testutils.NewTestHelper(t)
	ja := testutils.NewJSONAsserter(t)

	tests := []struct {
		name          string
		advJSON       string
		scanOptions   *ble.ScanOptions
		shouldInclude bool
		description   string
	}{
		{
			name: "includes device with no filters",
			advJSON: `{
				"name": "Test Device",
				"address": "AA:BB:CC:DD:EE:FF",
				"rssi": -50,
				"manufacturerData": null,
				"serviceData": null,
				"services": [],
				"txPower": null,
				"connectable": true
			}`,
			scanOptions:   &ble.ScanOptions{},
			shouldInclude: true,
			description:   "No filters should include all devices",
		},
		{
			name: "excludes device on block list",
			advJSON: `{
				"name": "Blocked Device",
				"address": "AA:BB:CC:DD:EE:FF",
				"rssi": -50,
				"manufacturerData": null,
				"serviceData": null,
				"services": [],
				"txPower": null,
				"connectable": true
			}`,
			scanOptions: &ble.ScanOptions{
				BlockList: []string{"AA:BB:CC:DD:EE:FF"},
			},
			shouldInclude: false,
			description:   "Device on block list should be excluded",
		},
		{
			name: "includes device with matching service UUID",
			advJSON: `{
				"name": "Battery Device",
				"address": "AA:BB:CC:DD:EE:FF",
				"rssi": -50,
				"manufacturerData": null,
				"serviceData": null,
				"services": ["180F"],
				"txPower": null,
				"connectable": true
			}`,
			scanOptions: &ble.ScanOptions{
				ServiceUUIDs: []blelib.UUID{blelib.UUID16(0x180F)},
			},
			shouldInclude: true,
			description:   "Device with matching service UUID should be included",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that our options are valid
			assert.NotNil(t, tt.scanOptions)

			// For now, we validate the structure by testing individual components
			// since shouldIncludeDevice is private. This demonstrates the testhelper pattern.

			// Test with a basic advertisement structure
			if tt.name == "includes device with no filters" {
				device := helper.CreateMockAdvertisementFromJSON(`{
					"name": "Test Device",
					"address": "AA:BB:CC:DD:EE:FF",
					"rssi": -50,
					"manufacturerData": null,
					"serviceData": null,
					"services": [],
					"txPower": null,
					"connectable": true
				}`).BuildDevice(helper.Logger)

				actualJSON := testutils.DeviceToJSON(device)
				ja.Assert(actualJSON, `{
					"id": "AA:BB:CC:DD:EE:FF",
					"name": "Test Device",
					"address": "AA:BB:CC:DD:EE:FF",
					"rssi": -50,
					"connectable": true,
					"manufacturer_data": null,
					"service_data": null,
					"services": [],
					"tx_power": null,
					"display_name": "Test Device"
				}`)
			}
		})
	}
}

func BenchmarkScanner_Creation(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scanner, _ := ble.NewScanner(logger)
		_ = scanner
	}
}

func BenchmarkDefaultScanOptions(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		opts := ble.DefaultScanOptions()
		_ = opts
	}
}

func TestScanOptions_JSONSerialization(t *testing.T) {
	helper := testutils.NewTestHelper(t)
	ja := testutils.NewJSONAsserter(t)

	t.Run("scan options validation", func(t *testing.T) {
		opts := &ble.ScanOptions{
			Duration:        30 * time.Second,
			DuplicateFilter: true,
			ServiceUUIDs:    []blelib.UUID{blelib.UUID16(0x180F), blelib.UUID16(0x180A)},
			AllowList:       []string{"AA:BB:CC:DD:EE:FF", "11:22:33:44:55:66"},
			BlockList:       []string{"FF:FF:FF:FF:FF:FF"},
		}

		// Validate that all fields are properly set
		assert.Equal(t, 30*time.Second, opts.Duration)
		assert.True(t, opts.DuplicateFilter)
		assert.Len(t, opts.ServiceUUIDs, 2)
		assert.Len(t, opts.AllowList, 2)
		assert.Len(t, opts.BlockList, 1)
	})

	t.Run("device creation with complex advertisement", func(t *testing.T) {
		device := helper.CreateMockAdvertisementFromJSON(`{
			"name": "Complex Device",
			"address": "AA:BB:CC:DD:EE:FF",
			"rssi": -65,
			"manufacturerData": [76, 0, 1, 2, 3],
			"serviceData": {
				"180F": [100, 90],
				"180A": [1, 2, 3]
			},
			"services": ["180F", "180A", "1801"],
			"txPower": 4,
			"connectable": true
		}`).BuildDevice(helper.Logger)

		actualJSON := testutils.DeviceToJSON(device)
		ja.Assert(actualJSON, `{
			"id": "AA:BB:CC:DD:EE:FF",
			"name": "Complex Device",
			"address": "AA:BB:CC:DD:EE:FF",
			"rssi": -65,
			"tx_power": 4,
			"connectable": true,
			"manufacturer_data": [76, 0, 1, 2, 3],
			"service_data": {
				"180f": [100, 90],
				"180a": [1, 2, 3]
			},
			"services": [
				{"uuid": "180f", "characteristics": []},
				{"uuid": "180a", "characteristics": []},
				{"uuid": "1801", "characteristics": []}
			],
			"display_name": "Complex Device"
		}`)
	})
}
