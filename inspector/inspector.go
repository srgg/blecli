package inspector

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/go-ble/ble"
	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/device"
)

// InspectOptions defines options for inspecting a BLE device profile
type InspectOptions struct {
	ConnectTimeout time.Duration
	ReadLimit      int // 0 disables characteristic reads
}

// InspectResult is a structured representation of a device's GATT discovery results
// Includes inspect-only previews and a snapshot of the device enriched with GATT services
// (no characteristic values stored in the device model).
type InspectResult struct {
	Address  string        `json:"address,omitempty"`
	Name     string        `json:"name,omitempty"`
	Device   device.Device `json:"device,omitempty"`
	Services []ServiceInfo `json:"services"`
}

type ServiceInfo struct {
	UUID            string               `json:"uuid"`
	Characteristics []CharacteristicInfo `json:"characteristics"`
}

type CharacteristicInfo struct {
	UUID        string           `json:"uuid"`
	Properties  string           `json:"properties"`
	ValueHex    string           `json:"value_hex,omitempty"`
	ValueASCII  string           `json:"value_ascii,omitempty"`
	Descriptors []DescriptorInfo `json:"descriptors,omitempty"`
}

type DescriptorInfo struct {
	UUID string `json:"uuid"`
}

// InspectDevice connects to a device, discovers its profile and optionally reads characteristic previews
func InspectDevice(ctx context.Context, address string, opts *InspectOptions, logger *logrus.Logger) (*InspectResult, error) {
	if opts == nil {
		opts = &InspectOptions{ConnectTimeout: 30 * time.Second, ReadLimit: 64}
	}
	if logger == nil {
		logger = logrus.New()
	}

	// Initialize a BLE device
	d, err := device.DeviceFactory()
	if err != nil {
		return nil, fmt.Errorf("failed to create BLE device: %w", err)
	}
	ble.SetDefaultDevice(d)

	// Apply timeout to context if provided
	cctx := ctx
	if opts.ConnectTimeout > 0 {
		var cancel context.CancelFunc
		cctx, cancel = context.WithTimeout(ctx, opts.ConnectTimeout)
		defer cancel()
	}

	logger.WithField("address", address).Info("Dialing BLE device...")

	// Progress ticker for connecting phase - show countdown
	connectStart := time.Now()
	stopProgress := make(chan bool)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopProgress:
				return
			case <-ticker.C:
				elapsed := time.Since(connectStart)
				remaining := opts.ConnectTimeout - elapsed
				if remaining > 0 {
					seconds := int(remaining.Seconds())
					if remaining.Truncate(time.Second) < remaining {
						seconds++
					}
					if seconds > 0 {
						fmt.Printf("\rInspecting device %s (Connecting %ds)   ", address, seconds)
					}
				}
			}
		}
	}()

	fmt.Printf("Inspecting device %s (Connecting %ds)   ", address, int(opts.ConnectTimeout.Seconds()))
	client, err := ble.Dial(cctx, ble.NewAddr(address))
	stopProgress <- true

	if err != nil {
		fmt.Print("\r\033[K") // Clear the line
		return nil, fmt.Errorf("failed to connect to device %s: %w", address, err)
	}
	defer func() {
		_ = client.CancelConnection()
	}()

	logger.Info("Discovering profile (services/characteristics)...")

	// Progress ticker for discovery phase
	discoverStart := time.Now()
	stopProgress2 := make(chan bool)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopProgress2:
				return
			case <-ticker.C:
				elapsed := time.Since(discoverStart)
				seconds := int(elapsed.Seconds()) + 1
				fmt.Printf("\rInspecting device %s (Discovering %ds)   ", address, seconds)
			}
		}
	}()

	fmt.Printf("\rInspecting device %s (Discovering 0s)   ", address)
	profile, err := client.DiscoverProfile(true)
	stopProgress2 <- true

	if err != nil {
		fmt.Print("\r\033[K") // Clear the progress line
		return nil, fmt.Errorf("failed to discover profile: %w", err)
	}

	// Progress ticker for exploring phase (reading characteristics)
	if opts.ReadLimit > 0 {
		exploreStart := time.Now()
		stopProgress3 := make(chan bool)
		go func() {
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stopProgress3:
					return
				case <-ticker.C:
					elapsed := time.Since(exploreStart)
					seconds := int(elapsed.Seconds()) + 1
					fmt.Printf("\rInspecting device %s (Exploring %ds)   ", address, seconds)
				}
			}
		}()
		fmt.Printf("\rInspecting device %s (Exploring 0s)   ", address)
		defer func() {
			stopProgress3 <- true
			fmt.Print("\r\033[K") // Clear the progress line
		}()
	} else {
		fmt.Print("\r\033[K") // Clear the progress line
	}

	res := &InspectResult{Address: address}
	var deviceName string

	for _, svc := range profile.Services {
		si := ServiceInfo{UUID: svc.UUID.String()}
		// We'll skip building device services for now since we only need ServiceInfo

		for _, ch := range svc.Characteristics {
			propStr := fmt.Sprintf("0x%02X", ch.Property)
			ci := CharacteristicInfo{UUID: ch.UUID.String(), Properties: propStr}

			// Optional reads for preview (inspect-only)
			if opts.ReadLimit > 0 {
				if data, err := client.ReadCharacteristic(ch); err == nil && len(data) > 0 {
					trim := data
					if len(trim) > opts.ReadLimit {
						trim = trim[:opts.ReadLimit]
					}
					ci.ValueHex = strings.ToUpper(hex.EncodeToString(trim))
					ci.ValueASCII = asciiPreview(trim)
					// Capture Device Name (GAP, 0x2A00) if present
					if ch.UUID.Equal(ble.MustParse("2A00")) {
						deviceName = ci.ValueASCII
					}
				}
			}

			// Descriptors for inspect view
			for _, d := range ch.Descriptors {
				ci.Descriptors = append(ci.Descriptors, DescriptorInfo{UUID: d.UUID.String()})
			}

			si.Characteristics = append(si.Characteristics, ci)
		}

		res.Services = append(res.Services, si)
	}

	// Build a device.Device snapshot so the CLI can show all device fields up front
	dev := device.NewDeviceWithAddress(address, logger)
	res.Name = deviceName
	res.Device = dev

	return res, nil
}

// asciiPreview returns a safe ASCII preview, replacing non-printable bytes with '.'
func asciiPreview(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		if c >= 32 && c <= 126 {
			sb.WriteByte(c)
		} else {
			sb.WriteByte('.')
		}
	}
	return sb.String()
}
