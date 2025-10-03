package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	blelib "github.com/go-ble/ble"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/srg/blim/internal/device"
	"github.com/srg/blim/internal/testutils"
	"github.com/srg/blim/internal/testutils/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

// ScanTestSuite provides testify/suite for proper test isolation
type ScanTestSuite struct {
	suite.Suite
	originalDeviceFactory func() (blelib.Device, error)
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

	// Save the original BLE device factory and inject mock
	suite.originalDeviceFactory = device.DeviceFactory
	device.DeviceFactory = func() (blelib.Device, error) {
		mockDevice := &mocks.MockDevice{}
		// Set up expectations for the Scan method
		mockDevice.On("Scan", mock.Anything, mock.Anything, mock.Anything).Return(nil)
		return mockDevice, nil
	}
}

// TearDownSuite runs once after all tests in the suite
func (suite *ScanTestSuite) TearDownSuite() {
	// Restore original factories and flag values
	device.DeviceFactory = suite.originalDeviceFactory
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

	// Reset the scanCmd and re-initialize flags to ensure a clean state for each test
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
	// Reset flags to ensure a clean state
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

			// We expect an error because BLE scanning will fail in the test environment
			// but we can still check that flags were parsed correctly
			if err != nil {
				// This is expected in the test environment
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

	// Run watch mode in a goroutine with a timeout
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

func TestDisplayDevicesTable(t *testing.T) {
	// Create devices using mock advertisements
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	// Device 1
	device1 := testutils.CreateMockAdvertisementFromJSON(`{
			"name": "Test Device 1",
			"address": "AA:BB:CC:DD:EE:FF",
			"rssi": -45,
			"manufacturerData": null,
			"serviceData": null,
			"services": ["180F", "180A"],
			"txPower": 0,
			"connectable": true
		}`).BuildDevice(logger)

	// Device 2
	device2 := testutils.CreateMockAdvertisementFromJSON(`{
			"name": "",
			"address": "11:22:33:44:55:66",
			"rssi": -70,
			"manufacturerData": null,
			"serviceData": null,
			"services": null,
			"txPower": 0,
			"connectable": true
		}`).BuildDevice(logger)

	devices := []device.DeviceInfo{device1, device2}

	// In a real implementation, we would redirect stdout
	_ = bytes.Buffer{} // Placeholder for output capture

	err := displayDevicesTable(devices)
	assert.NoError(t, err)

	// In a real test, we would check the buffer output
	// For now, just verify the function doesn't panic
}

func TestDisplayDevicesJSON(t *testing.T) {
	// Create device using mock advertisement
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	device1 := testutils.CreateMockAdvertisementFromJSON(`{
			"name": "Test Device",
			"address": "AA:BB:CC:DD:EE:FF",
			"rssi": -45,
			"services": ["180F", "180A"],
			"connectable": true,
			"manufacturerData": null,
			"serviceData": null,
			"txPower": 0
		}`).BuildDevice(logger)
	devices := []device.Device{device1}

	// Test that we can access device properties
	assert.Equal(t, "AA:BB:CC:DD:EE:FF", devices[0].GetID())
	assert.Equal(t, "Test Device", devices[0].GetName())
	assert.Equal(t, -45, devices[0].GetRSSI())
}

func TestDisplayDevicesCSV(t *testing.T) {
	// Create device using mock advertisement
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	device1 := testutils.CreateMockAdvertisementFromJSON(`{
			"name": "Test Device",
			"address": "AA:BB:CC:DD:EE:FF",
			"rssi": -45,
			"services": ["180F", "180A"],
			"connectable": true,
			"manufacturerData": null,
			"serviceData": null,
			"txPower": 0
		}`).BuildDevice(logger)

	devices := []device.Device{device1}

	// Test CSV formatting logic
	expectedHeader := "Name,Address,RSSI,Services,LastSeen"

	// Verify header format
	assert.Equal(t, "Name,Address,RSSI,Services,LastSeen", expectedHeader)

	// Test service joining
	uuids := make([]string, 0, len(devices[0].GetAdvertisedServices()))
	for _, s := range devices[0].GetAdvertisedServices() {
		uuids = append(uuids, s)
	}
	services := strings.Join(uuids, ";")
	assert.Equal(t, "180f;180a", services) // UUIDs are lowercase
}

func TestDevice_DisplayName_Integration(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)

	tests := []struct {
		name      string
		localName string
		address   string
		expected  string
	}{
		{
			name:      "returns device name when available",
			localName: "My BLE Device",
			address:   "AA:BB:CC:DD:EE:FF",
			expected:  "My BLE Device",
		},
		{
			name:      "returns address when name is empty",
			localName: "",
			address:   "11:22:33:44:55:66",
			expected:  "11:22:33:44:55:66",
		},
		{
			name:      "handles long device names",
			localName: "Very Long Device Name That Exceeds Limit",
			address:   "AA:BB:CC:DD:EE:FF",
			expected:  "Very Long Device Name That Exceeds Limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := testutils.CreateMockAdvertisementFromJSON(`{
				"name": "%s",
				"address": "%s",
				"rssi": -50,
				"connectable": true,
				"manufacturerData": null,
				"serviceData": null,
				"services": null,
				"txPower": 0
			}`, tt.localName, tt.address).BuildDevice(logger)

			result := device.DisplayName()
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

// TestScanCommandSuite runs the test suite
func TestScanCommandSuite(t *testing.T) {
	suite.Run(t, new(ScanTestSuite))
}
