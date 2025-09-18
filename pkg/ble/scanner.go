package ble

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-ble/ble"
	"github.com/go-ble/ble/darwin"
	"github.com/sirupsen/logrus"

	"github.com/srg/blecli/pkg/device"
)

// DeviceFactory creates BLE device instances - can be overridden in tests
var DeviceFactory = func() (ble.Device, error) {
	return darwin.NewDevice()
}

// Scanner handles BLE device discovery
type Scanner struct {
	devices     map[string]*device.Device
	deviceMutex sync.RWMutex
	logger      *logrus.Logger
	isScanning  bool
	scanMutex   sync.RWMutex
}

// ScanOptions configures the scanning behavior
type ScanOptions struct {
	Duration        time.Duration // How long to scan (0 = indefinite)
	DuplicateFilter bool          // Filter duplicate advertisements
	ServiceUUIDs    []ble.UUID    // Only discover devices advertising these services
	AllowList       []string      // Only include devices with these addresses
	BlockList       []string      // Exclude devices with these addresses
}

// DefaultScanOptions returns sensible default scanning options
func DefaultScanOptions() *ScanOptions {
	return &ScanOptions{
		Duration:        10 * time.Second,
		DuplicateFilter: true,
		ServiceUUIDs:    nil,
		AllowList:       nil,
		BlockList:       nil,
	}
}

// NewScanner creates a new BLE scanner
func NewScanner(logger *logrus.Logger) (*Scanner, error) {
	if logger == nil {
		logger = logrus.New()
	}

	// Initialize BLE device using factory (allows mocking in tests)
	d, err := DeviceFactory()
	if err != nil {
		return nil, fmt.Errorf("failed to create BLE device: %w", err)
	}
	ble.SetDefaultDevice(d)

	return &Scanner{
		devices: make(map[string]*device.Device),
		logger:  logger,
	}, nil
}

// Scan starts BLE device discovery
func (s *Scanner) Scan(ctx context.Context, opts *ScanOptions) error {
	if opts == nil {
		opts = DefaultScanOptions()
	}

	s.scanMutex.Lock()
	if s.isScanning {
		s.scanMutex.Unlock()
		return fmt.Errorf("scanner is already running")
	}
	s.isScanning = true
	s.scanMutex.Unlock()

	defer func() {
		s.scanMutex.Lock()
		s.isScanning = false
		s.scanMutex.Unlock()
	}()

	s.logger.Info("Starting BLE scan...")

	// Create scan context with timeout if specified
	scanCtx := ctx
	if opts.Duration > 0 {
		var cancel context.CancelFunc
		scanCtx, cancel = context.WithTimeout(ctx, opts.Duration)
		defer cancel()
	}

	// Configure scan filter
	filter := func(adv ble.Advertisement) bool {
		return s.shouldIncludeDevice(adv, opts)
	}

	// Start scanning
	err := ble.Scan(scanCtx, opts.DuplicateFilter, s.handleAdvertisement, filter)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		return fmt.Errorf("scan failed: %w", err)
	}

	s.logger.WithField("device_count", len(s.devices)).Info("BLE scan completed")
	return nil
}

// handleAdvertisement processes discovered BLE advertisements
func (s *Scanner) handleAdvertisement(adv ble.Advertisement) {
	s.deviceMutex.Lock()
	defer s.deviceMutex.Unlock()

	deviceID := adv.Addr().String()

	if existingDevice, exists := s.devices[deviceID]; exists {
		// Update existing device
		existingDevice.Update(adv)
		s.logger.WithFields(logrus.Fields{
			"device": existingDevice.DisplayName(),
			"rssi":   existingDevice.RSSI,
		}).Debug("Updated device")
	} else {
		// Create new device
		newDevice := device.NewDevice(adv)
		s.devices[deviceID] = newDevice
		s.logger.WithFields(logrus.Fields{
			"device":  newDevice.DisplayName(),
			"address": newDevice.Address,
			"rssi":    newDevice.RSSI,
		}).Info("Discovered new device")
	}
}

// shouldIncludeDevice determines if a device should be included based on filters
func (s *Scanner) shouldIncludeDevice(adv ble.Advertisement, opts *ScanOptions) bool {
	address := adv.Addr().String()

	// Check block list
	for _, blocked := range opts.BlockList {
		if address == blocked {
			return false
		}
	}

	// Check allow list (if specified)
	if len(opts.AllowList) > 0 {
		allowed := false
		for _, allowedAddr := range opts.AllowList {
			if address == allowedAddr {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}

	// Check service UUIDs (if specified)
	if len(opts.ServiceUUIDs) > 0 {
		advServices := adv.Services()
		if len(advServices) == 0 {
			return false
		}

		hasRequiredService := false
		for _, requiredUUID := range opts.ServiceUUIDs {
			for _, advUUID := range advServices {
				if requiredUUID.Equal(advUUID) {
					hasRequiredService = true
					break
				}
			}
			if hasRequiredService {
				break
			}
		}
		if !hasRequiredService {
			return false
		}
	}

	return true
}

// GetDevices returns all discovered devices
func (s *Scanner) GetDevices() []*device.Device {
	s.deviceMutex.RLock()
	defer s.deviceMutex.RUnlock()

	devices := make([]*device.Device, 0, len(s.devices))
	for _, dev := range s.devices {
		devices = append(devices, dev)
	}
	return devices
}

// GetDevice returns a specific device by ID
func (s *Scanner) GetDevice(deviceID string) (*device.Device, bool) {
	s.deviceMutex.RLock()
	defer s.deviceMutex.RUnlock()

	dev, exists := s.devices[deviceID]
	return dev, exists
}

// ClearDevices removes all discovered devices
func (s *Scanner) ClearDevices() {
	s.deviceMutex.Lock()
	defer s.deviceMutex.Unlock()

	s.devices = make(map[string]*device.Device)
	s.logger.Info("Cleared all discovered devices")
}

// IsScanning returns whether the scanner is currently active
func (s *Scanner) IsScanning() bool {
	s.scanMutex.RLock()
	defer s.scanMutex.RUnlock()
	return s.isScanning
}

// Stop stops an active scan
func (s *Scanner) Stop() error {
	s.scanMutex.RLock()
	scanning := s.isScanning
	s.scanMutex.RUnlock()

	if !scanning {
		return fmt.Errorf("scanner is not running")
	}

	err := ble.Stop()
	if err != nil {
		return fmt.Errorf("failed to stop scan: %w", err)
	}

	s.logger.Info("BLE scan stopped")
	return nil
}
