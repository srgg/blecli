package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/srg/blim/inspector"
	"github.com/srg/blim/internal/device"
)

// readCmd represents the read command
var readCmd = &cobra.Command{
	Use:   "read <device-address> <uuid>",
	Short: "Read a characteristic or descriptor value",
	Long: fmt.Sprintf(`Reads data from a BLE characteristic or descriptor.

Examples:
  # Read Battery Level characteristic
  blim read %s 2a19

  # Read with service disambiguation
  blim read %s --service 180f --char 2a19

  # Read descriptor (Client Characteristic Configuration)
  blim read %s --service 180d --char 2a37 --desc 2902

  # Output as hex
  blim read %s 2a19 --hex

  # Continuously watch characteristic (polls every second)
  blim read %s 2a37 --watch

  # Watch with custom interval
  blim read %s 2a37 --watch 500ms

%s`, exampleDeviceAddress, exampleDeviceAddress, exampleDeviceAddress, exampleDeviceAddress, exampleDeviceAddress, exampleDeviceAddress, deviceAddressNote),
	Args: cobra.RangeArgs(1, 2),
	RunE: runRead,
}

var (
	readServiceUUID string
	readCharUUID    string
	readDescUUID    string
	readHex         bool
	readTimeout     time.Duration
	readWatch       string
)

func init() {
	readCmd.Flags().StringVar(&readServiceUUID, "service", "", "Service UUID (required if characteristic UUID is ambiguous)")
	readCmd.Flags().StringVar(&readCharUUID, "char", "", "Characteristic UUID")
	readCmd.Flags().StringVar(&readDescUUID, "desc", "", "Descriptor UUID (reads descriptor instead of characteristic)")
	readCmd.Flags().BoolVar(&readHex, "hex", false, "Output as hex string (e.g., 'FF01'); raw bytes by default")
	readCmd.Flags().DurationVar(&readTimeout, "timeout", 5*time.Second, "Read timeout")
	readCmd.Flags().StringVar(&readWatch, "watch", "", "Continuously read at interval (e.g., 1s, 500ms); default 1s if no value given")
	readCmd.Flags().Lookup("watch").NoOptDefVal = "1s"
}

func runRead(cmd *cobra.Command, args []string) error {
	address := args[0]

	// Parse UUID from positional arg or flags
	var targetUUID string
	if len(args) == 2 {
		targetUUID = args[1]
	} else if readCharUUID != "" {
		targetUUID = readCharUUID
	} else if readDescUUID != "" {
		targetUUID = readDescUUID
	} else {
		return fmt.Errorf("UUID required: provide as second argument or via --char/--desc flag")
	}

	// Parse watch interval if a watch flag is set
	var watchInterval time.Duration
	if readWatch != "" {
		var err error
		watchInterval, err = time.ParseDuration(readWatch)
		if err != nil {
			return fmt.Errorf("invalid watch interval: %w", err)
		}
	}

	// Configure logger
	logger, err := configureLogger(cmd, "verbose")
	if err != nil {
		return err
	}

	// All arguments validated - don't show usage on runtime errors
	cmd.SilenceUsage = true

	// Setup progress printer
	operation := "Reading"
	if readWatch != "" {
		operation = "Watching"
	}
	progress := NewProgressPrinter(fmt.Sprintf("%s %s from %s", operation, targetUUID, address), "Connecting", "Processing")
	progress.Start()
	defer progress.Stop()

	// Build inspect options
	opts := &inspector.InspectOptions{
		ConnectTimeout:        30 * time.Second,
		DescriptorReadTimeout: readTimeout,
	}

	// Use background context
	ctx := context.Background()

	// Define the read operation
	readOperation := func(dev device.Device) (any, error) {
		// Stop progress indicator before printing output
		progress.Stop()

		// Get connection
		conn := dev.GetConnection()
		if conn == nil {
			return nil, fmt.Errorf("device not connected")
		}

		// Resolve target characteristic/descriptor
		char, desc, err := resolveTarget(conn, targetUUID, readServiceUUID, readCharUUID, readDescUUID)
		if err != nil {
			return nil, err
		}

		// Perform read or watch
		if readWatch != "" {
			return nil, watchChar(ctx, dev, char, desc, watchInterval, logger)
		}

		return nil, performRead(char, desc)
	}

	_, err = inspector.InspectDevice(ctx, address, opts, logger, progress.Callback(), readOperation)
	return err
}

// resolveTarget resolves the target characteristic and optional descriptor from UUIDs
func resolveTarget(conn device.Connection, targetUUID, serviceUUID, charUUID, descUUID string) (device.Characteristic, device.Descriptor, error) {
	// Normalize UUIDs
	normalizedTarget := device.NormalizeUUID(targetUUID)
	normalizedService := device.NormalizeUUID(serviceUUID)
	normalizedChar := device.NormalizeUUID(charUUID)
	normalizedDesc := device.NormalizeUUID(descUUID)

	// Case 1: Explicit service + char + optional desc
	if serviceUUID != "" && (charUUID != "" || normalizedTarget != "") {
		charToFind := normalizedChar
		if charToFind == "" {
			charToFind = normalizedTarget
		}

		char, err := conn.GetCharacteristic(normalizedService, charToFind)
		if err != nil {
			return nil, nil, fmt.Errorf("characteristic %s not found in service %s: %w", charToFind, serviceUUID, err)
		}

		// If descriptor requested, find it
		if descUUID != "" || normalizedDesc != "" {
			descToFind := normalizedDesc
			if descToFind == "" {
				descToFind = normalizedTarget
			}
			desc := findDescriptor(char, descToFind)
			if desc == nil {
				return nil, nil, fmt.Errorf("descriptor %s not found in characteristic %s", descToFind, charToFind)
			}
			return char, desc, nil
		}

		return char, nil, nil
	}

	// Case 2: Auto-resolve from target UUID
	return autoResolveTarget(conn, normalizedTarget, descUUID != "")
}

// autoResolveTarget attempts to automatically resolve a UUID to a characteristic or descriptor
func autoResolveTarget(conn device.Connection, targetUUID string, isDescriptor bool) (device.Characteristic, device.Descriptor, error) {
	var foundChars []device.Characteristic
	var foundDescs []device.Descriptor
	var foundCharForDesc device.Characteristic

	// Search all services
	for _, svc := range conn.Services() {
		for _, char := range svc.GetCharacteristics() {
			// Check if this is the target characteristic
			if device.NormalizeUUID(char.UUID()) == targetUUID {
				foundChars = append(foundChars, char)
			}

			// If looking for descriptor, search within this characteristic
			if isDescriptor {
				for _, desc := range char.GetDescriptors() {
					if device.NormalizeUUID(desc.UUID()) == targetUUID {
						foundDescs = append(foundDescs, desc)
						foundCharForDesc = char
					}
				}
			}
		}
	}

	// Handle results
	if isDescriptor {
		if len(foundDescs) == 0 {
			return nil, nil, fmt.Errorf("descriptor %s not found", targetUUID)
		}
		if len(foundDescs) > 1 {
			return nil, nil, fmt.Errorf("descriptor %s found in multiple characteristics, specify --service and --char", targetUUID)
		}
		return foundCharForDesc, foundDescs[0], nil
	}

	if len(foundChars) == 0 {
		return nil, nil, fmt.Errorf("characteristic %s not found", targetUUID)
	}
	if len(foundChars) > 1 {
		return nil, nil, fmt.Errorf("characteristic %s found in multiple services, specify --service", targetUUID)
	}

	return foundChars[0], nil, nil
}

// findDescriptor searches for a descriptor by UUID in a characteristic
func findDescriptor(char device.Characteristic, descUUID string) device.Descriptor {
	for _, desc := range char.GetDescriptors() {
		if device.NormalizeUUID(desc.UUID()) == descUUID {
			return desc
		}
	}
	return nil
}

// performRead executes a read operation on a characteristic or descriptor
func performRead(char device.Characteristic, desc device.Descriptor) error {
	var data []byte
	var err error

	// Read descriptor or characteristic
	if desc != nil {
		data = desc.Value()
		if data == nil {
			// Descriptor value not available
			if descErr, ok := desc.ParsedValue().(*device.DescriptorError); ok {
				return fmt.Errorf("failed to read descriptor: %s", descErr.Reason)
			}
			return fmt.Errorf("descriptor value not available")
		}
	} else {
		// Read characteristic using the abstracted interface
		data, err = char.Read(readTimeout)
		if err != nil {
			return fmt.Errorf("failed to read characteristic: %w", err)
		}
	}

	// Format and output data
	return outputData(data)
}

// watchChar continuously reads a characteristic or descriptor at the specified interval
func watchChar(ctx context.Context, dev device.Device, char device.Characteristic, desc device.Descriptor, interval time.Duration, logger *logrus.Logger) error {
	fmt.Fprintf(os.Stderr, "Watching (reading every %v). Press Ctrl+C to stop...\n", interval)

	// Perform immediate first read
	if err := performSingleRead(char, desc, logger); err != nil {
		return err
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := performSingleRead(char, desc, logger); err != nil {
				// Check if the connection was lost by checking for ErrNotConnected in the error chain
				if errors.Is(err, device.ErrNotConnected) {
					return ErrConnectionLost
				}

				// Log other errors but continue watching
				logger.WithError(err).Warn("Failed to read characteristic, continuing...")
			} else {
				logger.Debug("Read operation successful")
			}
		}
	}
}

// performSingleRead executes a single read operation and outputs the data
func performSingleRead(char device.Characteristic, desc device.Descriptor, logger *logrus.Logger) error {
	var data []byte
	var err error

	// Read descriptor or characteristic
	if desc != nil {
		data = desc.Value()
		if data == nil {
			if descErr, ok := desc.ParsedValue().(*device.DescriptorError); ok {
				logger.WithError(fmt.Errorf("%s", descErr.Reason)).Error("failed to read descriptor")
			} else {
				logger.Error("descriptor value not available")
			}
			return fmt.Errorf("descriptor value not available")
		}
	} else {
		data, err = char.Read(readTimeout)
		if err != nil {
			logger.WithError(err).Error("failed to read characteristic")
			return err
		}
	}

	// Output data
	if err := outputData(data); err != nil {
		logger.WithError(err).Error("failed to output data")
		return err
	}

	return nil
}

// outputData formats and outputs data according to flags
func outputData(data []byte) error {
	if readHex {
		// Hex output
		fmt.Println(hex.EncodeToString(data))
		return nil
	}

	// Default: Raw binary output to stdout
	_, err := os.Stdout.Write(data)
	return err
}
