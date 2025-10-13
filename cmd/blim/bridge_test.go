package main

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/testutils"
	"github.com/stretchr/testify/suite"
)

// BridgeCmdTestSuite tests the bridge command via the runBridge function
type BridgeCmdTestSuite struct {
	testutils.MockBLEPeripheralSuite
	originalFlags struct {
		serviceUUID    string
		connectTimeout time.Duration
		verbose        bool
		luaScript      string
	}
}

// SetupSuite saves original flags and sets up a mock peripheral
func (suite *BridgeCmdTestSuite) SetupSuite() {
	// Save original flag values
	suite.originalFlags.serviceUUID = bridgeServiceUUID
	suite.originalFlags.connectTimeout = bridgeConnectTimeout
	suite.originalFlags.luaScript = bridgeLuaScript
}

// TearDownSuite restores original flags
func (suite *BridgeCmdTestSuite) TearDownSuite() {
	bridgeServiceUUID = suite.originalFlags.serviceUUID
	bridgeConnectTimeout = suite.originalFlags.connectTimeout
	bridgeLuaScript = suite.originalFlags.luaScript
}

// SetupTest initializes the test environment with a bridge-compatible peripheral
func (suite *BridgeCmdTestSuite) SetupTest() {
	// Create a peripheral with services that bridge.lua expects
	suite.WithPeripheral().
		FromJSON(`{
			"services": [
				{
					"uuid": "1234",
					"characteristics": [
						{"uuid": "5678", "properties": "read,notify", "value": []}
					]
				},
				{
					"uuid": "180d",
					"characteristics": [
						{"uuid": "2a37", "properties": "read,notify", "value": []},
						{"uuid": "2a38", "properties": "read,notify", "value": []}
					]
				},
				{
					"uuid": "180f",
					"characteristics": [
						{"uuid": "2a19", "properties": "read,notify", "value": []}
					]
				},
				{
					"uuid": "6e400001-b5a3-f393-e0a9-e50e24dcca9e",
					"characteristics": [
						{"uuid": "6e400002-b5a3-f393-e0a9-e50e24dcca9e", "properties": "read,write,notify", "value": []},
						{"uuid": "6e400003-b5a3-f393-e0a9-e50e24dcca9e", "properties": "read,notify", "value": []}
					]
				}
			]
		}`).
		Build()

	suite.MockBLEPeripheralSuite.SetupTest()

	// Reset flags to defaults
	bridgeServiceUUID = "6E400001-B5A3-F393-E0A9-E50E24DCCA9E"
	bridgeConnectTimeout = 30 * time.Second
	bridgeLuaScript = ""

	// Reset command flags
	bridgeCmd.ResetFlags()
	bridgeCmd.Flags().StringVar(&bridgeServiceUUID, "service", "6E400001-B5A3-F393-E0A9-E50E24DCCA9E", "BLE service UUID to bridge with")
	bridgeCmd.Flags().DurationVar(&bridgeConnectTimeout, "connect-timeout", 30*time.Second, "Connection timeout")
	bridgeCmd.Flags().StringVar(&bridgeLuaScript, "script", "", "Lua script file")
}

//// TestBridgeCommandOutput tests runBridge function and verifies stdout output
//func (suite *BridgeCmdTestSuite) TestBridgeCommandOutput() {
//	// GOAL: Test runBridge command function with os.Stdout capture
//	//
//	// TEST SCENARIO: Execute bridgeCmd with device address → runBridge executes →
//	// bridge header printed to os.Stdout → verify output
//	//
//	// NOTE: This test only verifies the bridge header output to stdout.
//	// Notification output goes to PTY master (not stdout), but the PTY monitoring code
//	// in RunDeviceBridge is commented out (bridge.go:706-782), so notifications cannot
//	// be verified in this test. To test notifications, the PTY monitoring code would need
//	// to be uncommented and the test would need to read from the PTY master.
//
//	// Redirect os.Stdout to capture output
//	oldStdout := os.Stdout
//	r, w, err := os.Pipe()
//	suite.Require().NoError(err)
//	os.Stdout = w
//
//	// Restore os.Stdout after test
//	defer func() {
//		os.Stdout = oldStdout
//	}()
//
//	// Create root command with bridge subcommand
//	rootCmd := &cobra.Command{Use: "blim"}
//	rootCmd.AddCommand(bridgeCmd)
//	rootCmd.SetArgs([]string{"bridge", "00:00:00:00:00:01"})
//
//	// Run bridge command in the background with timeout
//	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
//	defer cancel()
//
//	// Channel to capture output
//	outputCh := make(chan string, 1)
//	go func() {
//		var buf bytes.Buffer
//		_, _ = buf.ReadFrom(r)
//		outputCh <- buf.String()
//	}()
//
//	errCh := make(chan error, 1)
//	go func() {
//		errCh <- rootCmd.ExecuteContext(ctx)
//	}()
//
//	// Wait for bridge to start and execute
//	time.Sleep(500 * time.Millisecond)
//
//	// Cancel to stop bridge
//	cancel()
//
//	// Wait for command to finish (with timeout - bridge may block on ctx.Done())
//	select {
//	case err := <-errCh:
//		// Context cancellation is expected
//		if err != nil && err != context.Canceled && err.Error() != "context canceled" {
//			suite.T().Logf("Bridge error (may be expected): %v", err)
//		}
//	case <-time.After(3 * time.Second):
//		// Bridge blocks on <-ctx.Done() - timeout is expected
//		suite.T().Log("Bridge command timeout (expected - bridge blocks until context done)")
//	}
//
//	// Close write end to flush output
//	w.Close()
//
//	// Get captured output
//	var output string
//	select {
//	case output = <-outputCh:
//	case <-time.After(1 * time.Second):
//		suite.Fail("Timeout waiting for output")
//	}
//
//	suite.T().Logf("Captured stdout:\n%s", output)
//
//	// Expected header output (from bridge-test-test-scenarios.yaml)
//	// NOTE: Notification output goes to PTY master, not stdout, so not verified here
//	// NOTE: Progress printer output is included with ANSI escape codes
//	expectedStdout := "\rStarting bridge for 00:00:00:00:00:01 (Connecting...)   \r\x1b[K\n" +
//		`=== BLE-PTY Bridge is Active ===
//Device: 00:00:00:00:00:01
//Service: 1234
//Characteristics: 1
//  - 5678
//
//Service: 180d
//Characteristics: 2
//  - 2a37
//  - 2a38
//
//Service: 180f
//Characteristics: 1
//  - 2a19
//
//Service: 6e400001b5a3f393e0a9e50e24dcca9e
//Characteristics: 2
//  - 6e400002b5a3f393e0a9e50e24dcca9e
//  - 6e400003b5a3f393e0a9e50e24dcca9e
//
//Bridge is running. Press Ctrl+C to stop the bridge.
//`
//
//	// Use TextAsserter to verify the output matches the expectation
//	testutils.NewTextAsserter(suite.T()).
//		WithOptions(
//			testutils.WithTrimSpace(true),
//			testutils.WithIgnoreTrailingWhitespace(true),
//		).
//		Assert(output, expectedStdout)
//}

// TestBridgeCmdTestSuite runs the test suite
func TestBridgeCmdTestSuite(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Reduce noise in test output

	bridgeSuite := &BridgeCmdTestSuite{}
	bridgeSuite.Logger = logger

	suite.Run(t, bridgeSuite)
}
