package scanner

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cornelk/hashmap"
	blelib "github.com/go-ble/ble"
	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/device"
	"github.com/srg/blim/internal/lua"
)

// ProgressCallback is called when the scan phase changes
type ProgressCallback func(phase string)

// DeviceEventType marks if the device was newly discovered or updated
type DeviceEventType int

const (
	EventNew DeviceEventType = iota
	EventUpdated
)

type DeviceEvent struct {
	Type       DeviceEventType
	DeviceInfo device.DeviceInfo
}

// Scanner handles BLE device discovery
type Scanner struct {
	devices *hashmap.Map[string, device.Device]
	events  *lua.RingChannel[DeviceEvent]
	logger  *logrus.Logger
	//isScanning bool

	scanOptions *ScanOptions
	scanDevice  blelib.Device
}

// ScanOptions configures scanning behavior
type ScanOptions struct {
	Duration        time.Duration
	DuplicateFilter bool
	ServiceUUIDs    []blelib.UUID
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

	return &Scanner{
		events: lua.NewRingChannel[DeviceEvent](100),
		logger: logger,
	}, nil
}

// Scan performs BLE discovery with provided options
func (s *Scanner) Scan(ctx context.Context, opts *ScanOptions, progressCallback ProgressCallback) (map[string]device.DeviceInfo, error) {
	s.devices = hashmap.New[string, device.Device]()

	if opts == nil {
		opts = DefaultScanOptions()
	}
	if progressCallback == nil {
		progressCallback = func(string) {} // No-op callback
	}

	s.logger.WithField("duration", opts.Duration).Info("Starting BLE scan...")

	// Report scanning phase
	progressCallback("Scanning")

	dev, err := device.DeviceFactory()
	if err != nil {
		return nil, fmt.Errorf("failed to create BLE device: %w", err)
	}
	s.scanDevice = dev

	s.scanOptions = opts
	defer func() {
		s.scanOptions = nil
	}()
	err = s.scanDevice.Scan(ctx, opts.DuplicateFilter, s.handleAdvertisement)
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return nil, fmt.Errorf("scan failed: %w", err)
	}

	s.logger.WithField("device_count", s.devices.Len()).Info("BLE scan completed")

	// Report processing phase
	progressCallback("Processing results")

	devices := make(map[string]device.DeviceInfo, s.devices.Len())
	s.devices.Range(func(key string, value device.Device) bool {
		devices[key] = value
		return true
	})

	return devices, nil
}

// handleAdvertisement updates existing or adds a new device
func (s *Scanner) handleAdvertisement(adv blelib.Advertisement) {
	deviceID := adv.Addr().String()

	dev, existing := s.devices.Get(deviceID)
	if !existing {
		if !s.shouldIncludeDevice(adv, s.scanOptions) {
			return
		}
		dev, existing = s.devices.GetOrInsert(deviceID, device.NewDevice(adv, s.logger))
	}

	event := DeviceEvent{
		DeviceInfo: dev,
	}

	if existing {
		dev.Update(adv)
		event.Type = EventUpdated
	} else {
		s.logger.WithFields(logrus.Fields{
			"device":  dev.GetName(),
			"address": dev.GetAddress(),
			"rssi":    dev.GetRSSI(),
		}).Info("Discovered new device")
		event.Type = EventNew
	}

	s.events.ForceSend(event)
}

// shouldIncludeDevice applies to allow/block/service filters
func (s *Scanner) shouldIncludeDevice(adv blelib.Advertisement, opts *ScanOptions) bool {
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
func (s *Scanner) makeDeviceList() []device.DeviceInfo {
	devs := make([]device.DeviceInfo, 0, s.devices.Len())

	s.devices.Range(func(key string, value device.Device) bool {
		devs = append(devs, value)
		return true
	})

	return devs
}

// Events return a read-only channel of device events
func (s *Scanner) Events() <-chan DeviceEvent {
	return s.events.C()
}

//func (s *Scanner) CancelScan() error {
//	if s.scanDevice != nil {
//		return s.scanDevice.Stop()
//	}
//
//	return fmt.Errorf("no scan device to cancel")
//}
