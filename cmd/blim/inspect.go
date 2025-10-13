package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/srg/blim"
	"github.com/srg/blim/inspector"
	"github.com/srg/blim/internal/device"
	"github.com/srg/blim/internal/lua"
	"github.com/srg/blim/pkg/config"
)

// inspectCmd represents the inspect command
var inspectCmd = &cobra.Command{
	Use:   "inspect <device-address>",
	Short: "Inspect services, characteristics, and descriptors of a BLE device",
	Long: `Connects to a BLE device by address and discovers its services,
characteristics, and descriptors. Attempts to read characteristic values when possible.`,
	Args: cobra.ExactArgs(1),
	RunE: runInspect,
}

var (
	inspectConnectTimeout        time.Duration
	inspectDescriptorReadTimeout time.Duration
	inspectVerbose               bool
	inspectJSON                  bool
	inspectReadLimit             int
)

func init() {
	inspectCmd.Flags().DurationVar(&inspectConnectTimeout, "connect-timeout", 30*time.Second, "Connection timeout")
	inspectCmd.Flags().DurationVar(&inspectDescriptorReadTimeout, "descriptor-timeout", 0, "Timeout for reading descriptor values (default: 2s if unset, 0 to skip descriptor reads)")
	inspectCmd.Flags().BoolVarP(&inspectVerbose, "verbose", "v", false, "Verbose output")
	inspectCmd.Flags().BoolVar(&inspectJSON, "json", false, "Output as JSON")
	inspectCmd.Flags().IntVar(&inspectReadLimit, "read-limit", 64, "Max bytes to read from readable characteristics (0 to disable reads)")
}

func runInspect(cmd *cobra.Command, args []string) error {
	address := args[0]

	cfg := config.DefaultConfig()
	if inspectVerbose {
		cfg.LogLevel = logrus.DebugLevel
	}

	// All arguments validated - don't show usage on runtime errors
	cmd.SilenceUsage = true

	logger := cfg.NewLogger()

	// Build inspect options
	opts := &inspector.InspectOptions{
		ConnectTimeout:        inspectConnectTimeout,
		DescriptorReadTimeout: inspectDescriptorReadTimeout,
	}

	// Use background context; per-command timeout is applied inside the inspector
	ctx := context.Background()

	// Setup progress printer
	progress := NewProgressPrinter(fmt.Sprintf("Inspecting device %s", address), "Connecting", "Processing results")
	progress.Start()
	defer progress.Stop()

	// Use Lua script for output generation
	processDevice := func(dev device.Device) (error, error) {
		return nil, executeInspectLuaScript(ctx, dev, logger)
	}

	_, err := inspector.InspectDevice(ctx, address, opts, logger, progress.Callback(), processDevice)
	return err
}

// executeInspectLuaScript runs the embedded inspect.lua script with the connected device
func executeInspectLuaScript(ctx context.Context, dev device.Device, logger *logrus.Logger) error {
	// Determine format based on --json flag
	format := "text"
	if inspectJSON {
		format = "json"
	}

	// Prepare script arguments
	args := map[string]string{
		"format": format,
	}

	// Execute the embedded script with output streaming
	return lua.ExecuteDeviceScriptWithOutput(
		ctx,
		dev,
		nil,
		logger,
		blecli.DefaultInspectLuaScript,
		args,
		os.Stdout,
		os.Stderr,
		50*time.Millisecond,
	)
}
