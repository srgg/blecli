package main

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	ble "github.com/go-ble/ble"
	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/device"
	"github.com/srg/blim/internal/testutils"
	"github.com/srg/blim/internal/testutils/mocks"
	"github.com/srg/blim/pkg/config"
	"github.com/srg/blim/scanner"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

// ScanInterruptSuite tests scan interrupt behavior with proper mock setup
type ScanInterruptSuite struct {
	testutils.MockBLEPeripheralSuite
}

// SetupTest configures mock advertisements for scan tests
func (s *ScanInterruptSuite) SetupTest() {
	// Configure mock advertisements for scanning with all required fields
	adv1 := testutils.NewAdvertisementBuilder().
		WithAddress("AA:BB:CC:DD:EE:FF").
		WithName("TestDevice1").
		WithRSSI(-50).
		WithConnectable(true).
		WithManufacturerData([]byte{}).
		WithNoServiceData().
		WithServices().
		WithTxPower(0).
		Build()

	adv2 := testutils.NewAdvertisementBuilder().
		WithAddress("11:22:33:44:55:66").
		WithName("TestDevice2").
		WithRSSI(-60).
		WithConnectable(true).
		WithManufacturerData([]byte{}).
		WithNoServiceData().
		WithServices().
		WithTxPower(0).
		Build()

	s.WithAdvertisements().
		WithAdvertisements(adv1, adv2).
		Build()

	// Call parent to apply mock configuration
	s.MockBLEPeripheralSuite.SetupTest()

	// Customize the mock device to add blocking Scan behavior for interrupt testing
	// This overrides the default non-blocking behavior from PeripheralDeviceBuilder
	mockDevice := s.PeripheralBuilder.Build().(*mocks.MockDevice)

	// Remove default Scan expectation and add blocking version
	mockDevice.ExpectedCalls = nil
	mockDevice.On("Scan", mock.Anything, mock.Anything, mock.MatchedBy(func(handler ble.AdvHandler) bool {
		// Call handler for each advertisement
		handler(adv1)
		handler(adv2)
		return true
	})).Run(func(args mock.Arguments) {
		// Block until context is cancelled (for interrupt testing)
		ctx := args.Get(0).(context.Context)
		<-ctx.Done()
	}).Return(nil)

	// Update device factory with our customized device
	device.DeviceFactory = func() (ble.Device, error) {
		return mockDevice, nil
	}
}

// TestSingleScanInterrupt tests that a single scan with duration responds to SIGINT
func (s *ScanInterruptSuite) TestSingleScanInterrupt() {
	// Create configuration
	cfg := config.DefaultConfig()
	cfg.LogLevel = logrus.DebugLevel
	logger := cfg.NewLogger()

	// Create scanner
	scan, err := scanner.NewScanner(logger)
	s.Require().NoError(err, "Failed to create BLE scanner")

	// Create scan options for a longer duration to reproduce the hanging issue
	scanOpts := &scanner.ScanOptions{
		Duration:        20 * time.Second, // Use default CLI duration
		DuplicateFilter: true,
	}

	done := make(chan error, 1)
	go func() {
		done <- runSingleScan(scan, scanOpts, cfg, logger)
	}()

	// Wait longer to allow more BLE devices to be discovered (more mutex activity)
	time.Sleep(100 * time.Millisecond)

	// Send interrupt signal to ourselves
	process, _ := os.FindProcess(os.Getpid())
	process.Signal(syscall.SIGINT)

	// Wait for completion with timeout
	select {
	case err := <-done:
		if err != nil {
			s.T().Logf("Single scan returned error (expected for interrupt): %v", err)
		} else {
			s.T().Logf("Single scan completed successfully")
		}
	case <-time.After(5 * time.Second):
		s.Fail("Single scan did not complete within timeout")
	}
}

// TestWatchModeInterrupt tests that watch mode responds to SIGINT
func (s *ScanInterruptSuite) TestWatchModeInterrupt() {
	// Create configuration
	cfg := config.DefaultConfig()
	cfg.LogLevel = logrus.DebugLevel
	logger := cfg.NewLogger()

	// Create scanner
	scan, err := scanner.NewScanner(logger)
	s.Require().NoError(err, "Failed to create BLE scanner")

	// Create scan options for indefinite scan
	watchOpts := &scanner.ScanOptions{
		Duration:        0, // Indefinite
		DuplicateFilter: true,
	}

	done := make(chan error, 1)
	go func() {
		done <- runWatchMode(scan, watchOpts, cfg, logger)
	}()

	// Wait a bit, then send an interrupt
	time.Sleep(100 * time.Millisecond)

	// Send interrupt signal to ourselves
	process, _ := os.FindProcess(os.Getpid())
	process.Signal(syscall.SIGINT)

	// Wait for completion with timeout
	select {
	case err := <-done:
		if err != nil {
			s.T().Logf("Watch mode returned error (expected for interrupt): %v", err)
		} else {
			s.T().Logf("Watch mode completed successfully")
		}
	case <-time.After(5 * time.Second):
		s.Fail("Watch mode did not complete within timeout - this indicates a hang!")
	}
}

// TestWatchModeHangAfterScanFinishes tests that watch mode runs indefinitely and responds to interrupt
func (s *ScanInterruptSuite) TestWatchModeHangAfterScanFinishes() {
	// Create configuration
	cfg := config.DefaultConfig()
	cfg.LogLevel = logrus.DebugLevel
	logger := cfg.NewLogger()

	// Create scanner
	scan, err := scanner.NewScanner(logger)
	s.Require().NoError(err, "Failed to create BLE scanner")

	// Create scan options - watch mode runs indefinitely regardless of duration
	shortOpts := &scanner.ScanOptions{
		DuplicateFilter: true,
	}

	done := make(chan error, 1)
	go func() {
		done <- runWatchMode(scan, shortOpts, cfg, logger)
	}()

	// Watch mode should run indefinitely until interrupted
	// Wait at least 100ms to ensure it's running properly
	time.Sleep(100 * time.Millisecond)

	select {
	case err := <-done:
		s.Fail("Watch mode unexpectedly exited without interrupt: %v", err)
	default:
		// Good - still running as expected
		s.T().Logf("Watch mode correctly running indefinitely")
	}

	// Now interrupt it to ensure it responds properly
	process, _ := os.FindProcess(os.Getpid())
	process.Signal(syscall.SIGINT)

	// Watch mode should respond to the interrupt
	select {
	case err := <-done:
		if err != nil {
			s.T().Logf("Watch mode completed with error (expected for interrupt): %v", err)
		} else {
			s.T().Logf("Watch mode completed successfully after interrupt")
		}
	case <-time.After(5 * time.Second):
		s.Fail("Watch mode did not respond to interrupt within timeout!")
	}
}

// TestScanInterrupt is the test entry point
func TestScanInterrupt(t *testing.T) {
	suite.Run(t, new(ScanInterruptSuite))
}
