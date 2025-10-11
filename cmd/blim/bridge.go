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
	"github.com/srg/blim"
	"github.com/srg/blim/bridge"
	"github.com/srg/blim/internal/device"
	"github.com/srg/blim/internal/lua"
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
	bridgeSymlink        string
)

func init() {
	bridgeCmd.Flags().StringVar(&bridgeServiceUUID, "service", "6E400001-B5A3-F393-E0A9-E50E24DCCA9E", "BLE service UUID to bridge with")
	bridgeCmd.Flags().DurationVar(&bridgeConnectTimeout, "connect-timeout", 30*time.Second, "Connection timeout")
	bridgeCmd.Flags().BoolVarP(&bridgeVerbose, "verbose", "v", false, "Verbose output")
	bridgeCmd.Flags().StringVar(&bridgeLuaScript, "script", "", "Lua script file with ble_to_tty() and tty_to_ble() functions")
	bridgeCmd.Flags().StringVar(&bridgeSymlink, "symlink", "", "Create a symlink to the PTY device (e.g., /tmp/ble-device)")
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

	// All arguments validated - don't show usage on runtime errors
	cmd.SilenceUsage = true

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
		cancel()
	}()

	// Load script content before creating the callback
	var scriptContent string
	if bridgeLuaScript != "" {
		// Read the custom script file
		logger.WithField("file", bridgeLuaScript).Info("Loading custom Lua script")
		content, err := os.ReadFile(bridgeLuaScript)
		if err != nil {
			return fmt.Errorf("failed to read script file: %w", err)
		}
		scriptContent = string(content)
	} else {
		// Use default bridge script
		logger.Info("Using default bridge script")
		scriptContent = blecli.DefaultBridgeLuaScript
	}

	var scriptArgs map[string]string

	// Setup progress printer
	progress := NewProgressPrinter(fmt.Sprintf("Starting bridge for %s", deviceAddress), "Connecting", "Running")
	progress.Start()
	defer progress.Stop()

	// Bridge callback - executes the Lua script with output streaming
	bridgeCallback := func(b bridge.Bridge) (error, error) {
		// Execute the Lua script
		err = lua.ExecuteDeviceScriptWithOutput(
			ctx,
			nil,
			b.GetLuaAPI(),
			logger,
			scriptContent,
			scriptArgs,
			os.Stdout,
			os.Stderr,
			50*time.Millisecond,
		)
		if err != nil {
			return nil, err
		}

		// Script executed successfully, and subscriptions are active
		// Wait for context cancellation (Ctrl+C) to keep the bridge running
		<-ctx.Done()
		logger.Info("Bridge shutting down...")

		return nil, nil
	}

	// Run the bridge with callback
	_, err = bridge.RunDeviceBridge(
		ctx,
		&bridge.BridgeOptions{
			BleAddress:        deviceAddress,
			BleConnectTimeout: bridgeConnectTimeout,
			BleSubscribeOptions: []device.SubscribeOptions{
				{
					Service: serviceUUID.String(),
				},
			},
			Logger:         logger,
			TTYSymlinkPath: bridgeSymlink,
		},
		progress.Callback(),
		bridgeCallback,
	)

	return err
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
