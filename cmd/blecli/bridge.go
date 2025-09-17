package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-ble/ble"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	ble2 "github.com/srg/blecli/pkg/ble"

	"github.com/srg/blecli/pkg/config"
	"github.com/srg/blecli/pkg/connection"
)

// bridgeCmd represents the bridge command
var bridgeCmd = &cobra.Command{
	Use:   "bridge <device-address>",
	Short: "Create a PTY bridge to a BLE device",
	Long: `Creates a bidirectional PTY (pseudoterminal) bridge to a BLE device,
allowing applications that expect a serial port to communicate with BLE devices.

The bridge creates a virtual serial device (e.g., /dev/ttys001) that applications
can connect to. Data written to the PTY is sent to the BLE device via the Nordic
UART Service, and data received from the device is written to the PTY.

This is useful for:
- Connecting terminal emulators to BLE devices
- Using existing serial applications with BLE devices
- Testing and debugging BLE serial communication
- Integrating BLE devices with legacy serial software

Example:
  blecli bridge AA:BB:CC:DD:EE:FF
  blecli bridge --service=custom-uuid AA:BB:CC:DD:EE:FF`,
	Args: cobra.ExactArgs(1),
	RunE: runBridge,
}

var (
	bridgeServiceUUID    string
	bridgeTxCharUUID     string
	bridgeRxCharUUID     string
	bridgeConnectTimeout time.Duration
	bridgeVerbose        bool
)

func init() {
	bridgeCmd.Flags().StringVar(&bridgeServiceUUID, "service", "6E400001-B5A3-F393-E0A9-E50E24DCCA9E", "BLE service UUID for serial communication")
	bridgeCmd.Flags().StringVar(&bridgeTxCharUUID, "tx-char", "6E400003-B5A3-F393-E0A9-E50E24DCCA9E", "TX characteristic UUID (device -> client)")
	bridgeCmd.Flags().StringVar(&bridgeRxCharUUID, "rx-char", "6E400002-B5A3-F393-E0A9-E50E24DCCA9E", "RX characteristic UUID (client -> device)")
	bridgeCmd.Flags().DurationVar(&bridgeConnectTimeout, "connect-timeout", 30*time.Second, "Connection timeout")
	bridgeCmd.Flags().BoolVarP(&bridgeVerbose, "verbose", "v", false, "Verbose output")
}

func runBridge(cmd *cobra.Command, args []string) error {
	deviceAddress := args[0]

	// Create configuration
	cfg := config.DefaultConfig()
	if bridgeVerbose {
		cfg.LogLevel = logrus.DebugLevel
	}

	logger := cfg.NewLogger()

	// Parse UUIDs
	serviceUUID, err := parseUUID(bridgeServiceUUID, "service")
	if err != nil {
		return err
	}

	txCharUUID, err := parseUUID(bridgeTxCharUUID, "tx-char")
	if err != nil {
		return err
	}

	rxCharUUID, err := parseUUID(bridgeRxCharUUID, "rx-char")
	if err != nil {
		return err
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupts gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		logger.Info("Received interrupt signal, shutting down...")
		cancel()
	}()

	// Create BLE connection
	connOpts := &connection.ConnectOptions{
		DeviceAddress:  deviceAddress,
		ConnectTimeout: bridgeConnectTimeout,
		ServiceUUID:    serviceUUID,
		TxCharUUID:     txCharUUID,
		RxCharUUID:     rxCharUUID,
	}

	conn := connection.NewConnection(connOpts, logger)

	// Create PTY bridge
	bridge := ble2.NewBridge(logger)
	bridgeOpts := ble2.DefaultBridgeOptions()

	// Set up data handlers
	conn.SetDataHandler(func(data []byte) {
		// Data from BLE device -> PTY
		if err := bridge.WriteFromDevice(data); err != nil {
			logger.WithError(err).Error("Failed to write data to PTY")
		}
	})

	// Connect to BLE device
	logger.WithField("address", deviceAddress).Info("Connecting to BLE device...")
	if err := conn.Connect(ctx, connOpts); err != nil {
		return fmt.Errorf("failed to connect to BLE device: %w", err)
	}
	defer conn.Disconnect()

	// Start PTY bridge
	logger.Info("Starting PTY bridge...")
	if err := bridge.Start(ctx, bridgeOpts, conn.Write); err != nil {
		return fmt.Errorf("failed to start PTY bridge: %w", err)
	}
	defer bridge.Stop()

	// Display connection info
	fmt.Printf("\n=== BLE-PTY Bridge Active ===\n")
	fmt.Printf("Device: %s\n", deviceAddress)
	fmt.Printf("PTY: %s\n", bridge.GetPTYName())
	fmt.Printf("Service: %s\n", serviceUUID.String())
	fmt.Printf("TX Char: %s\n", txCharUUID.String())
	fmt.Printf("RX Char: %s\n", rxCharUUID.String())
	fmt.Printf("\nBridge is running. Connect your application to %s\n", bridge.GetPTYName())
	fmt.Printf("Press Ctrl+C to stop the bridge.\n\n")

	// Run until context is cancelled
	<-ctx.Done()

	logger.Info("Bridge shutting down...")
	return nil
}

func parseUUID(uuidStr, name string) (*ble.UUID, error) {
	if uuidStr == "" {
		return nil, fmt.Errorf("%s UUID cannot be empty", name)
	}

	uuid, err := ble.Parse(uuidStr)
	if err != nil {
		return nil, fmt.Errorf("invalid %s UUID '%s': %w", name, uuidStr, err)
	}

	return &uuid, nil
}
