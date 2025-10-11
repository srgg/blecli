package bridge

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

const (
	// testSyncWait is the delay to allow async operations to complete in tests
	testSyncWait = 100 * time.Millisecond

	// maxShutdownDuration is the maximum acceptable time for bridge shutdown
	maxShutdownDuration = 1 * time.Second

	// signalTestTimeout is the timeout for signal handling test (uses timeout to simulate Ctrl+C)
	signalTestTimeout = 2 * time.Second
)

// BridgeTestSuite runs tests for Bridge using the BridgeSuite infrastructure.
type BridgeTestSuite struct {
	BridgeSuite
}

func (suite *BridgeTestSuite) TestBridgeScenarios() {
	// GOAL: Verify bridge handles all YAML-defined general test scenarios
	//
	// TEST SCENARIO: Load YAML test-scenarios → execute each test case → all assertions pass

	suite.RunTestCasesFromFile("./test-scenarios/bridge-test-scenarios.yaml")
}

func (suite *BridgeTestSuite) TestPTYWriteYAML() {
	// GOAL: Verify the pty_write() function works correctly with various data types and scenarios
	//
	// TEST SCENARIO: Load YAML test cases for pty_write → execute each test → all pass

	suite.RunTestCasesFromFile("./test-scenarios/bridge-pty-write-tests.yaml")
}

func (suite *BridgeTestSuite) TestPTYReadYAML() {
	// GOAL: Verify the pty_read() function works correctly with various scenarios
	//
	// TEST SCENARIO: Load YAML test cases for pty_read → execute each test → all pass

	suite.RunTestCasesFromFile("./test-scenarios/bridge-pty-read-tests.yaml")
}

func (suite *BridgeTestSuite) TestSymlinkCreation() {
	// GOAL: Verify symlink is created pointing to PTY slave device
	//
	// TEST SCENARIO: Create a bridge with a symlink path → symlink exists → points to a PTY slave

	bridgeCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use a temporary symlink path
	symlinkPath := fmt.Sprintf("/tmp/blim-test-symlink-%d", time.Now().UnixNano())

	var actualPTYPath string
	bridgeCallback := func(b Bridge) (error, error) {
		actualPTYPath = b.GetTTYName()

		// Verify symlink exists and points to PTY
		linkTarget, err := os.Readlink(symlinkPath)
		suite.NoError(err, "Symlink must exist")
		suite.Equal(actualPTYPath, linkTarget, "Symlink must point to PTY slave")

		return nil, nil
	}

	_, err := RunDeviceBridge(
		bridgeCtx,
		&BridgeOptions{
			BleAddress:        suite.LuaApi.GetDevice().GetAddress(),
			BleConnectTimeout: 5 * time.Second,
			Logger:            suite.Logger,
			TTYSymlinkPath:    symlinkPath,
		},
		nil,
		bridgeCallback,
	)

	suite.NoError(err, "Bridge must run successfully")

	// Verify symlink is cleaned up after bridge exits
	_, err = os.Lstat(symlinkPath)
	suite.True(os.IsNotExist(err), "Symlink must be removed after bridge exit")
}

func (suite *BridgeTestSuite) TestSymlinkCleanupOnError() {
	// GOAL: Verify symlink is removed even when the bridge fails
	//
	// TEST SCENARIO: Create a bridge with symlink → error occurs → symlink is removed

	bridgeCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	symlinkPath := fmt.Sprintf("/tmp/blim-test-symlink-error-%d", time.Now().UnixNano())

	bridgeCallback := func(b Bridge) (error, error) {
		// Verify symlink exists before error
		_, err := os.Lstat(symlinkPath)
		suite.NoError(err, "Symlink must exist during bridge execution")

		// Simulate error
		return nil, fmt.Errorf("simulated bridge error")
	}

	_, err := RunDeviceBridge(
		bridgeCtx,
		&BridgeOptions{
			BleAddress:        suite.LuaApi.GetDevice().GetAddress(),
			BleConnectTimeout: 5 * time.Second,
			Logger:            suite.Logger,
			TTYSymlinkPath:    symlinkPath,
		},
		nil,
		bridgeCallback,
	)

	suite.Error(err, "Bridge must return error")
	suite.Contains(err.Error(), "simulated bridge error", "Error must be from callback")

	// Verify symlink is cleaned up even after error
	_, err = os.Lstat(symlinkPath)
	suite.True(os.IsNotExist(err), "Symlink must be removed even after error")
}

func (suite *BridgeTestSuite) TestNoSymlinkWhenNotSpecified() {
	// GOAL: Verify bridge works normally without a symlink (backward compatibility)
	//
	// TEST SCENARIO: Create bridge without SymlinkPath → bridge runs → no symlink created

	bridgeCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var ptyPath string
	bridgeCallback := func(b Bridge) (error, error) {
		ptyPath = b.GetTTYName()
		return nil, nil
	}

	_, err := RunDeviceBridge(
		bridgeCtx,
		&BridgeOptions{
			BleAddress:        suite.LuaApi.GetDevice().GetAddress(),
			BleConnectTimeout: 5 * time.Second,
			Logger:            suite.Logger,
		},
		nil,
		bridgeCallback,
	)

	suite.NoError(err, "Bridge must run successfully without symlink")
	suite.NotEmpty(ptyPath, "PTY must be created")
}

func (suite *BridgeTestSuite) TestTTYSymlinkAlreadyExists() {
	// GOAL: Verify bridge fails gracefully when tty symlink path already exists
	//
	// TEST SCENARIO: Pre-create symlink → create bridge with the same path → error returned

	symlinkPath := fmt.Sprintf("/tmp/blim-test-symlink-exists-%d", time.Now().UnixNano())

	// Pre-create a symlink
	err := os.Symlink("/dev/null", symlinkPath)
	suite.NoError(err, "Pre-create tty symlink for test")
	defer os.Remove(symlinkPath)

	bridgeCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bridgeCallback := func(b Bridge) (error, error) {
		suite.Fail("Callback should not be reached")
		return nil, nil
	}

	_, err = RunDeviceBridge(
		bridgeCtx,
		&BridgeOptions{
			BleAddress:        suite.LuaApi.GetDevice().GetAddress(),
			BleConnectTimeout: 5 * time.Second,
			Logger:            suite.Logger,
			TTYSymlinkPath:    symlinkPath,
		},
		nil,
		bridgeCallback,
	)

	suite.Error(err, "Bridge must fail when tty symlink already exists")
	suite.Contains(err.Error(), "failed to create tty symlink", "Error must mention symlink creation")
}

// TestBridgeTestSuite runs the test suite using testify/suite
func TestBridgeTestSuite(t *testing.T) {
	suite.Run(t, new(BridgeTestSuite))
}
