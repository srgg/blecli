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

// bleScanningDeviceMock wraps ble.Device to implement device.Scanner for tests
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

// createTestLogger creates a configured logger for tests
func (s *ScanInterruptSuite) createTestLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: time.RFC3339,
	})
	return logger
}

// createTestScanner creates a scanner with test logger
func (s *ScanInterruptSuite) createTestScanner() *scanner.Scanner {
	scan, err := scanner.NewScanner(s.createTestLogger())
	s.Require().NoError(err, "scanner creation MUST succeed")
	return scan
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

	// Wrap in a custom mock that blocks until context is canceled
	blockingDevice := &blockingScanDevice{
		Device: bleDevice,
		adv1:   adv1,
		adv2:   adv2,
	}

	// Update the device factory with the blocking device
	devicefactory.DeviceFactory = func() (device.Scanner, error) {
		return blockingDevice, nil
	}
}

// blockingScanDevice wraps a BLE device to make a Scan block until the context is canceled
// This is needed for interrupt testing
type blockingScanDevice struct {
	blelib.Device
	adv1, adv2 device.Advertisement
}

// Scan sends advertisements, then blocks until context is canceled
func (b *blockingScanDevice) Scan(ctx context.Context, allowDup bool, handler func(device.Advertisement)) error {
	// Send the advertisements first
	handler(b.adv1)
	handler(b.adv2)

	// Then block until context is canceled (simulating ongoing scan)
	<-ctx.Done()
	return ctx.Err()
}

// hangingScanDevice simulates Bluetooth being disabled mid-scan by emitting one ad then hanging
type hangingScanDevice struct {
	adv device.Advertisement
}

// Scan emits one advertisement then returns Bluetooth error (simulating Bluetooth disabled)
func (h *hangingScanDevice) Scan(ctx context.Context, allowDup bool, handler func(device.Advertisement)) error {
	// Emit one advertisement (scan starts working)
	handler(h.adv)

	// Then return Bluetooth error (simulating Bluetooth disabled mid-scan)
	time.Sleep(10 * time.Millisecond) // Small delay to simulate the scan working briefly
	return device.ErrBluetoothOff
}

// TestSingleScanInterrupt tests that a single scan with duration responds to SIGINT
func (s *ScanInterruptSuite) TestSingleScanInterrupt() {
	// GOAL: Verify single scan with duration exits cleanly on SIGINT
	//
	// TEST SCENARIO: Start timed scan → send SIGINT after 100ms → scan completes within 5s

	logger := s.createTestLogger()
	scan := s.createTestScanner()

	cfg := &scanConfig{
		scanTimeout:  20 * time.Second,
		outputFormat: "table",
	}

	scanOpts := &scanner.ScanOptions{
		Duration:        20 * time.Second,
		DuplicateFilter: true,
	}

	done := make(chan error, 1)
	go func() {
		done <- runSingleScan(scan, scanOpts, cfg, logger)
	}()

	time.Sleep(100 * time.Millisecond)

	process, _ := os.FindProcess(os.Getpid())
	process.Signal(syscall.SIGINT)

	select {
	case <-done:
		// Test passes - scan completed (either with or without error is acceptable for interrupt)
	case <-time.After(5 * time.Second):
		s.Fail("single scan MUST complete within 5s after SIGINT")
	}
}

// TestWatchModeInterrupt tests that watch mode responds to SIGINT
func (s *ScanInterruptSuite) TestWatchModeInterrupt() {
	// GOAL: Verify watch mode exits cleanly on SIGINT without hanging
	//
	// TEST SCENARIO: Start watch mode → send SIGINT after 100ms → watch mode completes within 5s

	logger := s.createTestLogger()
	scan := s.createTestScanner()

	cfg := &scanConfig{
		scanTimeout:  0,
		outputFormat: "table",
	}

	watchOpts := &scanner.ScanOptions{
		Duration:        0,
		DuplicateFilter: true,
	}

	done := make(chan error, 1)
	go func() {
		done <- runWatchMode(scan, watchOpts, cfg, logger)
	}()

	time.Sleep(100 * time.Millisecond)

	process, _ := os.FindProcess(os.Getpid())
	process.Signal(syscall.SIGINT)

	select {
	case <-done:
		// Test passes - watch mode completed (either with or without error is acceptable for interrupt)
	case <-time.After(5 * time.Second):
		s.Fail("watch mode MUST complete within 5s after SIGINT")
	}
}

// TestWatchModeHangAfterScanFinishes tests that watch mode runs indefinitely and responds to an interrupt
func (s *ScanInterruptSuite) TestWatchModeHangAfterScanFinishes() {
	// GOAL: Verify watch mode runs indefinitely until interrupted
	//
	// TEST SCENARIO: Start watch mode → verify still running after 100ms → send SIGINT → completes within 5s

	logger := s.createTestLogger()
	scan := s.createTestScanner()

	cfg := &scanConfig{
		scanTimeout:  0,
		outputFormat: "table",
	}

	shortOpts := &scanner.ScanOptions{
		DuplicateFilter: true,
	}

	done := make(chan error, 1)
	go func() {
		done <- runWatchMode(scan, shortOpts, cfg, logger)
	}()

	time.Sleep(100 * time.Millisecond)

	select {
	case err := <-done:
		s.Fail("watch mode MUST NOT exit without interrupt: %v", err)
	default:
		// Expected - still running
	}

	process, _ := os.FindProcess(os.Getpid())
	process.Signal(syscall.SIGINT)

	select {
	case <-done:
		// Test passes - watch mode completed after interrupt
	case <-time.After(5 * time.Second):
		s.Fail("watch mode MUST complete within 5s after SIGINT")
	}
}

// TestWatchModeBluetoothDisabled verifies watch mode detects stalled scans when Bluetooth is disabled
func (s *ScanInterruptSuite) TestWatchModeBluetoothDisabled() {
	// GOAL: Verify watch mode exits with error when Bluetooth disabled mid-scan
	//
	// TEST SCENARIO: Bluetooth disabled during scan → returns ErrBluetoothOff → watch mode exits with error

	adv := testutils.NewAdvertisementBuilder().
		WithAddress("AA:BB:CC:DD:EE:FF").
		WithName("TestDevice").
		WithRSSI(-50).
		WithConnectable(true).
		WithManufacturerData([]byte{}).
		WithNoServiceData().
		WithServices().
		WithTxPower(0).
		Build()

	hangingDev := &hangingScanDevice{adv: adv}

	devicefactory.DeviceFactory = func() (device.Scanner, error) {
		return hangingDev, nil
	}

	logger := s.createTestLogger()
	scan := s.createTestScanner()

	cfg := &scanConfig{
		scanTimeout:  0,
		outputFormat: "table",
	}

	watchOpts := &scanner.ScanOptions{
		Duration:        0,
		DuplicateFilter: true,
	}

	done := make(chan error, 1)
	go func() {
		done <- runWatchMode(scan, watchOpts, cfg, logger)
	}()

	time.Sleep(100 * time.Millisecond)

	select {
	case err := <-done:
		s.Assert().Error(err, "watch mode MUST return error when Bluetooth disabled")
		s.Assert().ErrorIs(err, device.ErrBluetoothOff, "error MUST be device.ErrBluetoothOff")
	case <-time.After(500 * time.Millisecond):
		s.Fail("watch mode MUST exit within 500ms when Bluetooth disabled")
	}
}

// TestScanInterrupt is the test entry point
func TestScanInterrupt(t *testing.T) {
	suite.Run(t, new(ScanInterruptSuite))
}
