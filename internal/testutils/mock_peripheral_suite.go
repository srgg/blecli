//go:build test

package testutils

import (
	"testing"
	"time"

	blelib "github.com/go-ble/ble"
	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/device"
	goble "github.com/srg/blim/internal/device/go-ble"
	"github.com/stretchr/testify/suite"
)

// MockBLEPeripheralSuite provides a reusable test suite with mock BLE peripheral support.
// It follows testify/suite best practices and provides standardized BLE mocking capabilities.
//
// The suite automatically handles device factory lifecycle management and provides
// a fluent API for configuring mock BLE peripherals with services, characteristics,
// and advertisements.
//
// Basic usage (automatic setup with default battery service):
//
//	type SimpleSuite struct {
//	    testutils.MockBLEPeripheralSuite
//	}
//
//	func TestSimpleSuite(t *testing.T) {
//	    suite.Run(t, new(SimpleSuite))
//	}
//
// Custom device profile usage:
//
//	type InspectSuite struct {
//	    testutils.MockBLEPeripheralSuite
//	}
//
//	func (s *InspectSuite) SetupTest() {
//	    // Configure custom peripheral with Heart Rate service first
//	    s.WithPeripheral().
//	        WithService("180D"). // Heart Rate Service
//	        WithCharacteristic("2A37", "read,notify", []byte{80}) // 80 BPM
//
//	    s.MockBLEPeripheralSuite.SetupTest() // Call parent last to apply configuration
//	}
//
// Scanner with advertisement usage:
//
//	type ScannerSuite struct {
//	    testutils.MockBLEPeripheralSuite
//	}
//
//	func (s *ScannerSuite) SetupTest() {
//	    // Configure scan advertisements first
//	    adv1 := testutils.NewAdvertisementBuilder().
//	        WithAddress("AA:BB:CC:DD:EE:FF").WithName("HeartRate1").Build()
//	    adv2 := testutils.NewAdvertisementBuilder().
//	        WithAddress("11:22:33:44:55:66").WithName("HeartRate2").Build()
//
//	    s.WithAdvertisements().
//	        WithAdvertisements(adv1, adv2).
//	        Build()
//
//	    s.MockBLEPeripheralSuite.SetupTest() // Call parent last to apply configuration
//	}
//
// MockBLEPeripheralSuite embeds testify/suite.Suite and provides BLE-specific test utilities.
type MockBLEPeripheralSuite struct {
	suite.Suite

	// Core test utilities
	Helper *TestHelper    // Test helper with logging and assertions
	Logger *logrus.Logger // Structured logger for test output

	// BLE device factory management
	OriginalDeviceFactory func() (blelib.Device, error) // Backup of the original factory
	TestTimeout           time.Duration                 // Default timeout for BLE operations

	// Mock peripheral configuration
	PeripheralBuilder *PeripheralDeviceBuilder // Builder for configuring mock devices

	// Mock advertisements configuration
	AdvertisementsBuilder *AdvertisementArrayBuilder[[]device.Advertisement] // Builder for configuring mocked Advertisements for Scan
}

// SetupSuite initializes the test suite following testify/suite best practices.
// Called once before all tests in the suite.
func (s *MockBLEPeripheralSuite) SetupSuite() {
	s.Helper = NewTestHelper(s.T())
	s.Logger = s.Helper.Logger
	s.TestTimeout = 30 * time.Second

	// Save the original BLE device factory for restoration
	s.OriginalDeviceFactory = goble.DeviceFactory

	// Use t.Cleanup for automatic resource restoration (testify/suite best practice)
	s.T().Cleanup(func() {
		if s.OriginalDeviceFactory != nil {
			goble.DeviceFactory = s.OriginalDeviceFactory
			s.Logger.Debug("Device factory restored via t.Cleanup")
		}
	})

	s.Logger.Debug("Suite setup completed")
}

// SetupTest configures the mock device factory before each test.
// Called before each test method.
func (s *MockBLEPeripheralSuite) SetupTest() {
	if s.PeripheralBuilder == nil {
		s.PeripheralBuilder = createDefaultPeripheralBuilder(s.T())
	}

	if s.AdvertisementsBuilder != nil {
		s.PeripheralBuilder.
			WithScanAdvertisements().
			WithAdvertisements(s.AdvertisementsBuilder.Build()...).
			Build()

	}

	// Set up the default device factory
	s.OriginalDeviceFactory = goble.DeviceFactory
	goble.DeviceFactory = func() (blelib.Device, error) {
		return s.PeripheralBuilder.Build(), nil
	}

	s.Logger.Debug("Test setup completed - ready for execution")
}

// TearDownTest resets the peripheral builder after each test.
// Called after each test method.
// Note: Disconnect channel cleanup is handled automatically via t.Cleanup() registered in Build().
func (s *MockBLEPeripheralSuite) TearDownTest() {
	// Restore the device factory to prevent nil pointer panics in subsequent tests
	if s.OriginalDeviceFactory != nil {
		goble.DeviceFactory = s.OriginalDeviceFactory
	}

	// Reset peripheral builder to clean state
	s.PeripheralBuilder = nil
	s.AdvertisementsBuilder = nil
}

// TearDownSuite performs final cleanup after all tests complete.
// Device factory restoration is handled automatically via t.Cleanup().
func (s *MockBLEPeripheralSuite) TearDownSuite() {
	s.Logger.Debug("Suite teardown completed")
}

// WithPeripheral returns the peripheral builder for fluent configuration.
// Use this method to configure custom device profiles in the test setup.
func (s *MockBLEPeripheralSuite) WithPeripheral() *PeripheralDeviceBuilder {
	if s.PeripheralBuilder == nil {
		s.PeripheralBuilder = NewPeripheralDeviceBuilder(s.T())
	}

	s.Logger.Debug("Peripheral configuration started")
	return s.PeripheralBuilder
}

// WithAdvertisements returns the advertisement array builder for configuring scan advertisements.
// Use this method to set up scan advertisements in the test setup.
func (s *MockBLEPeripheralSuite) WithAdvertisements() *AdvertisementArrayBuilder[[]device.Advertisement] {

	if s.AdvertisementsBuilder == nil {
		s.AdvertisementsBuilder = NewAdvertisementArrayBuilder[[]device.Advertisement]()
	}

	s.Logger.Debug("Advertisements configuration started")
	return s.AdvertisementsBuilder
}

// createDefaultPeripheralBuilder creates a default PeripheralDeviceBuilder for testing.
// Returns a PeripheralDeviceBuilder that creates a mock peripheral with Battery Service (180F)
// and Battery Level characteristic (2A19) set to 50%.
func createDefaultPeripheralBuilder(t *testing.T) *PeripheralDeviceBuilder {
	return NewPeripheralDeviceBuilder(t).
		FromJSON(`
		{
			"services": [
				{
					"uuid": "180F",
					"characteristics": [
						{ "uuid": "2A19", "properties": "read,notify", "value": [50] }
					]
				}
			]
		}`)
}
