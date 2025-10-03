package bridge

import (
	"context"
	"strings"
	"time"

	"github.com/srg/blim/internal/device"
	"github.com/srg/blim/internal/lua"
)

// BridgeSuite provides test infrastructure for Bridge tests.
//
// Design: Embeds internal.LuaApiSuite to reuse all test infrastructure (YAML parsing,
// BLE simulation, peripheral mocks, step execution) while adding Bridge-specific features:
//   - PTY output validation (via embedded LuaApiSuite output collector)
//   - Error handler capture for stderr validation
//   - Bridge lifecycle management
//
// Only overrides SetupTest() to add custom error handler setup.
// All other methods (CreateSubscriptionJsonScript, NewPeripheralDataSimulator, etc.) are inherited.
type BridgeSuite struct {
	lua.LuaApiSuite
	capturedErrors     []string                // Errors captured by customErrorHandler
	customErrorHandler func(time.Time, string) // Pluggable error handler for validation
}

// SetupTest overrides LuaApiSuite.SetupTest to add Bridge-specific error capture.
//
// Override rationale: Bridge needs custom error handler validation (stderr testing),
// which requires capturing errors via error handler callback before calling parent setup.
// All other test infrastructure (peripheral mocks, BLE simulation) is inherited from parent.
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

// bridgeHandle wraps RunDeviceBridge for test lifecycle management
type bridgeHandle struct {
	ctx    context.Context
	cancel context.CancelFunc
	errCh  chan error
}

func (h *bridgeHandle) Stop() error {
	h.cancel()
	return <-h.errCh
}

// createAndStartBridge creates and starts a bridge using RunDeviceBridge with the given script.
// Uses the same BLE device instance as the test suite for proper mocking.
func (suite *BridgeSuite) createAndStartBridge(script string, ctx context.Context) (*bridgeHandle, error) {
	// Reset captured errors for each test
	suite.capturedErrors = []string{}

	// Create context if not provided
	bridgeCtx := ctx
	var cancel context.CancelFunc
	if bridgeCtx == nil {
		bridgeCtx, cancel = context.WithCancel(context.Background())
	} else {
		bridgeCtx, cancel = context.WithCancel(bridgeCtx)
	}

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

	// Custom error writer that captures errors for validation
	errorWriter := &errorCaptureWriter{suite: suite}

	// Bridge callback - executes Lua script using ExecuteDeviceScriptWithOutput
	bridgeCallback := func(b Bridge) (error, error) {
		// Execute script with output streaming
		// stdout goes to nil - test suite's luaOutputCapture will collect from channel
		// stderr goes to errorWriter for error validation
		return nil, lua.ExecuteDeviceScriptWithOutput(
			bridgeCtx,
			b.GetDevice(),
			suite.Logger,
			script,
			nil,         // no args
			nil,         // stdout - test suite collects from channel
			errorWriter, // stderr to error capture
			50*time.Millisecond,
		)
	}

	// Run bridge asynchronously for tests
	errCh := make(chan error, 1)
	go func() {
		_, err := RunDeviceBridge(
			bridgeCtx,
			&BridgeOptions{
				Address:        "00:00:00:00:01",
				ConnectTimeout: 5 * time.Second,
				Services:       subscribeOptions,
				Logger:         suite.Logger,
			},
			nil, // no progress callback for tests
			bridgeCallback,
		)
		errCh <- err
	}()

	// Wait a bit for bridge to start
	time.Sleep(50 * time.Millisecond)

	return &bridgeHandle{
		ctx:    bridgeCtx,
		cancel: cancel,
		errCh:  errCh,
	}, nil
}

// errorCaptureWriter captures stderr output for error validation
type errorCaptureWriter struct {
	suite *BridgeSuite
}

func (w *errorCaptureWriter) Write(p []byte) (n int, err error) {
	msg := string(p)
	w.suite.customErrorHandler(time.Now(), msg)
	return len(p), nil
}
