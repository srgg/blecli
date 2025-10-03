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

// BridgeTestSuite runs tests for Bridge2 using the BridgeSuite infrastructure.
type BridgeTestSuite struct {
	BridgeSuite
}

func (suite *BridgeTestSuite) TestBridgeShutdown() {
	// Simple test script for shutdown test
	testScript := `
		print("Test bridge2 script loaded for shutdown test")
		-- No actual subscriptions needed for shutdown test`

	bridgeCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start bridge
	bridge, err := suite.createAndStartBridge(testScript, bridgeCtx)
	suite.NoError(err)

	time.Sleep(100 * time.Millisecond)

	// Cancel context (simulates Ctrl+C)
	cancel()
	time.Sleep(100 * time.Millisecond)

	// Stop the bridge - should complete quickly since context is already cancelled
	stopStart := time.Now()
	if err := bridge.Stop(); err != nil {
		suite.Errorf(err, "Stop failed: %v", err)
	}
	stopDuration := time.Since(stopStart)

	if stopDuration > 1*time.Second {
		suite.Errorf(fmt.Errorf("stop took too long: %v", stopDuration), "stop exceeded threshold")
	}

	suite.T().Logf("Bridge stopped in %v", stopDuration)
}

func (suite *BridgeTestSuite) TestBridge2CtrlC() {
	t := suite.T()
	if testing.Short() {
		t.Skip("Skipping manual Ctrl+C test")
	}

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	// Test script that simulates hanging behavior
	testScript := `
		print("Test bridge2 script loaded for Ctrl+C test")
		-- Simple script for signal handling test`

	bridgeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start bridge
	bridge, err := suite.createAndStartBridge(testScript, bridgeCtx)
	suite.NoError(err)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		t.Log("Signal received, cancelling context")
		cancel()
	}()

	time.Sleep(100 * time.Millisecond)
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

// TestBridgeYAMLScenarios runs all Bridge test scenarios from the YAML file
func (suite *BridgeTestSuite) TestBridgeScenarios() {
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
