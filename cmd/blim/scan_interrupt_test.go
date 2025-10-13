package main

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	blelib "github.com/go-ble/ble"
	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/device"
	goble "github.com/srg/blim/internal/device/go-ble"
	"github.com/srg/blim/internal/devicefactory"
	"github.com/srg/blim/internal/testutils"
	"github.com/srg/blim/scanner"
	"github.com/stretchr/testify/suite"
)

// bleScanningDeviceMock wraps ble.Device to implement device.ScanningDevice for tests
type bleScanningDeviceMock struct {
	blelib.Device
}

// Scan adapts ble.Device.Scan to use device.Advertisement
func (s *bleScanningDeviceMock) Scan(ctx context.Context, allowDup bool, handler func(device.Advertisement)) error {
	bleHandler := func(adv blelib.Advertisement) {
		handler(goble.NewBLEAdvertisement(adv))
	}
	return s.Device.Scan(ctx, allowDup, bleHandler)
}

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

	// The PeripheralDeviceBuilder creates a mock device, but we need to customize it
	// to add blocking Scan behavior for interrupt testing
	bleDevice := s.PeripheralBuilder.Build()

	// Wrap in a custom mock that blocks until context is cancelled
	blockingDevice := &blockingScanDevice{
		Device: bleDevice,
		adv1:   adv1,
		adv2:   adv2,
	}

	// Update the device factory with the blocking device
	devicefactory.DeviceFactory = func() (device.ScanningDevice, error) {
		return blockingDevice, nil
	}
}

// blockingScanDevice wraps a BLE device to make Scan block until context is cancelled
// This is needed for interrupt testing
type blockingScanDevice struct {
	blelib.Device
	adv1, adv2 device.Advertisement
}

// Scan sends advertisements then blocks until context is cancelled
func (b *blockingScanDevice) Scan(ctx context.Context, allowDup bool, handler func(device.Advertisement)) error {
	// Send the advertisements first
	handler(b.adv1)
	handler(b.adv2)

	// Then block until context is cancelled (simulating ongoing scan)
	<-ctx.Done()
	return ctx.Err()
}

// TestSingleScanInterrupt tests that a single scan with duration responds to SIGINT
func (s *ScanInterruptSuite) TestSingleScanInterrupt() {
	// Create logger
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: time.RFC3339,
	})

	// Create configuration
	cfg := &scanConfig{
		scanTimeout:  20 * time.Second,
		outputFormat: "table",
	}

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
	// Create logger
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: time.RFC3339,
	})

	// Create configuration
	cfg := &scanConfig{
		scanTimeout:  0,
		outputFormat: "table",
	}

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

// TestWatchModeHangAfterScanFinishes tests that watch mode runs indefinitely and responds to an interrupt
func (s *ScanInterruptSuite) TestWatchModeHangAfterScanFinishes() {
	// Create logger
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: time.RFC3339,
	})

	// Create configuration
	cfg := &scanConfig{
		scanTimeout:  0,
		outputFormat: "table",
	}

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
