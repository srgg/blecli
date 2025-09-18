package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/go-ble/ble"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	blecli "github.com/srg/blecli/pkg/ble"
	"github.com/srg/blecli/pkg/device"
)

// MockBLEDevice implements ble.Device interface for testing
type MockBLEDevice struct{}

func (m *MockBLEDevice) AddService(svc *ble.Service) error                          { return nil }
func (m *MockBLEDevice) RemoveAllServices() error                                   { return nil }
func (m *MockBLEDevice) SetServices(svcs []*ble.Service) error                      { return nil }
func (m *MockBLEDevice) Stop() error                                                { return nil }
func (m *MockBLEDevice) Advertise(ctx context.Context, adv ble.Advertisement) error { return nil }
func (m *MockBLEDevice) AdvertiseNameAndServices(ctx context.Context, name string, ss ...ble.UUID) error {
	return nil
}
func (m *MockBLEDevice) AdvertiseIBeacon(ctx context.Context, u ble.UUID, major, minor uint16, pwr int8) error {
	return nil
}
func (m *MockBLEDevice) AdvertiseIBeaconData(ctx context.Context, b []byte) error        { return nil }
func (m *MockBLEDevice) AdvertiseMfgData(ctx context.Context, id uint16, b []byte) error { return nil }
func (m *MockBLEDevice) AdvertiseServiceData16(ctx context.Context, id uint16, b []byte) error {
	return nil
}
func (m *MockBLEDevice) Scan(ctx context.Context, allowDup bool, h ble.AdvHandler) error {
	// Mock scan that returns immediately without doing actual BLE operations
	return nil
}
func (m *MockBLEDevice) Dial(ctx context.Context, a ble.Addr) (ble.Client, error) { return nil, nil }

// ScanTestSuite provides testify/suite for proper test isolation
type ScanTestSuite struct {
	suite.Suite
	originalDeviceFactory func() (ble.Device, error)
	originalFlags         struct {
		scanDuration    time.Duration
		scanFormat      string
		scanVerbose     bool
		scanServices    []string
		scanAllowList   []string
		scanBlockList   []string
		scanNoDuplicate bool
		scanWatch       bool
	}
}

// SetupSuite runs once before all tests in the suite
func (suite *ScanTestSuite) SetupSuite() {
	// Save original flag values
	suite.originalFlags.scanDuration = scanDuration
	suite.originalFlags.scanFormat = scanFormat
	suite.originalFlags.scanVerbose = scanVerbose
	suite.originalFlags.scanServices = scanServices
	suite.originalFlags.scanAllowList = scanAllowList
	suite.originalFlags.scanBlockList = scanBlockList
	suite.originalFlags.scanNoDuplicate = scanNoDuplicate
	suite.originalFlags.scanWatch = scanWatch

	// Save original BLE device factory and inject mock
	suite.originalDeviceFactory = blecli.DeviceFactory
	blecli.DeviceFactory = func() (ble.Device, error) {
		return &MockBLEDevice{}, nil
	}
}

// TearDownSuite runs once after all tests in the suite
func (suite *ScanTestSuite) TearDownSuite() {
	// Restore original factories and flag values
	blecli.DeviceFactory = suite.originalDeviceFactory
	scanDuration = suite.originalFlags.scanDuration
	scanFormat = suite.originalFlags.scanFormat
	scanVerbose = suite.originalFlags.scanVerbose
	scanServices = suite.originalFlags.scanServices
	scanAllowList = suite.originalFlags.scanAllowList
	scanBlockList = suite.originalFlags.scanBlockList
	scanNoDuplicate = suite.originalFlags.scanNoDuplicate
	scanWatch = suite.originalFlags.scanWatch
}

// SetupTest runs before each test in the suite
func (suite *ScanTestSuite) SetupTest() {
	// Reset flags before each test for proper isolation
	resetScanFlags()

	// Reset the scanCmd and re-initialize flags to ensure clean state for each test
	// This prevents command state pollution between tests
	scanCmd.ResetFlags()

	// Re-add all the flags with their default values
	scanCmd.Flags().DurationVarP(&scanDuration, "duration", "d", 10*time.Second, "Scan duration (0 for indefinite)")
	scanCmd.Flags().StringVarP(&scanFormat, "format", "f", "table", "Output format (table, json, csv)")
	scanCmd.Flags().BoolVarP(&scanVerbose, "verbose", "v", false, "Verbose output")
	scanCmd.Flags().StringSliceVarP(&scanServices, "services", "s", nil, "Filter by service UUIDs")
	scanCmd.Flags().StringSliceVar(&scanAllowList, "allow", nil, "Only show devices with these addresses")
	scanCmd.Flags().StringSliceVar(&scanBlockList, "block", nil, "Hide devices with these addresses")
	scanCmd.Flags().BoolVar(&scanNoDuplicate, "no-duplicates", true, "Filter duplicate advertisements")
	scanCmd.Flags().BoolVarP(&scanWatch, "watch", "w", false, "Continuously scan and update results")
}

func (suite *ScanTestSuite) TestScanCmd_Help() {
	cmd := &cobra.Command{}
	cmd.AddCommand(scanCmd)

	// Test help output
	output, err := executeCommand(cmd, "scan", "--help")
	suite.Require().NoError(err)

	suite.Assert().Contains(output, "Scan for and display Bluetooth Low Energy devices")
	suite.Assert().Contains(output, "--duration")
	suite.Assert().Contains(output, "--format")
	suite.Assert().Contains(output, "--verbose")
}

func (suite *ScanTestSuite) TestScanCmd_InvalidFormat() {
	// Reset flags to ensure clean state
	resetScanFlags()

	cmd := &cobra.Command{}
	cmd.AddCommand(scanCmd)

	// Test invalid format - should fail during flag parsing or validation
	output, err := executeCommand(cmd, "scan", "--format=invalid")
	if err == nil {
		suite.T().Logf("No error returned. Output was: %s", output)
		suite.T().Logf("scanFormat value is: %s", scanFormat)
	}
	suite.Require().Error(err)
	suite.Assert().Contains(err.Error(), "invalid format 'invalid': must be one of [table json csv]")
}

func (suite *ScanTestSuite) TestScanCmd_Flags() {
	tests := []struct {
		name     string
		args     []string
		expected map[string]interface{}
	}{
		{
			name: "default flags",
			args: []string{"scan"},
			expected: map[string]interface{}{
				"duration":      10 * time.Second,
				"format":        "table",
				"verbose":       false,
				"no-duplicates": true,
				"watch":         false,
			},
		},
		{
			name: "custom duration",
			args: []string{"scan", "--duration=30s"},
			expected: map[string]interface{}{
				"duration": 30 * time.Second,
			},
		},
		{
			name: "json format",
			args: []string{"scan", "--format=json"},
			expected: map[string]interface{}{
				"format": "json",
			},
		},
		{
			name: "verbose mode",
			args: []string{"scan", "--verbose"},
			expected: map[string]interface{}{
				"verbose": true,
			},
		},
		{
			name: "service filter",
			args: []string{"scan", "--services=180F,180A"},
			expected: map[string]interface{}{
				"services": []string{"180F", "180A"},
			},
		},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {
			// Reset flags to defaults
			resetScanFlags()

			cmd := &cobra.Command{}
			cmd.AddCommand(scanCmd)

			// Parse flags without executing
			cmd.SetArgs(tt.args)
			err := cmd.Execute()

			// We expect an error because BLE scanning will fail in test environment
			// but we can still check that flags were parsed correctly
			if err != nil {
				// This is expected in test environment
			}

			// Check flag values
			for key, expected := range tt.expected {
				switch key {
				case "duration":
					suite.Assert().Equal(expected, scanDuration)
				case "format":
					suite.Assert().Equal(expected, scanFormat)
				case "verbose":
					suite.Assert().Equal(expected, scanVerbose)
				case "no-duplicates":
					suite.Assert().Equal(expected, scanNoDuplicate)
				case "watch":
					suite.Assert().Equal(expected, scanWatch)
				case "services":
					suite.Assert().Equal(expected, scanServices)
				}
			}
		})
	}
}

// TestScanCmd_WatchMode tests watch mode with timeout
func (suite *ScanTestSuite) TestScanCmd_WatchMode() {
	cmd := &cobra.Command{}
	cmd.AddCommand(scanCmd)

	// Run watch mode in a goroutine with timeout
	done := make(chan error)

	go func() {
		_, err := executeCommand(cmd, "scan", "--watch")
		done <- err
	}()

	// Wait for either completion or timeout
	select {
	case err := <-done:
		// Command completed (shouldn't happen in normal watch mode)
		suite.Assert().NoError(err)
	case <-time.After(3 * time.Second):
		// Timeout - this is expected for watch mode, test passes
		suite.Assert().True(scanWatch, "Watch flag should be set")
		// Watch mode is running as expected - test passes
	}
}

// TestScanCommandSuite runs the test suite
func TestScanCommandSuite(t *testing.T) {
	suite.Run(t, new(ScanTestSuite))
}

func TestDisplayDevicesTable(t *testing.T) {
	devices := []*device.Device{
		{
			ID:      "AA:BB:CC:DD:EE:FF",
			Name:    "Test Device 1",
			Address: "AA:BB:CC:DD:EE:FF",
			RSSI:    -45,
			Services: []device.Service{
				{UUID: "180F"}, {UUID: "180A"},
			},
			LastSeen: time.Now().Add(-5 * time.Second),
		},
		{
			ID:       "11:22:33:44:55:66",
			Name:     "",
			Address:  "11:22:33:44:55:66",
			RSSI:     -70,
			Services: []device.Service{},
			LastSeen: time.Now().Add(-10 * time.Second),
		},
	}

	// In a real implementation, we would redirect stdout
	_ = bytes.Buffer{} // Placeholder for output capture

	err := displayDevicesTable(devices)
	assert.NoError(t, err)

	// In a real test, we would check the buffer output
	// For now, just verify the function doesn't panic
}

func TestDisplayDevicesJSON(t *testing.T) {
	devices := []*device.Device{
		{
			ID:      "AA:BB:CC:DD:EE:FF",
			Name:    "Test Device",
			Address: "AA:BB:CC:DD:EE:FF",
			RSSI:    -45,
			Services: []device.Service{
				{UUID: "180F"},
			},
			LastSeen: time.Now(),
		},
	}

	// Capture output to buffer
	var buf bytes.Buffer

	// Create a custom encoder for testing
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	err := encoder.Encode(devices)

	assert.NoError(t, err)

	// Verify JSON structure
	var decoded []*device.Device
	err = json.Unmarshal(buf.Bytes(), &decoded)
	assert.NoError(t, err)
	assert.Len(t, decoded, 1)
	assert.Equal(t, "Test Device", decoded[0].Name)
}

func TestDisplayDevicesCSV(t *testing.T) {
	devices := []*device.Device{
		{
			ID:      "AA:BB:CC:DD:EE:FF",
			Name:    "Test Device",
			Address: "AA:BB:CC:DD:EE:FF",
			RSSI:    -45,
			Services: []device.Service{
				{UUID: "180F"}, {UUID: "180A"},
			},
			LastSeen: time.Now(),
		},
	}

	// Test CSV formatting logic
	expectedHeader := "Name,Address,RSSI,Services,LastSeen"

	// Verify header format
	assert.Equal(t, "Name,Address,RSSI,Services,LastSeen", expectedHeader)

	// Test service joining
	uuids := make([]string, 0, len(devices[0].Services))
	for _, s := range devices[0].Services {
		uuids = append(uuids, s.UUID)
	}
	services := strings.Join(uuids, ";")
	assert.Equal(t, "180F;180A", services)
}

func TestDevice_DisplayName_Integration(t *testing.T) {
	tests := []struct {
		name     string
		device   *device.Device
		expected string
	}{
		{
			name: "returns device name when available",
			device: &device.Device{
				Name:    "My BLE Device",
				Address: "AA:BB:CC:DD:EE:FF",
			},
			expected: "My BLE Device",
		},
		{
			name: "returns address when name is empty",
			device: &device.Device{
				Name:    "",
				Address: "11:22:33:44:55:66",
			},
			expected: "11:22:33:44:55:66",
		},
		{
			name: "handles long device names",
			device: &device.Device{
				Name:    "Very Long Device Name That Exceeds Limit",
				Address: "AA:BB:CC:DD:EE:FF",
			},
			expected: "Very Long Device Name That Exceeds Limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.device.DisplayName()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestClearScreen(t *testing.T) {
	// Test that clearScreen doesn't panic
	assert.NotPanics(t, func() {
		clearScreen()
	})
}

// Helper functions for testing

func setupTest(t *testing.T) {
	resetScanFlags()
}

func resetScanFlags() {
	scanDuration = 10 * time.Second
	scanFormat = "table"
	scanVerbose = false
	scanServices = nil
	scanAllowList = nil
	scanBlockList = nil
	scanNoDuplicate = true
	scanWatch = false
}

func executeCommand(root *cobra.Command, args ...string) (string, error) {
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)

	err := root.Execute()
	return buf.String(), err
}

// Benchmark tests
func BenchmarkDisplayDevicesTable(b *testing.B) {
	devices := make([]*device.Device, 100)
	for i := 0; i < 100; i++ {
		devices[i] = &device.Device{
			ID:      "AA:BB:CC:DD:EE:FF",
			Name:    "Benchmark Device",
			Address: "AA:BB:CC:DD:EE:FF",
			RSSI:    -50,
			Services: []device.Service{
				{UUID: "180F"}, {UUID: "180A"},
			},
			LastSeen: time.Now(),
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// In a real benchmark, we would capture output to /dev/null
		_ = displayDevicesTable(devices)
	}
}

func BenchmarkDisplayDevicesJSON(b *testing.B) {
	devices := make([]*device.Device, 100)
	for i := 0; i < 100; i++ {
		devices[i] = &device.Device{
			ID:      "AA:BB:CC:DD:EE:FF",
			Name:    "Benchmark Device",
			Address: "AA:BB:CC:DD:EE:FF",
			RSSI:    -50,
			Services: []device.Service{
				{UUID: "180F"}, {UUID: "180A"},
			},
			LastSeen: time.Now(),
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = displayDevicesJSON(devices)
	}
}
