package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/srg/blim/internal/device"
	"github.com/srg/blim/scanner"
)

// scanCmd represents the scan command
var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan for BLE devices",
	Long: `Scan for and display Bluetooth Low Energy devices in the vicinity.

This command will scan for BLE devices and display information about
discovered devices, including their names, addresses, RSSI values, and
advertised services.`,
	RunE: runScan,
}

var (
	scanDuration    time.Duration
	scanFormat      string
	scanServices    []string
	scanAllowList   []string
	scanBlockList   []string
	scanNoDuplicate bool
	scanWatch       bool
)

type scanConfig struct {
	scanTimeout  time.Duration
	outputFormat string
}

func defaultScanConfig() *scanConfig {
	return &scanConfig{
		scanTimeout:  10 * time.Second,
		outputFormat: "table",
	}
}

func init() {
	scanCmd.Flags().DurationVarP(&scanDuration, "duration", "d", 10*time.Second, "Scan duration (0 for indefinite)")
	scanCmd.Flags().StringVarP(&scanFormat, "format", "f", "table", "Output format (table, json)")
	scanCmd.Flags().StringSliceVarP(&scanServices, "services", "s", nil, "Filter by service UUIDs")
	scanCmd.Flags().StringSliceVar(&scanAllowList, "allow", nil, "Only show devices with these addresses")
	scanCmd.Flags().StringSliceVar(&scanBlockList, "block", nil, "Hide devices with these addresses")
	scanCmd.Flags().BoolVar(&scanNoDuplicate, "no-duplicates", true, "Filter duplicate advertisements")
	scanCmd.Flags().BoolVarP(&scanWatch, "watch", "w", false, "Continuously scan and update results")
}

func runScan(cmd *cobra.Command, args []string) error {
	// Validate format parameter
	validFormats := []string{"table", "json"}
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

	// Configure logger based on --log-level and --verbose flags
	logger, err := configureLogger(cmd, "verbose")
	if err != nil {
		return err
	}

	// All arguments validated - don't show usage on runtime errors
	cmd.SilenceUsage = true

	// Create configuration
	cfg := defaultScanConfig()
	cfg.outputFormat = scanFormat
	if scanDuration > 0 {
		cfg.scanTimeout = scanDuration
	}

	// For watch mode, default to indefinite scan if no duration specified
	if scanWatch && scanDuration == 0 {
		cfg.scanTimeout = 0 // Indefinite
	}

	// Create scanner
	s, err := scanner.NewScanner(logger)
	if err != nil {
		return fmt.Errorf("failed to create BLE scanner: %w", err)
	}

	// Validate and normalize service UUIDs if provided
	var serviceUUIDs []string
	if len(scanServices) > 0 {
		var err error
		serviceUUIDs, err = device.ValidateUUID(scanServices...)
		if err != nil {
			return fmt.Errorf("invalid service UUID: %w", err)
		}
	}

	// Create scan options
	scanOpts := &scanner.ScanOptions{
		Duration:        cfg.scanTimeout,
		DuplicateFilter: scanNoDuplicate,
		ServiceUUIDs:    serviceUUIDs,
		AllowList:       scanAllowList,
		BlockList:       scanBlockList,
	}

	if scanWatch {
		return runWatchMode(s, scanOpts, cfg, logger)
	}

	return runSingleScan(s, scanOpts, cfg, logger)
}

func runSingleScan(scanner *scanner.Scanner, opts *scanner.ScanOptions, cfg *scanConfig, logger *logrus.Logger) error {
	if cfg == nil {
		cfg = defaultScanConfig()
	}

	// Create context with timeout
	baseCtx := context.Background()
	if cfg.scanTimeout > 0 {
		var cancel context.CancelFunc
		baseCtx, cancel = context.WithTimeout(baseCtx, cfg.scanTimeout)
		defer cancel()
	}

	// Create a cancellable context for signal handling
	ctx, cancel := context.WithCancel(baseCtx)
	defer cancel()

	// Listen for Ctrl+C to cancel
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		<-sigCh
		fmt.Println("\nCtrl+C pressed, cancelling scan...")
		cancel()
	}()

	// Setup progress printer
	progress := NewCountdownProgressPrinter("Scanning for BLE devices", "Scanning", cfg.scanTimeout, "Processing results")
	progress.Start()
	defer progress.Stop()

	// Perform scan
	devices, err := scanner.Scan(ctx, opts, progress.Callback())

	if err != nil && !errors.Is(err, context.Canceled) {
		logger.WithError(err).Error("scan failed")
		return err
	}
	return displayDevicesTableFromMap(devices, cfg)
}

func runWatchMode(s *scanner.Scanner, opts *scanner.ScanOptions, cfg *scanConfig, logger *logrus.Logger) error {
	// Scan until interrupted by the user.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up our own signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		<-sigCh

		fmt.Println("\nCtrl+C pressed, cancelling scan...")
		cancel()
	}()

	// Start collecting events immediately BEFORE starting the scan
	// Use deviceEntry to track both device info and last seen timestamp
	devicesMap := make(map[string]scanner.DeviceEntry)

	// Run the blocking scan in a goroutine
	scanErrCh := make(chan error, 1)
	go func() {
		var err error
		devicesMap, err = s.Scan(ctx, opts, nil) // No progress callback for watch mode
		scanErrCh <- err
		close(scanErrCh)
	}()

	printDeviceTable := func(err error) error {
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}

		clearScreen()
		return displayDevicesTableFromMap(devicesMap, cfg)
	}

	// Add a ticker to check the timeout periodically and avoid channel starvation
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	tickCount := 0

	for {
		select {
		case <-ctx.Done():
			return printDeviceTable(ctx.Err())

		case err := <-scanErrCh:
			// Scan completed
			// If there's a real error (not just cancellation), exit
			if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				return printDeviceTable(err)
			}
			// Otherwise, scan completed normally - continue watching events indefinitely
		case <-ticker.C:
			// Periodic check to ensure ctx.Done() gets a chance to be processed
			// This prevents channel starvation from the busy events channel
			select {
			case <-ctx.Done():
				return printDeviceTable(nil)
			default:
				// Continue processing events
			}

			// Periodic print of device table
			tickCount++

			if tickCount == 10 {
				_ = printDeviceTable(nil)
				tickCount = 0
			}

		case ev := <-s.Events():
			devicesMap[ev.DeviceInfo.Address()] = scanner.DeviceEntry{
				Device:   ev.DeviceInfo,
				LastSeen: ev.Timestamp,
			}
		}
	}
}

type deviceWithTime struct {
	device.DeviceInfo
	lastSeen time.Time
}

func displayDevicesTableFromMap(enties map[string]scanner.DeviceEntry, cfg *scanConfig) error {
	if len(enties) == 0 {
		fmt.Println("No devices discovered")
		return nil
	}

	// Track last seen time for scan display
	devList := make([]scanner.DeviceEntry, 0, len(enties))
	for _, e := range enties {
		devList = append(devList, e)
	}

	// Sort by Name
	sort.Slice(devList, func(i, j int) bool {
		return devList[i].Device.Name() > devList[j].Device.Name()
	})

	switch cfg.outputFormat {
	case "json":
		// Extract just the DeviceInfo for JSON
		infoList := make([]device.DeviceInfo, len(devList))
		for i, d := range devList {
			infoList[i] = d.Device
		}
		return displayDevicesJSON(infoList)
	default:
		return displayDevicesTable(devList)
	}
}

func displayDevicesTable(entries []scanner.DeviceEntry) error {
	var base io.Writer = os.Stdout
	if base == nil {
		base = io.Discard
	}
	w := tabwriter.NewWriter(base, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tADDRESS\tRSSI\tSERVICES\tLAST SEEN")
	fmt.Fprintln(w, strings.Repeat("-", 80))

	for _, e := range entries {
		dev := e.Device
		name := dev.Name()
		if len(name) > 20 {
			name = name[:17] + "..."
		}

		// Join service UUIDs for display
		uuids := make([]string, 0, len(dev.AdvertisedServices()))
		for _, s := range dev.AdvertisedServices() {
			uuids = append(uuids, s)
		}
		services := strings.Join(uuids, ",")
		if len(services) > 30 {
			services = services[:27] + "..."
		}

		lastSeen := time.Since(e.LastSeen).Truncate(time.Second)

		fmt.Fprintf(w, "%s\t%s\t%d dBm\t%s\t%s ago\n",
			name, dev.Address(), dev.RSSI(), services, lastSeen)
	}

	return w.Flush()
}

func displayDevicesJSON(devices []device.DeviceInfo) error {
	var w io.Writer = os.Stdout
	if w == nil {
		w = io.Discard
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(devices)
}

func clearScreen() {
	var w io.Writer = os.Stdout
	if w == nil {
		w = io.Discard
	}
	fmt.Fprint(w, "\033[2J\033[H")
}
