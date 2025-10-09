package bridge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
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

func (suite *BridgeTestSuite) TestBridgeShutdown() {
	// GOAL: Verify bridge stops quickly after context cancellation
	//
	// TEST SCENARIO: Cancel context → Stop() called → completes within 1 second

	// Simple test script for shutdown test
	testScript := `
		print("Test bridge script loaded for shutdown test")
		-- No actual subscriptions needed for shutdown test`

	bridgeCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start bridge
	bridge, err := suite.createAndStartBridge(testScript, bridgeCtx)
	suite.NoError(err)

	time.Sleep(testSyncWait)

	// Cancel context (simulates Ctrl+C)
	cancel()
	time.Sleep(testSyncWait)

	// Stop the bridge - should complete quickly since context is already cancelled
	stopStart := time.Now()
	if err := bridge.Stop(); err != nil {
		suite.Errorf(err, "Stop failed: %v", err)
	}
	stopDuration := time.Since(stopStart)

	if stopDuration > maxShutdownDuration {
		suite.Errorf(fmt.Errorf("stop took too long: %v", stopDuration), "stop exceeded threshold")
	}

	suite.T().Logf("Bridge stopped in %v", stopDuration)
}

func (suite *BridgeTestSuite) TestBridgeCtrlC() {
	// GOAL: Verify signal handling triggers graceful shutdown
	//
	// TEST SCENARIO: Signal received or timeout → context cancelled → Stop() completes

	t := suite.T()
	if testing.Short() {
		t.Skip("Skipping manual Ctrl+C test")
	}

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	// Test script for signal handling
	testScript := `
		print("Test bridge script loaded for Ctrl+C test")
		-- Simple script for signal handling test`

	bridgeCtx, cancel := context.WithTimeout(context.Background(), signalTestTimeout)
	defer cancel()

	// Start bridge
	bridge, err := suite.createAndStartBridge(testScript, bridgeCtx)
	suite.NoError(err)

	// Buffer size = 1 allows signal.Notify to send one signal without blocking.
	// Only one signal is needed to trigger shutdown, so buffer = 1 is sufficient.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		t.Log("Signal received, cancelling context")
		cancel()
	}()

	time.Sleep(testSyncWait)
	t.Log("Send SIGINT to test...")

	<-bridgeCtx.Done()
	if errors.Is(bridgeCtx.Err(), context.DeadlineExceeded) {
		t.Log("Timeout - simulating Ctrl+C")
	} else {
		t.Log("Signal received, stopping bridge...")
	}

	stopStart := time.Now()
	err = bridge.Stop()
	duration := time.Since(stopStart)

	if err != nil {
		t.Errorf("Stop failed: %v", err)
	}

	t.Logf("Stop completed in %v", duration)
}

func (suite *BridgeTestSuite) TestBridgeScenarios() {
	// GOAL: Verify bridge handles all YAML-defined test scenarios
	//
	// TEST SCENARIO: Load YAML scenarios → execute each test case → all assertions pass

	// Read the YAML file
	yamlContent, err := os.ReadFile("bridge-test-scenarios.yaml")
	suite.Require().NoError(err, "Failed to read bridge-test-scenarios.yaml")

	// Run all test cases from the YAML file using the parent's method
	suite.RunTestCasesFromYAML(string(yamlContent))
}

// TestBridgeTestSuite runs the test suite using testify/suite
func TestBridgeTestSuite(t *testing.T) {
	suite.Run(t, new(BridgeTestSuite))
}
