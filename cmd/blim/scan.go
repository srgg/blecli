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

	blelib "github.com/go-ble/ble"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/srg/blim/internal/device"
	"github.com/srg/blim/pkg/config"
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

	// All arguments validated - don't show usage on runtime errors
	cmd.SilenceUsage = true

	// Create configuration
	cfg := config.DefaultConfig()
	if scanVerbose {
		cfg.LogLevel = logrus.DebugLevel
	}
	cfg.OutputFormat = scanFormat
	if scanDuration > 0 {
		cfg.ScanTimeout = scanDuration
	}

	// For watch mode, default to indefinite scan if no duration specified
	if scanWatch && scanDuration == 0 {
		cfg.ScanTimeout = 0 // Indefinite
	}

	logger := cfg.NewLogger()

	// Create scanner
	s, err := scanner.NewScanner(logger)
	if err != nil {
		return fmt.Errorf("failed to create BLE scanner: %w", err)
	}

	// Parse service UUIDs if provided
	var serviceUUIDs []blelib.UUID
	for _, svcStr := range scanServices {
		uuid, err := blelib.Parse(svcStr)
		if err != nil {
			return fmt.Errorf("invalid service UUID '%s': %w", svcStr, err)
		}
		serviceUUIDs = append(serviceUUIDs, uuid)
	}

	// Create scan options
	scanOpts := &scanner.ScanOptions{
		Duration:        cfg.ScanTimeout,
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

func runSingleScan(scanner *scanner.Scanner, opts *scanner.ScanOptions, cfg *config.Config, logger *logrus.Logger) error {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	// Scan for a specified duration, or until interrupted by the user.
	ctx := blelib.WithSigHandler(context.WithTimeout(context.Background(), cfg.ScanTimeout))

	// Listen for Ctrl+C to cancel
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nCtrl+C pressed, cancelling scan...")
		if cb, ok := ctx.Value(blelib.ContextKeySig).(func()); ok {
			cb() // stop the scan
		}
	}()

	// Setup progress printer
	progress := NewCountdownProgressPrinter("Scanning for BLE devices", "Scanning", cfg.ScanTimeout, "Processing results")
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

func runWatchMode(scanner *scanner.Scanner, opts *scanner.ScanOptions, cfg *config.Config, logger *logrus.Logger) error {
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
	devicesMap := make(map[string]device.DeviceInfo)

	// Run the blocking scan in a goroutine
	scanErrCh := make(chan error, 1)
	go func() {
		var err error
		devicesMap, err = scanner.Scan(ctx, opts, nil) // No progress callback for watch mode
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
			// Scan completed (normally or with error)
			return printDeviceTable(err)
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

		case ev := <-scanner.Events():
			devicesMap[ev.DeviceInfo.GetAddress()] = ev.DeviceInfo
		}
	}
}

func displayDevicesTableFromMap(devices map[string]device.DeviceInfo, cfg *config.Config) error {
	if len(devices) == 0 {
		fmt.Println("No devices discovered")
		return nil
	}

	devList := make([]device.DeviceInfo, 0, len(devices))
	for _, d := range devices {
		devList = append(devList, d)
	}

	// Sort by RSSI
	sort.Slice(devList, func(i, j int) bool {
		rssi1 := devList[i].GetRSSI()
		rssi2 := devList[j].GetRSSI()
		return rssi1 > rssi2
	})

	switch cfg.OutputFormat {
	case "json":
		return displayDevicesJSON(devList)
	case "csv":
		return displayDevicesCSV(devList)
	default:
		return displayDevicesTable(devList)
	}
}

func displayDevicesTable(devices []device.DeviceInfo) error {
	var base io.Writer = os.Stdout
	if base == nil {
		base = io.Discard
	}
	w := tabwriter.NewWriter(base, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tADDRESS\tRSSI\tSERVICES\tLAST SEEN")
	fmt.Fprintln(w, strings.Repeat("-", 80))

	for _, dev := range devices {

		name := dev.DisplayName()
		if len(name) > 20 {
			name = name[:17] + "..."
		}

		// Join service UUIDs for display
		uuids := make([]string, 0, len(dev.GetAdvertisedServices()))
		for _, s := range dev.GetAdvertisedServices() {
			uuids = append(uuids, s)
		}
		services := strings.Join(uuids, ",")
		if len(services) > 30 {
			services = services[:27] + "..."
		}

		lastSeen := time.Since(dev.GetLastSeen()).Truncate(time.Second)

		fmt.Fprintf(w, "%s\t%s\t%d dBm\t%s\t%s ago\n",
			name, dev.GetAddress(), dev.GetRSSI(), services, lastSeen)
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

func displayDevicesCSV(devices []device.DeviceInfo) error {
	var w io.Writer = os.Stdout
	if w == nil {
		w = io.Discard
	}
	fmt.Fprintln(w, "Name,Address,RSSI,Services,LastSeen")
	for _, dev := range devices {
		uuids := make([]string, 0, len(dev.GetAdvertisedServices()))
		for _, s := range dev.GetAdvertisedServices() {
			uuids = append(uuids, s)
		}
		services := strings.Join(uuids, ";")
		fmt.Fprintf(w, "%s,%s,%d,%s,%s\n",
			dev.DisplayName(), dev.GetAddress(), dev.GetRSSI(), services, dev.GetLastSeen().Format(time.RFC3339))
	}
	return nil
}

func clearScreen() {
	var w io.Writer = os.Stdout
	if w == nil {
		w = io.Discard
	}
	fmt.Fprint(w, "\033[2J\033[H")
}
