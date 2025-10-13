package goble

import (
	"fmt"
	"time"

	"github.com/go-ble/ble"
	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/bledb"
	"github.com/srg/blim/internal/device"
)

const (
	// DefaultDescriptorReadTimeout is the default timeout for descriptor read operations.
	// Used when ConnectOptions.DescriptorReadTimeout is not explicitly set (via CLI flag).
	DefaultDescriptorReadTimeout = 2 * time.Second
)

// BLEDescriptor implements the Descriptor interface for BLE GATT descriptors.
// It stores both raw descriptor values and parsed representations for well-known descriptor types.
type BLEDescriptor struct {
	uuid        string
	knownName   string
	value       []byte
	parsedValue interface{}
	BLEDesc     *ble.Descriptor // Reference to underlying BLE descriptor for potential re-reads
}

// newDescriptor creates a BLEDescriptor and attempts to read its value with a timeout.
// If timeout is 0, descriptor reads are skipped entirely (fast path - no blocking).
// If timeout > 0, attempts to read with that timeout (best-effort, won't fail on error/timeout).
// For well-known descriptor UUIDs (0x2900-0x2906), values are automatically parsed.
func newDescriptor(d *ble.Descriptor, client ble.Client, timeout time.Duration, logger *logrus.Logger) *BLEDescriptor {
	descRawUUID := d.UUID.String()
	descUUID := device.NormalizeUUID(descRawUUID)

	bleDesc := &BLEDescriptor{
		uuid:      descUUID,
		knownName: bledb.LookupDescriptor(descRawUUID),
		BLEDesc:   d,
	}

	// Fast path: skip descriptor reads if timeout is 0 or no client available
	if timeout == 0 || client == nil {
		return bleDesc
	}

	// Slow path: read descriptor value with timeout
	type readResult struct {
		data []byte
		err  error
	}
	resultCh := make(chan readResult, 1)

	go func() {
		// First check if descriptor already has a value from discovery
		if len(d.Value) > 0 {
			resultCh <- readResult{data: d.Value, err: nil}
			return
		}

		// Check if Handle is valid (0 means not set/invalid)
		// On Darwin/macOS, the go-ble/ble library doesn't populate descriptor handles,
		// so descriptors cannot be read explicitly
		if d.Handle == 0 {
			resultCh <- readResult{data: []byte{}, err: fmt.Errorf("descriptor handle not available (macOS limitation)")}
			return
		}

		// If no cached value and handle is valid, perform explicit read
		data, err := client.ReadDescriptor(d)
		resultCh <- readResult{data: data, err: err}
	}()

	select {
	case result := <-resultCh:
		if result.err == nil {
			bleDesc.value = result.data

			// Parse well-known descriptors automatically
			if parsed, err := device.ParseDescriptorValue(descUUID, result.data); err == nil {
				bleDesc.parsedValue = parsed
			} else {
				// Parse error - set parsedValue to DescriptorError
				bleDesc.parsedValue = &device.DescriptorError{
					Reason: "parse_error",
					Err:    err,
				}
				if logger != nil {
					logger.WithFields(logrus.Fields{
						"descriptor_uuid": descUUID,
						"error":           err,
					}).Debug("Failed to parse descriptor value")
				}
			}
		} else {
			// Read error - set parsedValue to DescriptorError
			bleDesc.parsedValue = &device.DescriptorError{
				Reason: "read_error",
				Err:    result.err,
			}
			if logger != nil {
				logger.WithFields(logrus.Fields{
					"descriptor_uuid": descUUID,
					"error":           result.err,
				}).Debug("Failed to read descriptor value")
			}
		}
	case <-time.After(timeout):
		// Timeout - set parsedValue to DescriptorError
		bleDesc.parsedValue = &device.DescriptorError{
			Reason: "timeout",
			Err:    nil,
		}
		if logger != nil {
			logger.WithFields(logrus.Fields{
				"descriptor_uuid": descUUID,
				"timeout":         timeout,
			}).Debug("Timeout reading descriptor value")
		}
	}

	return bleDesc
}

// UUID returns the normalized descriptor UUID (lowercase, without dashes for 16-bit UUIDs).
func (d *BLEDescriptor) UUID() string {
	return d.uuid
}

// KnownName returns the human-readable name for well-known descriptor UUIDs.
// Returns empty string for unknown descriptors.
func (d *BLEDescriptor) KnownName() string {
	return d.knownName
}

// Value returns the raw descriptor value bytes.
// Returns nil if the value was not successfully read or was skipped.
func (d *BLEDescriptor) Value() []byte {
	return d.value
}

// ParsedValue returns the parsed descriptor value for well-known descriptor types.
// Returns *DescriptorError if read/parse failed, nil if descriptor read was skipped.
//
// Type assertions can be used to access specific descriptor types:
//   - *ExtendedProperties for 0x2900
//   - string for 0x2901 (User Description)
//   - *ClientConfig for 0x2902
//   - *ServerConfig for 0x2903
//   - *PresentationFormat for 0x2904
//   - *ValidRange for 0x2906
//   - []byte for unknown descriptor types
//   - *DescriptorError if read/parse failed
func (d *BLEDescriptor) ParsedValue() interface{} {
	return d.parsedValue
}
