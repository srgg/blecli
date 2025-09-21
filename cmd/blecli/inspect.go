package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	blecli "github.com/srg/blecli/pkg/ble"
	"github.com/srg/blecli/pkg/config"
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
	inspectConnectTimeout time.Duration
	inspectVerbose        bool
	inspectJSON           bool
	inspectReadLimit      int
)

func init() {
	inspectCmd.Flags().DurationVar(&inspectConnectTimeout, "connect-timeout", 30*time.Second, "Connection timeout")
	inspectCmd.Flags().BoolVarP(&inspectVerbose, "verbose", "v", false, "Verbose output")
	inspectCmd.Flags().BoolVar(&inspectJSON, "json", false, "Output as JSON")
	inspectCmd.Flags().IntVar(&inspectReadLimit, "read-limit", 64, "Max bytes to read from readable characteristics (0 to disable reads)")
}

func runInspect(cmd *cobra.Command, args []string) error {
	address := args[0]

	// Create configuration/logger consistent with other commands
	cfg := config.DefaultConfig()
	if inspectVerbose {
		cfg.LogLevel = logrus.DebugLevel
	}
	logger := cfg.NewLogger()

	// Build inspect options
	opts := &blecli.InspectOptions{
		ConnectTimeout: inspectConnectTimeout,
		ReadLimit:      inspectReadLimit,
	}

	// Use background context; per-command timeout is applied inside the inspector
	ctx := context.Background()
	res, err := blecli.InspectDevice(ctx, address, opts, logger)
	if err != nil {
		return err
	}

	if inspectJSON {
		enc := json.NewEncoder(cmdOut())
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}

	outputInspectText(res)
	return nil
}

func outputInspectText(res *blecli.InspectResult) {
	// Device info first
	fmt.Fprintln(cmdOut(), "Device info:")
	if res.Device != nil {
		d := res.Device
		fmt.Fprintf(cmdOut(), "  ID: %s\n", d.GetID())
		fmt.Fprintf(cmdOut(), "  Address: %s\n", d.GetAddress())
		if d.GetName() != "" {
			fmt.Fprintf(cmdOut(), "  Name: %s\n", d.GetName())
		}
		fmt.Fprintf(cmdOut(), "  RSSI: %d\n", d.GetRSSI())
		fmt.Fprintf(cmdOut(), "  Connectable: %t\n", d.IsConnectable())
		if d.GetTxPower() != nil {
			fmt.Fprintf(cmdOut(), "  TxPower: %d dBm\n", *d.GetTxPower())
		}
		fmt.Fprintf(cmdOut(), "  LastSeen: %s\n", d.GetLastSeen().Format(time.RFC3339))

		// Services (UUIDs)
		if len(d.GetServices()) > 0 {
			fmt.Fprintln(cmdOut(), "  Services:")
			for _, s := range d.GetServices() {
				fmt.Fprintf(cmdOut(), "    - %s\n", s.GetUUID())
			}
		} else {
			fmt.Fprintln(cmdOut(), "  Services: none")
		}

		// Manufacturer data
		if len(d.GetManufacturerData()) > 0 {
			fmt.Fprintf(cmdOut(), "  Manufacturer Data: %X\n", d.GetManufacturerData())
		} else {
			fmt.Fprintln(cmdOut(), "  Manufacturer Data: none")
		}

		// Service data
		if len(d.GetServiceData()) > 0 {
			fmt.Fprintln(cmdOut(), "  Service Data:")
			keys := make([]string, 0, len(d.GetServiceData()))
			for k := range d.GetServiceData() {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(cmdOut(), "    - %s: %X\n", k, d.GetServiceData()[k])
			}
		} else {
			fmt.Fprintln(cmdOut(), "  Service Data: none")
		}
	} else {
		fmt.Fprintf(cmdOut(), "  Address: %s\n", res.Address)
		if res.Name != "" {
			fmt.Fprintf(cmdOut(), "  Name: %s\n", res.Name)
		}
	}
	count := len(res.Services)
	if res.Device != nil && len(res.Device.GetServices()) > 0 {
		count = len(res.Device.GetServices())
	}
	fmt.Fprintf(cmdOut(), "  GATT Services: %d\n", count)

	// Then list services
	for si, svc := range res.Services {
		fmt.Fprintf(cmdOut(), "\n[%d] Service %s\n", si+1, svc.UUID)
		for ci, ch := range svc.Characteristics {
			fmt.Fprintf(cmdOut(), "  [%d.%d] Characteristic %s (props: %s)\n", si+1, ci+1, ch.UUID, ch.Properties)
			if ch.ValueHex != "" || ch.ValueASCII != "" {
				if ch.ValueHex != "" {
					fmt.Fprintf(cmdOut(), "      value (hex):   %s\n", ch.ValueHex)
				}
				if ch.ValueASCII != "" {
					fmt.Fprintf(cmdOut(), "      value (ascii): %s\n", ch.ValueASCII)
				}
			}
			for _, d := range ch.Descriptors {
				fmt.Fprintf(cmdOut(), "      descriptor: %s\n", d.UUID)
			}
		}
	}
}

// cmdOut returns a writer to standard output
func cmdOut() io.Writer {
	return os.Stdout
}
