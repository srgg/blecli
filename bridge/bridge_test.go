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

//func (suite *BridgeTestSuite) TestBridgeShutdown() {
//	// GOAL: Verify bridge stops quickly after context cancellation
//	//
//	// TEST SCENARIO: Cancel context → Stop() called → completes within 1 second
//
//	// Simple test script for shutdown test
//	testScript := `
//		print("Test bridge script loaded for shutdown test")
//		-- No actual subscriptions needed for shutdown test`
//
//	bridgeCtx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//
//	// Start bridge
//	bridge, err := suite.createAndStartBridge(testScript, bridgeCtx)
//	suite.NoError(err)
//
//	time.Sleep(testSyncWait)
//
//	// Cancel context (simulates Ctrl+C)
//	cancel()
//	time.Sleep(testSyncWait)
//
//	// Stop the bridge - should complete quickly since context is already cancelled
//	stopStart := time.Now()
//	if err := bridge.Stop(); err != nil {
//		suite.Errorf(err, "Stop failed: %v", err)
//	}
//	stopDuration := time.Since(stopStart)
//
//	if stopDuration > maxShutdownDuration {
//		suite.Errorf(fmt.Errorf("stop took too long: %v", stopDuration), "stop exceeded threshold")
//	}
//
//	suite.T().Logf("Bridge stopped in %v", stopDuration)
//}

//func (suite *BridgeTestSuite) TestBridgeCtrlC() {
//	// GOAL: Verify signal handling triggers graceful shutdown
//	//
//	// TEST SCENARIO: Signal received or timeout → context cancelled → Stop() completes
//
//	t := suite.T()
//	if testing.Short() {
//		t.Skip("Skipping manual Ctrl+C test")
//	}
//
//	logger := logrus.New()
//	logger.SetLevel(logrus.DebugLevel)
//
//	// Test script for signal handling
//	testScript := `
//		print("Test bridge script loaded for Ctrl+C test")
//		-- Simple script for signal handling test`
//
//	bridgeCtx, cancel := context.WithTimeout(context.Background(), signalTestTimeout)
//	defer cancel()
//
//	// Start bridge
//	bridge, err := suite.createAndStartBridge(testScript, bridgeCtx)
//	suite.NoError(err)
//
//	// Buffer size = 1 allows signal.Notify to send one signal without blocking.
//	// Only one signal is needed to trigger shutdown, so buffer = 1 is sufficient.
//	sigChan := make(chan os.Signal, 1)
//	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
//
//	go func() {
//		<-sigChan
//		t.Log("Signal received, cancelling context")
//		cancel()
//	}()
//
//	time.Sleep(testSyncWait)
//	t.Log("Send SIGINT to test...")
//
//	<-bridgeCtx.Done()
//	if errors.Is(bridgeCtx.Err(), context.DeadlineExceeded) {
//		t.Log("Timeout - simulating Ctrl+C")
//	} else {
//		t.Log("Signal received, stopping bridge...")
//	}
//
//	stopStart := time.Now()
//	err = bridge.Stop()
//	duration := time.Since(stopStart)
//
//	if err != nil {
//		t.Errorf("Stop failed: %v", err)
//	}
//
//	t.Logf("Stop completed in %v", duration)
//}

func (suite *BridgeTestSuite) TestBridgeScenarios() {
	// GOAL: Verify bridge handles all YAML-defined test test-scenarios
	//
	// TEST SCENARIO: Load YAML test-scenarios → execute each test case → all assertions pass

	suite.RunTestCasesFromFile("./bridge-test-scenarios.yaml")
	//// Read the YAML file
	//yamlContent, err := os.ReadFile("bridge-test-test-scenarios.yaml")
	//suite.Require().NoError(err, "Failed to read bridge-test-test-scenarios.yaml")
	//
	//// Run all test cases from the YAML file using the parent's method
	//suite.RunTestCasesFromYAML(string(yamlContent))
}

func (suite *BridgeTestSuite) TestPTYWriteYAML() {
	// GOAL: Verify pty_write() function works correctly with various data types and scenarios
	//
	// TEST SCENARIO: Load YAML test cases for pty_write → execute each test → all pass

	suite.RunTestCasesFromFile("./bridge-pty-write-tests.yaml")
}

func (suite *BridgeTestSuite) TestPTYReadYAML() {
	// GOAL: Verify pty_read() function works correctly with various scenarios
	//
	// TEST SCENARIO: Load YAML test cases for pty_read → execute each test → all pass

	suite.RunTestCasesFromFile("./bridge-pty-read-tests.yaml")
}

func (suite *BridgeTestSuite) TestSymlinkCreation() {
	// GOAL: Verify symlink is created pointing to PTY slave device
	//
	// TEST SCENARIO: Create bridge with symlink path → symlink exists → points to PTY slave

	bridgeCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use temporary symlink path
	symlinkPath := fmt.Sprintf("/tmp/blim-test-symlink-%d", time.Now().UnixNano())

	var actualPTYPath string
	bridgeCallback := func(b Bridge) (error, error) {
		actualPTYPath = b.GetPTYName()

		// Verify symlink exists and points to PTY
		linkTarget, err := os.Readlink(symlinkPath)
		suite.NoError(err, "Symlink must exist")
		suite.Equal(actualPTYPath, linkTarget, "Symlink must point to PTY slave")

		return nil, nil
	}

	_, err := RunDeviceBridge(
		bridgeCtx,
		&BridgeOptions{
			Address:        suite.LuaApi.GetDevice().GetAddress(),
			ConnectTimeout: 5 * time.Second,
			Logger:         suite.Logger,
			SymlinkPath:    symlinkPath,
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
	// GOAL: Verify symlink is removed even when bridge fails
	//
	// TEST SCENARIO: Create bridge with symlink → error occurs → symlink is removed

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
			Address:        suite.LuaApi.GetDevice().GetAddress(),
			ConnectTimeout: 5 * time.Second,
			Logger:         suite.Logger,
			SymlinkPath:    symlinkPath,
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
	// GOAL: Verify bridge works normally without symlink (backward compatibility)
	//
	// TEST SCENARIO: Create bridge without SymlinkPath → bridge runs → no symlink created

	bridgeCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var ptyPath string
	bridgeCallback := func(b Bridge) (error, error) {
		ptyPath = b.GetPTYName()
		return nil, nil
	}

	_, err := RunDeviceBridge(
		bridgeCtx,
		&BridgeOptions{
			Address:        suite.LuaApi.GetDevice().GetAddress(),
			ConnectTimeout: 5 * time.Second,
			Logger:         suite.Logger,
			// SymlinkPath intentionally not set
		},
		nil,
		bridgeCallback,
	)

	suite.NoError(err, "Bridge must run successfully without symlink")
	suite.NotEmpty(ptyPath, "PTY must be created")
}

func (suite *BridgeTestSuite) TestSymlinkAlreadyExists() {
	// GOAL: Verify bridge fails gracefully when symlink path already exists
	//
	// TEST SCENARIO: Pre-create symlink → create bridge with same path → error returned

	symlinkPath := fmt.Sprintf("/tmp/blim-test-symlink-exists-%d", time.Now().UnixNano())

	// Pre-create a symlink
	err := os.Symlink("/dev/null", symlinkPath)
	suite.NoError(err, "Pre-create symlink for test")
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
			Address:        suite.LuaApi.GetDevice().GetAddress(),
			ConnectTimeout: 5 * time.Second,
			Logger:         suite.Logger,
			SymlinkPath:    symlinkPath,
		},
		nil,
		bridgeCallback,
	)

	suite.Error(err, "Bridge must fail when symlink already exists")
	suite.Contains(err.Error(), "failed to create symlink", "Error must mention symlink creation")
}

// TestBridgeTestSuite runs the test suite using testify/suite
func TestBridgeTestSuite(t *testing.T) {
	suite.Run(t, new(BridgeTestSuite))
}
