package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "blim",
	Short: "BLE CLI Tool for device scanning and communication",
	Long: `A Bluetooth Low Energy (BLE) command-line tool for macOS that provides:

- BLE device scanning
- Connection management
- Characteristic read/write operations
- Notification subscriptions
- PTY bridge for serial device emulation

Perfect for IoT development, device testing, and BLE protocol exploration.`,
	Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// Add subcommands
	rootCmd.AddCommand(scanCmd)
	rootCmd.AddCommand(bridgeCmd)
	rootCmd.AddCommand(inspectCmd)

	// Global flags
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().String("log-level", "", "Log level (debug, info, warn, error)")
}
