package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/go-ble/ble"
	"github.com/spf13/cobra"
	"github.com/sirupsen/logrus"

	blecli "github.com/srg/blecli/pkg/ble"
	"github.com/srg/blecli/pkg/config"
	"github.com/srg/blecli/pkg/device"
)

// scanCmd represents the scan command
var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan for BLE devices",
	Long: `Scan for and display Bluetooth Low Energy devices in the vicinity.

This command will scan for BLE devices and display information about
discovered devices including their names, addresses, RSSI values, and
advertised services.`,
	RunE: runScan,
}

var (
	scanDuration    time.Duration
	scanFormat      string
	scanVerbose     bool
	scanServices    []string
	scanAllowList   []string
	scanBlockList   []string
	scanNoDuplicate bool
	scanWatch       bool
)

func init() {
	scanCmd.Flags().DurationVarP(&scanDuration, "duration", "d", 10*time.Second, "Scan duration (0 for indefinite)")
	scanCmd.Flags().StringVarP(&scanFormat, "format", "f", "table", "Output format (table, json, csv)")
	scanCmd.Flags().BoolVarP(&scanVerbose, "verbose", "v", false, "Verbose output")
	scanCmd.Flags().StringSliceVarP(&scanServices, "services", "s", nil, "Filter by service UUIDs")
	scanCmd.Flags().StringSliceVar(&scanAllowList, "allow", nil, "Only show devices with these addresses")
	scanCmd.Flags().StringSliceVar(&scanBlockList, "block", nil, "Hide devices with these addresses")
	scanCmd.Flags().BoolVar(&scanNoDuplicate, "no-duplicates", true, "Filter duplicate advertisements")
	scanCmd.Flags().BoolVarP(&scanWatch, "watch", "w", false, "Continuously scan and update results")
}

func runScan(cmd *cobra.Command, args []string) error {
	// Validate format parameter
	validFormats := []string{"table", "json", "csv"}
	isValidFormat := false
	for _, format := range validFormats {
		if scanFormat == format {
			isValidFormat = true
			break
		}
	}
	if !isValidFormat {
		return fmt.Errorf("invalid format '%s': must be one of %v", scanFormat, validFormats)
	}

	// Create configuration
	cfg := config.DefaultConfig()
	if scanVerbose {
		cfg.LogLevel = logrus.DebugLevel
	}
	cfg.OutputFormat = scanFormat
	if scanDuration > 0 {
		cfg.ScanTimeout = scanDuration
	}

	logger := cfg.NewLogger()

	// Create scanner
	scanner, err := blecli.NewScanner(logger)
	if err != nil {
		return fmt.Errorf("failed to create BLE scanner: %w", err)
	}

	// Parse service UUIDs if provided
	var serviceUUIDs []ble.UUID
	for _, svcStr := range scanServices {
		uuid, err := ble.Parse(svcStr)
		if err != nil {
			return fmt.Errorf("invalid service UUID '%s': %w", svcStr, err)
		}
		serviceUUIDs = append(serviceUUIDs, uuid)
	}

	// Create scan options
	scanOpts := &blecli.ScanOptions{
		Duration:        cfg.ScanTimeout,
		DuplicateFilter: scanNoDuplicate,
		ServiceUUIDs:    serviceUUIDs,
		AllowList:       scanAllowList,
		BlockList:       scanBlockList,
	}

	if scanWatch {
		return runWatchMode(scanner, scanOpts, cfg)
	}

	return runSingleScan(scanner, scanOpts, cfg)
}

func runSingleScan(scanner *blecli.Scanner, opts *blecli.ScanOptions, cfg *config.Config) error {
	// Handle interrupts gracefully
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("\nScan interrupted by user")
		cancel()
	}()

	// Perform scan
	err := scanner.Scan(ctx, opts)
	if err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}

	// Get and display results
	devices := scanner.GetDevices()
	return displayDevices(devices, cfg)
}

func runWatchMode(scanner *blecli.Scanner, opts *blecli.ScanOptions, cfg *config.Config) error {
	// Handle interrupts gracefully
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("\nWatch mode interrupted by user")
		cancel()
	}()

	// Continuous scanning with periodic updates
	updateInterval := 2 * time.Second
	ticker := time.NewTicker(updateInterval)
	defer ticker.Stop()

	// Start initial scan in background
	go func() {
		opts.Duration = 0 // Indefinite scan for watch mode
		if err := scanner.Scan(ctx, opts); err != nil && err != context.Canceled {
			fmt.Printf("Scan error: %v\n", err)
		}
	}()

	// Display updates
	fmt.Println("Starting watch mode (Press Ctrl+C to stop)...")
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			devices := scanner.GetDevices()
			clearScreen()
			fmt.Printf("BLE Devices (Last updated: %s)\n", time.Now().Format("15:04:05"))
			fmt.Println(strings.Repeat("=", 80))
			if err := displayDevices(devices, cfg); err != nil {
				return err
			}
		}
	}
}

func displayDevices(devices []*device.Device, cfg *config.Config) error {
	if len(devices) == 0 {
		fmt.Println("No devices discovered")
		return nil
	}

	// Sort devices by RSSI (strongest first)
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].RSSI > devices[j].RSSI
	})

	switch cfg.OutputFormat {
	case "json":
		return displayDevicesJSON(devices)
	case "csv":
		return displayDevicesCSV(devices)
	default:
		return displayDevicesTable(devices)
	}
}

func displayDevicesTable(devices []*device.Device) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tADDRESS\tRSSI\tSERVICES\tLAST SEEN")
	fmt.Fprintln(w, strings.Repeat("-", 80))

	for _, dev := range devices {
		name := dev.DisplayName()
		if len(name) > 20 {
			name = name[:17] + "..."
		}

		services := strings.Join(dev.Services, ",")
		if len(services) > 30 {
			services = services[:27] + "..."
		}

		lastSeen := time.Since(dev.LastSeen).Truncate(time.Second)

		fmt.Fprintf(w, "%s\t%s\t%d dBm\t%s\t%s ago\n",
			name, dev.Address, dev.RSSI, services, lastSeen)
	}

	return w.Flush()
}

func displayDevicesJSON(devices []*device.Device) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(devices)
}

func displayDevicesCSV(devices []*device.Device) error {
	fmt.Println("Name,Address,RSSI,Services,LastSeen")
	for _, dev := range devices {
		services := strings.Join(dev.Services, ";")
		fmt.Printf("%s,%s,%d,%s,%s\n",
			dev.DisplayName(), dev.Address, dev.RSSI, services, dev.LastSeen.Format(time.RFC3339))
	}
	return nil
}

func clearScreen() {
	fmt.Print("\033[2J\033[H")
}