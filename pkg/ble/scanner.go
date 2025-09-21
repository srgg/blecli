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

// DeviceFactory creates BLE device instances (can be overridden in tests)
var DeviceFactory = func() (ble.Device, error) {
	return darwin.NewDevice()
}

// Scanner handles BLE device discovery
type Scanner struct {
	devices     map[string]device.Device
	deviceMutex sync.RWMutex
	logger      *logrus.Logger
	isScanning  bool
	scanMutex   sync.RWMutex
}

// ScanOptions configures scanning behavior
type ScanOptions struct {
	Duration        time.Duration
	DuplicateFilter bool
	ServiceUUIDs    []ble.UUID
	AllowList       []string
	BlockList       []string
}

// DefaultScanOptions returns default scanning options
func DefaultScanOptions() *ScanOptions {
	return &ScanOptions{
		Duration:        10 * time.Second,
		DuplicateFilter: true,
	}
}

// NewScanner creates a new BLE scanner
func NewScanner(logger *logrus.Logger) (*Scanner, error) {
	if logger == nil {
		logger = logrus.New()
	}

	d, err := DeviceFactory()
	if err != nil {
		return nil, fmt.Errorf("failed to create BLE device: %w", err)
	}
	ble.SetDefaultDevice(d)

	return &Scanner{
		devices: make(map[string]device.Device),
		logger:  logger,
	}, nil
}

// Scan starts BLE discovery with provided options
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

	scanCtx := ctx
	if opts.Duration > 0 {
		var cancel context.CancelFunc
		scanCtx, cancel = context.WithTimeout(ctx, opts.Duration)
		defer cancel()
	}

	filter := func(adv ble.Advertisement) bool {
		return s.shouldIncludeDevice(adv, opts)
	}

	err := ble.Scan(scanCtx, opts.DuplicateFilter, s.handleAdvertisement, filter)
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		return fmt.Errorf("scan failed: %w", err)
	}

	s.logger.WithField("device_count", len(s.devices)).Info("BLE scan completed")
	return nil
}

// handleAdvertisement updates existing or adds new device
func (s *Scanner) handleAdvertisement(adv ble.Advertisement) {
	s.deviceMutex.Lock()
	defer s.deviceMutex.Unlock()

	deviceID := adv.Addr().String()

	if existing, ok := s.devices[deviceID]; ok {
		existing.Update(adv)
		s.logger.WithFields(logrus.Fields{
			"device": existing.DisplayName(),
			"rssi":   existing.GetRSSI(),
		}).Debug("Updated device")
	} else {
		newDev := device.NewDevice(adv, nil)
		s.devices[deviceID] = newDev
		s.logger.WithFields(logrus.Fields{
			"device":  newDev.DisplayName(),
			"address": newDev.GetAddress(),
			"rssi":    newDev.GetRSSI(),
		}).Info("Discovered new device")
	}
}

// shouldIncludeDevice applies allow/block/service filters
func (s *Scanner) shouldIncludeDevice(adv ble.Advertisement, opts *ScanOptions) bool {
	addr := adv.Addr().String()

	for _, blocked := range opts.BlockList {
		if addr == blocked {
			return false
		}
	}

	if len(opts.AllowList) > 0 {
		allowed := false
		for _, a := range opts.AllowList {
			if addr == a {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}

	if len(opts.ServiceUUIDs) > 0 {
		hasRequired := false
		for _, required := range opts.ServiceUUIDs {
			for _, advUUID := range adv.Services() {
				if required.Equal(advUUID) {
					hasRequired = true
					break
				}
			}
			if hasRequired {
				break
			}
		}
		if !hasRequired {
			return false
		}
	}

	return true
}

// GetDevices returns a snapshot of discovered devices
func (s *Scanner) GetDevices() []device.Device {
	s.deviceMutex.RLock()
	defer s.deviceMutex.RUnlock()

	devs := make([]device.Device, 0, len(s.devices))
	for _, d := range s.devices {
		devs = append(devs, d)
	}
	return devs
}

// GetDevice returns a device by ID
func (s *Scanner) GetDevice(deviceID string) (device.Device, bool) {
	s.deviceMutex.RLock()
	defer s.deviceMutex.RUnlock()

	dev, ok := s.devices[deviceID]
	return dev, ok
}

// ClearDevices removes all devices
func (s *Scanner) ClearDevices() {
	s.deviceMutex.Lock()
	defer s.deviceMutex.Unlock()

	s.devices = make(map[string]device.Device)
	s.logger.Info("Cleared all discovered devices")
}

// IsScanning reports whether a scan is active
func (s *Scanner) IsScanning() bool {
	s.scanMutex.RLock()
	defer s.scanMutex.RUnlock()
	return s.isScanning
}

// Stop cancels active scan
func (s *Scanner) Stop() error {
	s.scanMutex.RLock()
	scanning := s.isScanning
	s.scanMutex.RUnlock()

	if !scanning {
		return fmt.Errorf("scanner is not running")
	}

	if err := ble.Stop(); err != nil {
		return fmt.Errorf("failed to stop scan: %w", err)
	}

	s.logger.Info("BLE scan stopped")
	return nil
}
