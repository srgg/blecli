package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/srg/blim/inspector"
	"github.com/srg/blim/internal/device"
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

	cfg := config.DefaultConfig()
	if inspectVerbose {
		cfg.LogLevel = logrus.DebugLevel
	}

	// All arguments validated - don't show usage on runtime errors
	cmd.SilenceUsage = true

	logger := cfg.NewLogger()

	// Build inspect options
	opts := &inspector.InspectOptions{
		ConnectTimeout: inspectConnectTimeout,
	}

	// Use background context; per-command timeout is applied inside the inspector
	ctx := context.Background()
	dev, err := inspector.InspectDevice(ctx, address, opts, logger)
	if err != nil {
		return err
	}
	defer dev.Disconnect()

	if inspectJSON {
		return outputInspectJSON(dev, inspectReadLimit)
	}

	outputInspectText(dev, inspectReadLimit)
	return nil
}

func outputInspectText(dev device.Device, readLimit int) {
	// Device info first
	fmt.Fprintln(cmdOut(), "Device info:")
	fmt.Fprintf(cmdOut(), "  ID: %s\n", dev.GetID())
	fmt.Fprintf(cmdOut(), "  Address: %s\n", dev.GetAddress())
	if dev.GetName() != "" {
		fmt.Fprintf(cmdOut(), "  Name: %s\n", dev.GetName())
	}
	fmt.Fprintf(cmdOut(), "  RSSI: %d\n", dev.GetRSSI())
	fmt.Fprintf(cmdOut(), "  Connectable: %t\n", dev.IsConnectable())
	if dev.GetTxPower() != nil {
		fmt.Fprintf(cmdOut(), "  TxPower: %d dBm\n", *dev.GetTxPower())
	}
	fmt.Fprintf(cmdOut(), "  LastSeen: %s\n", dev.GetLastSeen().Format(time.RFC3339))

	// Advertised Services
	if len(dev.GetAdvertisedServices()) > 0 {
		fmt.Fprintln(cmdOut(), "  Advertised Services:")
		for _, s := range dev.GetAdvertisedServices() {
			fmt.Fprintf(cmdOut(), "    - %s\n", s)
		}
	} else {
		fmt.Fprintln(cmdOut(), "  Advertised Services: none")
	}

	// Manufacturer data
	if len(dev.GetManufacturerData()) > 0 {
		fmt.Fprintf(cmdOut(), "  Manufacturer Data: %X\n", dev.GetManufacturerData())
	} else {
		fmt.Fprintln(cmdOut(), "  Manufacturer Data: none")
	}

	// Service data
	if len(dev.GetServiceData()) > 0 {
		fmt.Fprintln(cmdOut(), "  Service Data:")
		keys := make([]string, 0, len(dev.GetServiceData()))
		for k := range dev.GetServiceData() {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(cmdOut(), "    - %s: %X\n", k, dev.GetServiceData()[k])
		}
	} else {
		fmt.Fprintln(cmdOut(), "  Service Data: none")
	}

	// GATT Services from connection
	conn := dev.GetConnection()
	services := conn.GetServices()
	fmt.Fprintf(cmdOut(), "  GATT Services: %d\n", len(services))

	// List services
	si := 0
	for _, svc := range services {
		si++
		fmt.Fprintf(cmdOut(), "\n[%d] Service %s\n", si, svc.GetUUID())

		ci := 0
		for _, char := range svc.GetCharacteristics() {
			ci++
			fmt.Fprintf(cmdOut(), "  [%d.%d] Characteristic %s (props: %s)\n", si, ci, char.GetUUID(), char.GetProperties())

			// Optionally show characteristic values
			if readLimit > 0 {
				data := char.GetValue()
				if len(data) > 0 {
					trim := data
					if len(trim) > readLimit {
						trim = trim[:readLimit]
					}
					valueHex := strings.ToUpper(hex.EncodeToString(trim))
					valueASCII := asciiPreview(trim)

					if valueHex != "" {
						fmt.Fprintf(cmdOut(), "      value (hex):   %s\n", valueHex)
					}
					if valueASCII != "" {
						fmt.Fprintf(cmdOut(), "      value (ascii): %s\n", valueASCII)
					}
				}
			}

			// Descriptors
			for _, d := range char.GetDescriptors() {
				fmt.Fprintf(cmdOut(), "      descriptor: %s\n", d.GetUUID())
			}
		}
	}
}

func outputInspectJSON(dev device.Device, readLimit int) error {
	// Build JSON structure directly from device
	conn := dev.GetConnection()
	services := conn.GetServices()

	type DescriptorJSON struct {
		UUID string `json:"uuid"`
	}

	type CharacteristicJSON struct {
		UUID        string           `json:"uuid"`
		Properties  string           `json:"properties"`
		ValueHex    string           `json:"value_hex,omitempty"`
		ValueASCII  string           `json:"value_ascii,omitempty"`
		Descriptors []DescriptorJSON `json:"descriptors,omitempty"`
	}

	type ServiceJSON struct {
		UUID            string               `json:"uuid"`
		Characteristics []CharacteristicJSON `json:"characteristics"`
	}

	type InspectJSON struct {
		Address  string        `json:"address"`
		Name     string        `json:"name,omitempty"`
		Services []ServiceJSON `json:"services"`
	}

	result := InspectJSON{
		Address:  dev.GetAddress(),
		Name:     dev.GetName(),
		Services: make([]ServiceJSON, 0, len(services)),
	}

	for _, svc := range services {
		svcJSON := ServiceJSON{
			UUID:            svc.GetUUID(),
			Characteristics: make([]CharacteristicJSON, 0),
		}

		for _, char := range svc.GetCharacteristics() {
			charJSON := CharacteristicJSON{
				UUID:       char.GetUUID(),
				Properties: char.GetProperties(),
			}

			// Optionally include values
			if readLimit > 0 {
				data := char.GetValue()
				if len(data) > 0 {
					trim := data
					if len(trim) > readLimit {
						trim = trim[:readLimit]
					}
					charJSON.ValueHex = strings.ToUpper(hex.EncodeToString(trim))
					charJSON.ValueASCII = asciiPreview(trim)
				}
			}

			// Descriptors
			for _, d := range char.GetDescriptors() {
				charJSON.Descriptors = append(charJSON.Descriptors, DescriptorJSON{UUID: d.GetUUID()})
			}

			svcJSON.Characteristics = append(svcJSON.Characteristics, charJSON)
		}

		result.Services = append(result.Services, svcJSON)
	}

	enc := json.NewEncoder(cmdOut())
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

// asciiPreview returns a safe ASCII preview, replacing non-printable bytes with '.'
func asciiPreview(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		if c >= 32 && c <= 126 {
			sb.WriteByte(c)
		} else {
			sb.WriteByte('.')
		}
	}
	return sb.String()
}

// cmdOut returns a writer to standard output
func cmdOut() io.Writer {
	return os.Stdout
}
