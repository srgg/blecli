//go:build test

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/srg/blim/internal/device"
	goble "github.com/srg/blim/internal/device/go-ble"
	"github.com/srg/blim/internal/testutils"
)

// Test device addresses for consistent mock device identification
const (
	TestDeviceAddress1 = "00:00:00:00:00:01"
	TestDeviceAddress2 = "00:00:00:00:00:02"
)

// CommandTestSuite extends MockBLEPeripheralSuite with command testing utilities.
// All cmd/blim test suites should embed this instead of MockBLEPeripheralSuite.
type CommandTestSuite struct {
	testutils.MockBLEPeripheralSuite
}

// ConnectDevice connects to mock device and returns cleanup function.
// Uses TestDeviceAddress1 if address is empty.
func (s *CommandTestSuite) ConnectDevice(address string) (device.Device, func()) {
	if address == "" {
		address = TestDeviceAddress1
	}
	dev := goble.NewBLEDeviceWithAddress(address, s.Logger)
	ctx := context.Background()
	err := dev.Connect(ctx, &device.ConnectOptions{ConnectTimeout: 5 * time.Second})
	s.Require().NoError(err, "connection MUST succeed")
	return dev, func() { _ = dev.Disconnect() }
}

// CaptureStdout executes fn while capturing stdout, returns captured output.
// Stdout is restored even if fn panics.
func (s *CommandTestSuite) CaptureStdout(fn func()) string {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	s.Require().NoError(err, "pipe creation MUST succeed")
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	fn()

	w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

// ExecuteCommand runs a cobra command with args, returns output and error.
func (s *CommandTestSuite) ExecuteCommand(cmd *cobra.Command, args ...string) (string, error) {
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}
