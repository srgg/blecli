package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	blelib "github.com/go-ble/ble"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/srg/blim/bridge"
	"github.com/srg/blim/internal/device"
	"github.com/srg/blim/pkg/config"
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
  blim bridge AA:BB:CC:DD:EE:FF
  blim bridge --service=custom-uuid AA:BB:CC:DD:EE:FF`,
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

	deviceAddress := args[0]
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

	// Create a Lua-based PTY bridge
	bridge, err := bridge.NewBridge(logger, bridge.Bridge2Config{
		Address:    deviceAddress,
		ScriptFile: bridgeLuaScript,
		Script:     "",
	})
	if err != nil {
		return fmt.Errorf("failed to create bridge: %w", err)
	}

	if err = bridge.Start(ctx, &device.ConnectOptions{
		Services: []device.SubscribeOptions{
			{
				Service: serviceUUID.String(),
			},
		},
	}); err != nil {
		return fmt.Errorf("failed to start bridge: %w", err)
	}

	defer func() {
		logger.Debug("=== DEFER STOP BEGIN ===")
		start := time.Now()

		if err := bridge.Stop(); err != nil {
			logger.WithError(err).Error("Defer stop failed")
		} else {
			logger.WithField("duration", time.Since(start)).Debug("Defer stop completed")
		}
		logger.Debug("=== DEFER STOP END ===")
	}()

	// TODO: generate BLE Bridge info message by Lua script
	services := bridge.GetServices()
	fmt.Printf("\n=== BLE-PTY Bridge is Active ===\n")
	fmt.Printf("Device: %s\n", deviceAddress)
	fmt.Printf("PTY: %s\n", bridge.GetPTYName())
	fmt.Printf("Service: %s\n", serviceUUID.String())

	// Find the specific service and display its characteristics
	if svc, ok := services[serviceUUID.String()]; ok {
		characteristics := svc.GetCharacteristics()
		fmt.Printf("Characteristics: %d\n", len(characteristics))
		for _, char := range characteristics {
			fmt.Printf("  - %s\n", char.GetUUID())
		}
	} else {
		fmt.Printf("Characteristics: 0 (service not found)\n")
	}
	fmt.Printf("\nBridge is running. Connect your application to %s\n", bridge.GetPTYName())
	fmt.Printf("Press Ctrl+C to stop the bridge.\n\n")

	// Run until context is canceled
	logger.Debug("Waiting for context cancellation...")
	<-ctx.Done()

	logger.Info("Bridge shutting down...")
	logger.Debug("Function returning, defer will trigger...")
	return nil
}

func parseUUID(uuidStr, name string) (*blelib.UUID, error) {
	if uuidStr == "" {
		return nil, fmt.Errorf("%s UUID cannot be empty", name)
	}

	uuid, err := blelib.Parse(uuidStr)
	if err != nil {
		return nil, fmt.Errorf("invalid %s UUID '%s': %w", name, uuidStr, err)
	}

	return &uuid, nil
}
