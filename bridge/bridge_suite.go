package bridge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/device"
	"github.com/srg/blim/internal/lua"
)

// BridgeSuite provides test infrastructure for Bridge2 tests.
//
// Design: Embeds internal.LuaApiSuite to reuse all test infrastructure (YAML parsing,
// BLE simulation, peripheral mocks, step execution) while adding Bridge-specific features:
//   - PTY output validation (via embedded LuaApiSuite output collector)
//   - Error handler capture for stderr validation
//   - Bridge2 lifecycle management
//
// Only overrides SetupTest() to add custom error handler setup.
// All other methods (CreateSubscriptionJsonScript, NewPeripheralDataSimulator, etc.) are inherited.
type BridgeSuite struct {
	lua.LuaApiSuite
	bridge             *Bridge2     // Bridge instance under test
	capturedErrors     []string     // Errors captured by customErrorHandler
	customErrorHandler ErrorHandler // Pluggable error handler for validation
	skipOutputCapture  bool         // Skip creating test suite's output collector (Bridge creates its own)
}

// SetupTest overrides LuaApiSuite.SetupTest to add Bridge-specific error capture.
//
// Override rationale: Bridge2 needs custom error handler validation (stderr testing),
// which requires capturing errors via ErrorHandler callback before calling parent setup.
// All other test infrastructure (peripheral mocks, BLE simulation) is inherited from parent.
//
// IMPORTANT: For Bridge tests, we skip creating the test suite's output collector because
// the Bridge creates its own collector. Having two collectors on the same channel causes
// them to compete for output, resulting in lost data.
func (suite *BridgeSuite) SetupTest() {
	// Setup error capture before parent initialization
	suite.capturedErrors = []string{}
	suite.customErrorHandler = func(ts time.Time, msg string) {
		suite.T().Logf("DEBUG: Error handler called with: %q", msg)
		suite.capturedErrors = append(suite.capturedErrors, msg)
	}

	// Call parent SetupTest to configure a mock device factory
	suite.LuaApiSuite.SetupTest()
}

// RunBridgeTestCasesFromYAML parses YAML and executes Bridge test cases
func (suite *BridgeSuite) RunBridgeTestCasesFromYAML(yamlContent string) {
	suite.RunTestCasesFromYAML(yamlContent)
}

// ValidateExpectations overrides parent to add Bridge-specific validation
func (suite *BridgeSuite) ValidateExpectations(testCase lua.TestCase) {
	// Call parent for standard validation (ExpectedOutput)
	suite.LuaApiSuite.ValidateExpectations(testCase)

	// Validate Bridge-specific ExpectedStdout
	if testCase.ExpectedStdout != "" {
		stdout, err := suite.GetLuaStdout()
		suite.Require().NoError(err, "Failed to get Lua stdout")
		suite.Require().Contains(stdout, testCase.ExpectedStdout,
			"PTY stdout should contain expected output")
	}

	// Validate Bridge-specific ExpectedErrors
	if len(testCase.ExpectedErrors) > 0 {
		suite.Require().NotEmpty(suite.capturedErrors,
			"Expected errors but none were captured")

		// Check that all expected error substrings are found in captured errors
		for _, expectedErr := range testCase.ExpectedErrors {
			found := false
			for _, capturedErr := range suite.capturedErrors {
				if strings.Contains(capturedErr, expectedErr) {
					found = true
					break
				}
			}
			suite.Require().True(found,
				"Expected error substring %q not found in captured errors: %v",
				expectedErr, suite.capturedErrors)
		}
	}
}

// createAndStartBridge creates and starts a Bridge2 instance with the given script.
// Uses the same BLE device instance as the test suite for proper mocking.
func (suite *BridgeSuite) createAndStartBridge(script string, ctx context.Context) (*Bridge2, error) {
	// CRITICAL: The bridge must use the same BLEDevice instance as the test suite.
	lua.LuaApiFactory = func(address string, logger *logrus.Logger) (*lua.BLEAPI2, error) {
		// LuaApiTestSuite creates a connected LuaApi for test simplicity,
		// but bridge handles connection management by itself, so disconnect first
		err := suite.LuaApi.GetDevice().Disconnect()
		if err != nil {
			return nil, fmt.Errorf("failed to disconnect device: %w", err)
		}
		return suite.LuaApi, nil
	}

	// Reset captured errors for each test
	suite.capturedErrors = []string{}

	// Create bridge configuration
	config := Bridge2Config{
		Address:      "00:00:00:00:01",
		Script:       script,
		Logger:       suite.Logger,
		ErrorHandler: suite.customErrorHandler,
	}

	// Create and start the bridge
	bridge, err := NewBridge(suite.Logger, config)
	suite.NoError(err)
	suite.NotNil(bridge)

	// Start the bridge
	bridgeCtx := ctx
	cancel := func() {}

	if bridgeCtx == nil {
		bridgeCtx, cancel = context.WithCancel(context.Background())
	}

	defer cancel()

	// Build subscribe options from peripheral configuration
	var subscribeOptions []device.SubscribeOptions
	if suite.PeripheralBuilder != nil {
		for _, svc := range suite.PeripheralBuilder.GetServices() {
			var characteristics []string
			for _, char := range svc.Characteristics {
				characteristics = append(characteristics, char.UUID)
			}
			subscribeOptions = append(subscribeOptions, device.SubscribeOptions{
				Service:         svc.UUID,
				Characteristics: characteristics,
			})
		}
	}

	err = bridge.Start(bridgeCtx, &device.ConnectOptions{
		ConnectTimeout: 5 * time.Second,
		Services:       subscribeOptions,
	})

	if err != nil {
		// Don't try to stop a bridge that failed to start - it will give misleading errors
		return nil, err
	}

	suite.True(bridge.IsRunning())

	// Store bridge reference for cleanup
	suite.bridge = bridge

	return bridge, nil
}
