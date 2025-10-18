package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/srg/blim"
	"github.com/srg/blim/bridge"
	"github.com/srg/blim/internal/device"
	"github.com/srg/blim/internal/lua"
)

// bridgeCmd represents the bridge command
var bridgeCmd = &cobra.Command{
	Use:   "bridge <device-address>",
	Short: "Create a PTY bridge to a BLE device",
	Long: fmt.Sprintf(`Creates a bidirectional PTY (pseudoterminal) bridge to a BLE device,
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
  blim bridge %s
  blim bridge --service=custom-uuid %s

%s`, exampleDeviceAddress, exampleDeviceAddress, deviceAddressNote),
	Args: cobra.ExactArgs(1),
	RunE: runBridge,
}

var (
	bridgeServiceUUID           string
	bridgeConnectTimeout        time.Duration
	bridgeDescriptorReadTimeout time.Duration
	bridgeLuaScript             string
	bridgeSymlink               string
)

func init() {
	bridgeCmd.Flags().StringVar(&bridgeServiceUUID, "service", "6E400001-B5A3-F393-E0A9-E50E24DCCA9E", "BLE service UUID to bridge with")
	bridgeCmd.Flags().DurationVar(&bridgeConnectTimeout, "connect-timeout", 30*time.Second, "Connection timeout")
	bridgeCmd.Flags().DurationVar(&bridgeDescriptorReadTimeout, "descriptor-timeout", 0, "Timeout for reading descriptor values (default: 2s if unset, 0 to skip descriptor reads)")
	bridgeCmd.Flags().StringVar(&bridgeLuaScript, "script", "", "Lua script file with ble_to_tty() and tty_to_ble() functions")
	bridgeCmd.Flags().StringVar(&bridgeSymlink, "symlink", "", "Create a symlink to the PTY device (e.g., /tmp/ble-device)")
}

func runBridge(cmd *cobra.Command, args []string) error {
	// Configure logger based on --log-level and --verbose flags
	logger, err := configureLogger(cmd, "verbose")
	if err != nil {
		return err
	}

	// All arguments validated - don't show usage on runtime errors
	cmd.SilenceUsage = true

	deviceAddress := args[0]

	// Validate and normalize service UUID
	serviceUUIDs, err := device.ValidateUUID(bridgeServiceUUID)
	if err != nil {
		return fmt.Errorf("invalid service UUID: %w", err)
	}
	serviceUUID := serviceUUIDs[0]

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
	bridgeCallback := func(b bridge.Bridge) (any, error) {

		// HACK: Create an output drainer to capture output from the Lua API,
		// 		even though the script execution completed, the bridge is keeping the Lua State open, using it by
		// 		calling the Lua callbacks until the bridge is canceled.
		drainer := lua.NewOutputDrainer(ctx, b.GetLuaAPI().OutputChannel(), logger, os.Stdout, os.Stderr)

		defer func() {
			// Stop the drainer after a script completes
			drainer.Cancel()
			drainer.Wait()
		}()

		// Execute the Lua script
		err = lua.ExecuteDeviceScriptWithOutput(
			ctx,
			nil,
			b.GetLuaAPI(),
			logger,
			scriptContent,
			scriptArgs,
			nil,
			nil,
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
			BleAddress:               deviceAddress,
			BleConnectTimeout:        bridgeConnectTimeout,
			BleDescriptorReadTimeout: bridgeDescriptorReadTimeout,
			BleSubscribeOptions: []device.SubscribeOptions{
				{
					Service: serviceUUID,
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
