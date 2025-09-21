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
	"github.com/srg/blecli/pkg/device"

	"github.com/srg/blecli/pkg/config"
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
	bridgeConnectTimeout time.Duration
	bridgeVerbose        bool
	bridgeLuaScript      string
)

func init() {
	bridgeCmd.Flags().StringVar(&bridgeServiceUUID, "service", "6E400001-B5A3-F393-E0A9-E50E24DCCA9E", "BLE service UUID to bridge with")
	bridgeCmd.Flags().DurationVar(&bridgeConnectTimeout, "connect-timeout", 30*time.Second, "Connection timeout")
	bridgeCmd.Flags().BoolVarP(&bridgeVerbose, "verbose", "v", false, "Verbose output")
	bridgeCmd.Flags().StringVar(&bridgeLuaScript, "script", "", "Lua script file with ble_to_tty() and tty_to_ble() functions")
}

func runBridge(cmd *cobra.Command, args []string) error {
	deviceAddress := args[0]

	// Create configuration
	cfg := config.DefaultConfig()

	// Check global log level flag
	logLevel, _ := cmd.Flags().GetString("log-level")
	if logLevel != "" {
		switch logLevel {
		case "debug":
			cfg.LogLevel = logrus.DebugLevel
		case "info":
			cfg.LogLevel = logrus.InfoLevel
		case "warn":
			cfg.LogLevel = logrus.WarnLevel
		case "error":
			cfg.LogLevel = logrus.ErrorLevel
		default:
			return fmt.Errorf("invalid log level: %s", logLevel)
		}
	} else if bridgeVerbose {
		cfg.LogLevel = logrus.DebugLevel
	}

	logger := cfg.NewLogger()

	// Parse service UUID
	serviceUUID, err := parseUUID(bridgeServiceUUID, "service")
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
		logger.Debug("About to cancel context")
		cancel()
		logger.Debug("Context cancelled")
	}()

	// Create BLE connection
	connOpts := device.ConnectOptions{
		ConnectTimeout: bridgeConnectTimeout,
		ServiceUUID:    serviceUUID,
	}

	device := device.NewDeviceWithAddress(deviceAddress, logger)
	logger.WithField("address", deviceAddress).Info("Connecting to BLE device...")
	if err := device.Connect(ctx, &connOpts); err != nil {
		return fmt.Errorf("failed to connect to BLE device: %w", err)
	}

	defer func() {
		logger.Debug("=== DEFER DISCONNECT START ===")
		start := time.Now()
		if err := device.Disconnect(); err != nil {
			logger.WithError(err).Error("Defer disconnect failed")
		} else {
			logger.WithField("duration", time.Since(start)).Debug("Defer disconnect completed")
		}
		logger.Debug("=== DEFER DISCONNECT END ===")
	}()

	// Create a Lua-based PTY bridge
	bridge := ble2.NewBridge(logger)
	bridgeOpts := ble2.DefaultBridgeOptions()

	// Load Lua script if provided
	if bridgeLuaScript != "" {
		logger.WithField("script", bridgeLuaScript).Info("Loading Lua transformation script...")
		if err := bridge.GetEngine().LoadScriptFile(bridgeLuaScript); err != nil {
			return fmt.Errorf("failed to load Lua script: %w", err)
		}
	}

	//// Connect to BLE device first to discover characteristics
	//logger.WithField("address", deviceAddress).Info("Connecting to BLE device...")
	//if err := conn.Connect(ctx, connOpts); err != nil {
	//	return fmt.Errorf("failed to connect to BLE device: %w", err)
	//}
	//defer func() {
	//	logger.Debug("=== DEFER DISCONNECT START ===")
	//	start := time.Now()
	//	if err := conn.Disconnect(); err != nil {
	//		logger.WithError(err).Error("Defer disconnect failed")
	//	} else {
	//		logger.WithField("duration", time.Since(start)).Debug("Defer disconnect completed")
	//	}
	//	logger.Debug("=== DEFER DISCONNECT END ===")
	//}()

	// Add BLE characteristics to the bridge
	logger.Info("Adding BLE characteristics to Lua bridge...")
	characteristics := device.GetCharacteristics()
	for _, char := range characteristics {
		// Only add characteristics from the specified service if serviceUUID is set
		// For now, add all characteristics - could filter by service UUID later
		logger.WithField("uuid", char.GetUUID()).Debug("Added characteristic to bridge")
		// Note: bridge.AddBLECharacteristic expects a different type - this may need adjustment
	}

	// Set up BLE write callback
	bridge.SetBLEWriteCallback(func(uuid string, data []byte) error {
		return device.WriteToCharacteristic(uuid, data)
	})

	// Set up data handlers for incoming BLE data
	device.SetDataHandler(func(uuid string, data []byte) {
		// Raw BLE data -> Lua engine
		bridge.UpdateCharacteristic(uuid, data)
	})

	// Start Lua bridge
	logger.Info("Starting Lua-based PTY bridge...")
	logger.Debug("About to call bridge.Start()")
	if err := bridge.Start(ctx, bridgeOpts); err != nil {
		return fmt.Errorf("failed to start Lua bridge: %w", err)
	}
	logger.Debug("bridge.Start() completed")
	defer func() {
		logger.Debug("=== DEFER BRIDGE STOP START ===")
		start := time.Now()
		if err := bridge.Stop(); err != nil {
			logger.WithError(err).Error("Defer bridge stop failed")
		} else {
			logger.WithField("duration", time.Since(start)).Debug("Defer bridge stop completed")
		}
		logger.Debug("=== DEFER BRIDGE STOP END ===")
	}()

	// Display connection info
	characteristics = device.GetCharacteristics()
	fmt.Printf("\n=== BLE-PTY Bridge Active ===\n")
	fmt.Printf("Device: %s\n", deviceAddress)
	fmt.Printf("PTY: %s\n", bridge.GetPTYName())
	fmt.Printf("Service: %s\n", serviceUUID.String())
	fmt.Printf("Characteristics: %d\n", len(characteristics))
	for _, char := range characteristics {
		fmt.Printf("  - %s\n", char.GetUUID())
	}
	fmt.Printf("\nBridge is running. Connect your application to %s\n", bridge.GetPTYName())
	fmt.Printf("Press Ctrl+C to stop the bridge.\n\n")

	// Run until context is cancelled
	logger.Debug("Waiting for context cancellation...")
	<-ctx.Done()

	logger.Info("Bridge shutting down...")
	logger.Debug("Function returning, defer will trigger...")
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
