package lua

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/srg/blim/internal/device"
	"github.com/srg/blim/internal/testutils"
	orderedmap "github.com/wk8/go-ordered-map/v2"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultStepWaitDuration is the default time to wait after executing a test step before capturing output.
	// This allows asynchronous BLE callbacks and aggregated subscriptions time to process and deliver data.
	DefaultStepWaitDuration = 200 * time.Millisecond
)

// TestCase represents a complete BLE test case, including subscription configuration and steps.
type TestCase struct {
	// Name of the test case
	Name string `json:"name" yaml:"name"`

	// Script is an optional standalone Lua script to execute (mutually exclusive with Subscription)
	// Supports file:// URLs (e.g., "file://examples/inspect.lua?format=json&verbose=true")
	// URL query parameters are passed to Lua via arg[] table
	Script string `json:"script,omitempty" yaml:"script,omitempty"`

	// Peripheral is an optional explicit peripheral service configuration
	// If provided, these services will be used to configure the mock peripheral
	// If not provided, peripheral services will be auto-populated from Subscription.Services
	Peripheral []device.SubscribeOptions `json:"peripheral,omitempty" yaml:"peripheral,omitempty"`

	Subscription TestSubscriptionOptions `json:"subscription,omitempty" yaml:"subscription,omitempty"`

	// Steps is an ordered list of steps that occur in this test case
	Steps []TestStep `json:"steps" yaml:"steps"`

	// AllowMultiValue enables sending multiple values to the same characteristic (default: false)
	AllowMultiValue bool `json:"allow_multi_value,omitempty" yaml:"allow_multi_value,omitempty"`

	// ExpectErrorMessage is the expected error message substring (test expects error if non-empty)
	ExpectErrorMessage string `json:"expect_error_message,omitempty" yaml:"expect_error_message,omitempty"`

	// ExpectedJSONOutput is the expected JSON output from Lua callbacks
	ExpectedJSONOutput []map[string]interface{} `json:"expected_json_output,omitempty" yaml:"expected_json_output,omitempty"`

	// WaitAfter is the duration to wait after all steps complete before validating output (default: DefaultStepWaitDuration)
	WaitAfter time.Duration `json:"wait_after,omitempty" yaml:"wait_after,omitempty"`

	// Bridge-specific fields (optional - used only for Bridge tests):

	// ExpectedStdout is the expected stdout output (Bridge tests only - for PTY validation)
	ExpectedStdout string `json:"expected_stdout,omitempty" yaml:"expected_stdout,omitempty"`

	// ExpectedErrors is a list of expected error message substrings (Bridge tests only)
	ExpectedErrors []string `json:"expected_errors,omitempty" yaml:"expected_errors,omitempty"`
}

// TestSubscriptionOptions configures BLE characteristic subscription behavior and callbacks.
// Used in TestCase to define how characteristic notifications are monitored and processed.
type TestSubscriptionOptions struct {
	// Mode defines the subscription streaming mode (EveryUpdate, Batched, Aggregated)
	Mode string `json:"mode,omitempty" yaml:"mode,omitempty"`

	// SubscriptionMaxRate defines the maximum rate for subscription updates
	MaxRate time.Duration `json:"max_rate,omitempty" yaml:"max_rate,omitempty"`

	// WaitAfter is the default duration to wait after each test step (used if a step doesn't specify its own wait_after)
	WaitAfter time.Duration `json:"wait_after,omitempty" yaml:"wait_after,omitempty"`

	// Subscriptions define which services/characteristics to subscribe to
	Services []device.SubscribeOptions `json:"services" yaml:"services"`

	// CallbackScript is optional custom Lua code for the callback function body
	// If empty, uses the default callback that prints JSON output
	// The code has access to 'record' parameter and 'call_count' variable
	CallbackScript string `json:"callback_script,omitempty" yaml:"callback_script,omitempty"`
}

// TestStep represents one point in time where one or more services'
// / characteristics are updated.
// If At is zero or omitted, the step is applied immediately when reached.
type TestStep struct {
	// At specifies the time relative to the start of the test case.
	At time.Duration `json:"at,omitempty" yaml:"at,omitempty"`

	// Services is a list of service updates to apply at this step.
	Services []ServiceValues `json:"services" yaml:"services"`

	// WaitAfter waits this duration after step execution before capturing output (default: DefaultStepWaitDuration if zero).
	WaitAfter time.Duration `json:"wait_after,omitempty" yaml:"wait_after,omitempty"`

	// ExpectedJSONOutput validates Lua output immediately after this step (optional, validated during step execution).
	ExpectedJSONOutput []map[string]interface{} `json:"expected_json_output,omitempty" yaml:"expected_json_output,omitempty"`
}

// ServiceValues represents updates to all characteristics of a single service.
type ServiceValues struct {
	// Service is the UUID or name of the BLE service.
	Service string `json:"service" yaml:"service"`

	// Values is a list of characteristics and their data for this service.
	Values []CharacteristicValue `json:"values" yaml:"values"`
}

// CharacteristicValue represents the value of a single characteristic.
type CharacteristicValue struct {
	// Characteristic is the UUID or name of the characteristic.
	Characteristic string `json:"char" yaml:"char"`

	// Value is the raw byte value to be applied to the characteristic.
	Value []byte `json:"value" yaml:"value"`
}

// LuaErrorInfo captures detailed error information from Lua execution
type LuaErrorInfo struct {
	Message   string `json:"message"`             // Error message content
	Source    string `json:"source,omitempty"`    // Source of the error (e.g., "callback", "script")
	Line      int    `json:"line,omitempty"`      // Line number if available
	Timestamp string `json:"timestamp,omitempty"` // When the error occurred
}

// LuaSubscriptionCallbackData represents the JSON structure of Lua subscription callback output.
// Used for validation of subscription test results with support for errors, call counts, and BLE notification data.
type LuaSubscriptionCallbackData struct {
	CallCount int            `json:"call_count"`
	Errors    []LuaErrorInfo `json:"errors,omitempty"` // Collected stderr errors with details
	Record    struct {
		Values      map[string]interface{}   `json:"Values,omitempty"`
		BatchValues map[string][]interface{} `json:"BatchValues,omitempty"`
	} `json:"record"`
}

// LuaApiSuite provides test infrastructure for BLE API testing with Lua integration.
//
// It embeds MockBLEPeripheralSuite for peripheral simulation and manages a BLEAPI2 instance
// with an output collection for testing Lua script execution.
//
// # Overview
//
// Supports two types of Lua-based BLE tests:
//
// 1. **Subscription Tests**: Test BLE characteristic notifications with callbacks
// 2. **Script Tests**: Execute standalone Lua scripts with file:// URL support
//
// # Subscription Tests
//
// Subscription tests monitor BLE characteristic notifications and validate callback output.
// Supports three streaming modes: EveryUpdate, Batched, and Aggregated.
//
// Example YAML test case:
//
//	test_cases:
//	  - name: "Heart Rate Monitor Test"
//	    subscription:
//	      mode: EveryUpdate
//	      max_rate: 100ms
//	      services:
//	        - service: "180d"
//	          characteristics: ["2a37"]
//	    steps:
//	      - services:
//	          - service: "180d"
//	            values:
//	              - char: "2a37"
//	                value: [0x60]  # 96 bpm
//	    expected_json_output:
//	      - record:
//	          Values:
//	            "2a37": [0x60]
//
// # Script Tests
//
// Script tests execute standalone Lua scripts for device inspection or custom logic.
// Supports file:// URLs with query parameters passed via arg[] table.
//
// Example YAML test case:
//
//	test_cases:
//	  - name: "Inspect Device Test"
//	    script: "file://examples/inspect.lua?format=json&verbose=true"
//	    wait_after: 500ms
//	    expected_json_output:
//	      - device:
//	          id: "00:00:00:00:00:01"
//	          name: "Test Device"
//	        services:
//	          - uuid: "180d"
//	            characteristics:
//	              - uuid: "2a37"
//
// URL query parameters (format=json, verbose=true) are available in Lua via arg[] table:
//
//	local format = arg["format"]  -- "json"
//	local verbose = arg["verbose"] -- "true"
//
// # Error Handling Strategy
//
// This test infrastructure uses a deliberate error-handling strategy to distinguish between
// programmer errors and runtime failures:
//
// **Panics** are used for test suite misuse (programmer errors):
//   - Invalid test setup or configuration
//   - Contract violations in builder patterns (e.g., calling methods out of order)
//   - Missing required parameters
//   - Improper initialization
//
// Examples: WithCharacteristic called before WithService, nil builder parent.
//
// **Errors** are returned for runtime failures:
//   - External system failures (Lua execution, channel communication)
//   - Resource allocation failures
//   - Timeout conditions
//   - Unexpected data format issues
//
// Examples: Lua script execution errors, output collector failures, JSON parsing errors.
//
// This approach ensures that test suite bugs (programmer errors) are caught immediately
// during development with clear panic messages, while runtime issues are handled gracefully
// with proper error propagation.
type LuaApiSuite struct {
	testutils.MockBLEPeripheralSuite

	LuaApi           *BLEAPI2
	luaOutputCapture *LuaOutputCollector
	skipOutputSetup  bool // Allow child suites to skip output collector setup
}

// SetupTest initializes the test environment with a mock BLE peripheral and Lua API instance.
// Creates default services (1234, 180D, 180F) if no custom peripheral is configured.
// Sets up output collector to capture Lua stdout/stderr for validation.
// Called automatically before each test case by the testing framework.
func (suite *LuaApiSuite) SetupTest() {
	// -- Set up a Mock device factory only if not already configured
	if suite.PeripheralBuilder == nil || len(suite.PeripheralBuilder.GetServices()) == 0 {
		suite.WithPeripheral().
			FromJSON(`{
				"services": [
					{
						"uuid": "1234",
						"characteristics": [
							{
								"uuid": "5678",
								"properties": "read,notify",
								"value": [0]
							}
						]
					},
					{
						"uuid": "180D",
						"characteristics": [
							{ "uuid": "2A37", "properties": "read,notify", "value": [0] },
							{ "uuid": "2A38", "properties": "read,notify", "value": [0] }
						]
					},
					{
						"uuid": "180F",
						"characteristics": [
							{ "uuid": "2A19", "properties": "read,notify", "value": [0] }
						]
					}
				]
			}`).
			Build()
	}

	suite.MockBLEPeripheralSuite.SetupTest()

	// -- Create lua api
	suite.LuaApi = suite.createLuaApi()

	// Create and start a Lua output collector with proper error handling
	// Skip if child suite (e.g., BridgeSuite) manages its own output collector
	if !suite.skipOutputSetup {
		if err := suite.setupOutputCollector(); err != nil {
			suite.T().Fatalf("Failed to setup output collector: %v", err)
		}
	}
}

func (suite *LuaApiSuite) createLuaApi() *BLEAPI2 {
	// Create a BLE Device with mocked ble.Device
	dev := device.NewDeviceWithAddress("00:00:00:00:00:01", suite.Logger)

	// Set up mock connection with test services and characteristics
	// Use proper context with a timeout for the connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Define the services and characteristics needed for tests
	// Get services from the configured peripheral builder
	var subscribeOptions []device.SubscribeOptions
	if suite.PeripheralBuilder != nil && len(suite.PeripheralBuilder.GetServices()) > 0 {
		// Use the services configured in the peripheral builder
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
	} else {
		// Fallback to default services if no peripheral configured
		subscribeOptions = []device.SubscribeOptions{
			{
				Service:         "1234",
				Characteristics: []string{"5678"},
			},
			{
				Service:         "180d",
				Characteristics: []string{"2a37", "2a38"},
			},
			{
				Service:         "180f",
				Characteristics: []string{"2a19"},
			},
		}
	}

	opts := &device.ConnectOptions{
		ConnectTimeout: 5 * time.Second,
		Services:       subscribeOptions,
	}

	// Connect with mocked device factory (should succeed since we set up mocks in SetupSuite)
	err := dev.Connect(ctx, opts)
	suite.NoError(err, "Mock connection should succeed with mocked device factory")
	return NewBLEAPI2(dev, suite.Logger)
}

// setupOutputCollector creates and starts the Lua output collector with proper error handling
func (suite *LuaApiSuite) setupOutputCollector() error {
	lc, err := NewLuaOutputCollector(suite.LuaApi.OutputChannel(), 100, nil)
	if err != nil {
		return fmt.Errorf("creating lua output collector: %w", err)
	}

	if err := lc.Start(); err != nil {
		// Attempt cleanup on start failure - if stop also fails, log it but return the original error
		if stopErr := lc.Stop(); stopErr != nil {
			suite.T().Logf("Warning: failed to stop collector after start failure: %v", stopErr)
		}
		return fmt.Errorf("starting lua output collector: %w", err)
	}

	suite.luaOutputCapture = lc
	return nil
}

// TearDownTest cleans up test resources in proper order: output collector, Lua API, then peripheral.
// Called automatically after each test case by the testing framework.
// Logs warnings for cleanup errors but does not fail the test.
func (suite *LuaApiSuite) TearDownTest() {
	var errors []error

	// Stop Lua output collector first
	if suite.luaOutputCapture != nil {
		if err := suite.luaOutputCapture.Stop(); err != nil {
			errors = append(errors, fmt.Errorf("stopping lua output collector: %w", err))
		}
		suite.luaOutputCapture = nil
	}

	// Close lua API
	if suite.LuaApi != nil {
		suite.LuaApi.Close()
		suite.LuaApi = nil
	}

	// Call parent teardown
	suite.MockBLEPeripheralSuite.TearDownTest()

	// Report any cleanup errors (but don't fail the test)
	if len(errors) > 0 {
		for _, err := range errors {
			suite.T().Logf("Cleanup warning: %v", err)
		}
	}
}

// SetupSubTest reinitializes test resources for subtests.
// Cleans up existing output collector and Lua API, then creates fresh instances.
// Enables running multiple test cases in the same test function using suite.Run().
// Called automatically before each subtest by the testing framework.
func (suite *LuaApiSuite) SetupSubTest() {
	// Clean up existing resources in proper order
	if suite.luaOutputCapture != nil {
		if err := suite.luaOutputCapture.Stop(); err != nil {
			suite.T().Fatalf("Failed to stop lua output collector during subtest setup: %v", err)
		}
		suite.luaOutputCapture = nil
	}

	if suite.LuaApi != nil {
		suite.LuaApi.Close()
		suite.LuaApi = nil
	}

	// Create a new resources
	suite.LuaApi = suite.createLuaApi()

	// Setup output collector with fail-fast error handling
	if err := suite.setupOutputCollector(); err != nil {
		suite.T().Fatalf("Failed to setup output collector in subtest: %v", err)
	}
}

// CreateSubscriptionJsonScript generates a Lua script that subscribes to BLE characteristics and processes notifications.
// Returns a complete Lua script string with JSON callback logic for use in tests.
func (suite *LuaApiSuite) CreateSubscriptionJsonScript(pattern string, maxRate time.Duration, callbackScript string, subscription ...device.SubscribeOptions) string {
	// Validate inputs
	if maxRate < 0 {
		panic(fmt.Sprintf("CreateSubscriptionJsonScript: maxRate cannot be negative (got %v)", maxRate))
	}

	var services strings.Builder
	services.WriteString("{")
	for i, sub := range subscription {
		// Validate each subscription
		if sub.Service == "" {
			panic(fmt.Sprintf("CreateSubscriptionJsonScript: subscription[%d] has empty service UUID", i))
		}
		if len(sub.Characteristics) == 0 {
			panic(fmt.Sprintf("CreateSubscriptionJsonScript: subscription[%d] (service=%q) has no characteristics", i, sub.Service))
		}
		for j, char := range sub.Characteristics {
			if char == "" {
				panic(fmt.Sprintf("CreateSubscriptionJsonScript: subscription[%d] (service=%q) has empty characteristic at index %d", i, sub.Service, j))
			}
		}

		if i > 0 {
			services.WriteString(",")
		}
		if _, err := fmt.Fprintf(&services, `{service="%s",chars={"%s"}}`, sub.Service, strings.Join(sub.Characteristics, `","`)); err != nil {
			panic(fmt.Sprintf("CreateSubscriptionJsonScript: failed to build services string for subscription[%d] (service=%q, chars=%v): %v", i, sub.Service, sub.Characteristics, err))
		}
	}
	services.WriteString("}")

	// Use custom callback if provided, otherwise use default JSON output callback
	var callbackBody string
	if callbackScript != "" {
		callbackBody = callbackScript
	} else {
		callbackBody = `
				call_count = call_count + 1

				local output = {
					call_count = call_count,
					record = record
				}

				-- print(json.encode{call_count = call_count, record = record})
				print(json.encode(output))`
	}

	return fmt.Sprintf(`
		local json = require("json")
		call_count = 0

		ble.subscribe{
			services = %s,
			Mode = "%s",
			MaxRate = %d,
			Callback = function(record)%s
			end
		}`, services.String(), pattern, maxRate.Milliseconds(), callbackBody)
}

// GetLuaStdout retrieves all stdout output from the Lua output collector.
// Consumes and returns only stdout records, filtering out stderr.
// Returns an error if output collection failed.
func (suite *LuaApiSuite) GetLuaStdout() (string, error) {
	output, err := ConsumeRecords(suite.luaOutputCapture, func(record *LuaOutputRecord) (string, error) {
		if record == nil {
			return "", nil
		}
		if record.Source == "stdout" {
			return record.Content, nil
		}
		return "", nil
	})
	if err != nil {
		return "", fmt.Errorf("error consuming lua stdout: %v", err)
	}
	return output, nil
}

// LuaOutputAsJSON parses Lua output as JSON subscription callback data.
// Separates stdout (JSON) from stderr (errors) and returns structured callback data.
// Supports both single JSON objects and JSONL (JSON Lines) format for multiple callbacks.
// Collects stderr errors and attaches them to the first callback record.
// Returns an error if JSON parsing fails or no valid data is found.
//
// Format detection:
//   - Tries parsing as single JSON object first
//   - Falls back to JSONL if single JSON parsing fails
//   - Returns error if both formats fail
func (suite *LuaApiSuite) LuaOutputAsJSON() ([]LuaSubscriptionCallbackData, error) {
	// Create a custom consumer that separates stdout (JSON) and stderr (errors)
	var stdoutBuilder strings.Builder
	var collectedErrors []LuaErrorInfo

	consumer := func(record *LuaOutputRecord) (string, error) {
		if record == nil {
			// No more records - return accumulated stdout
			return stdoutBuilder.String(), nil
		}

		// Separate stdout and stderr records
		if record.Source == "stderr" {
			// Collect error information
			collectedErrors = append(collectedErrors, LuaErrorInfo{
				Message:   record.Content,
				Source:    "callback",
				Timestamp: record.Timestamp.Format(time.RFC3339),
			})
		} else {
			// Accumulate stdout (JSON output)
			stdoutBuilder.WriteString(record.Content)
		}

		return "", nil // Continue processing
	}

	// Collect all outputs using custom consumer
	output, err := ConsumeRecords(suite.luaOutputCapture, consumer)
	if err != nil {
		return nil, fmt.Errorf("error consuming lua output: %v", err)
	}

	// DEBUG: Log the raw Lua output for troubleshooting JSON parsing issues
	suite.T().Logf("DEBUG: Raw Lua stdout (%d chars): %q", len(output), output)
	if len(collectedErrors) > 0 {
		suite.T().Logf("DEBUG: Collected %d stderr errors", len(collectedErrors))
	}

	suite.Equal(int64(0), suite.luaOutputCapture.GetMetrics().ErrorsOccurred, "error occurred on lua output")

	// Bulletproof detection: try single JSON first, fallback to JSONL
	output = strings.TrimSpace(output)

	// Try parsing as a single JSON object first
	var singleData LuaSubscriptionCallbackData
	if err := json.Unmarshal([]byte(output), &singleData); err == nil {
		// Successfully parsed as a single JSON - return as an array with one element
		singleData.Errors = collectedErrors
		return []LuaSubscriptionCallbackData{singleData}, nil
	}

	// Failed as a single JSON, try JSONL format
	lines := strings.Split(output, "\n")
	var arrayData []LuaSubscriptionCallbackData

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var data LuaSubscriptionCallbackData
		if err := json.Unmarshal([]byte(line), &data); err != nil {
			return nil, fmt.Errorf("error unmarshalling as both JSON and JSONL - line %q: %v", line, err)
		}

		arrayData = append(arrayData, data)
	}

	if len(arrayData) == 0 {
		// If no JSON output, but we have errors, return a single record with errors
		if len(collectedErrors) > 0 {
			return []LuaSubscriptionCallbackData{{
				CallCount: 0,
				Errors:    collectedErrors,
			}}, nil
		}
		return nil, fmt.Errorf("no valid JSON data found in output")
	}

	// Add collected errors to the first callback record
	if len(arrayData) > 0 && len(collectedErrors) > 0 {
		arrayData[0].Errors = collectedErrors
	}

	return arrayData, nil
}

// GetCharacteristicValue retrieves the current value of a BLE characteristic.
// Returns the raw byte value from the mock peripheral's connection.
// Validates that service and characteristic UUIDs are non-empty.
// Returns an error if connection is unavailable or characteristic not found.
func (suite *LuaApiSuite) GetCharacteristicValue(service string, characteristic string) ([]byte, error) {
	// Validate inputs
	if service == "" {
		return nil, fmt.Errorf("GetCharacteristicValue: service UUID cannot be empty")
	}
	if characteristic == "" {
		return nil, fmt.Errorf("GetCharacteristicValue: characteristic UUID cannot be empty")
	}

	conn := suite.LuaApi.GetDevice().GetConnection()

	if conn == nil {
		return nil, fmt.Errorf("connection should be available")
	}

	c, err := conn.GetCharacteristic(service, characteristic)
	if err != nil {
		return nil, fmt.Errorf("should be able to get characteristic \"%s:%s\": %w", service, characteristic, err)
	}

	return c.GetValue(), nil
}

// ensurePeripheralService ensures a service and its characteristics exist in the peripheral builder
func (suite *LuaApiSuite) ensurePeripheralService(serviceUUID string, characteristicUUIDs []string) {
	// Check if service already exists in the peripheral builder
	if suite.PeripheralBuilder == nil {
		return
	}

	services := suite.PeripheralBuilder.GetServices()
	serviceExists := false
	for _, svc := range services {
		if strings.EqualFold(svc.UUID, serviceUUID) {
			serviceExists = true
			break
		}
	}

	// Add missing service with all its characteristics
	if !serviceExists {
		svcBuilder := suite.PeripheralBuilder.WithService(serviceUUID)
		for _, charUUID := range characteristicUUIDs {
			svcBuilder.WithCharacteristic(charUUID, "read,notify", []byte{0})
		}
	}
}

// RunTestCasesFromYAML parses YAML test case definitions and executes them.
// Automatically dedents the YAML content to support inline test definitions.
// Expects a "test_cases" array at the root level.
// Each test case runs as a separate subtest with full isolation.
func (suite *LuaApiSuite) RunTestCasesFromYAML(yamlContent string) {
	// Dedent the YAML content
	yamlContent = dedent(yamlContent)

	var scenario struct {
		TestCases []TestCase `yaml:"test_cases"`
	}
	err := yaml.Unmarshal([]byte(yamlContent), &scenario)
	suite.Require().NoError(err, "Failed to parse YAML test cases")

	suite.RunTestCases(scenario.TestCases...)
}

func dedent(s string) string {
	const tabWidth = 4
	lines := strings.Split(s, "\n")

	// find min indent
	minIndent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := 0
		for _, ch := range line {
			if ch == ' ' {
				indent++
			} else if ch == '\t' {
				indent += tabWidth
			} else {
				break
			}
		}
		if minIndent == -1 || indent < minIndent {
			minIndent = indent
		}
	}
	if minIndent <= 0 {
		return strings.ReplaceAll(s, "\t", strings.Repeat(" ", tabWidth))
	}

	// strip min indent, normalize tabs → spaces
	var out []string
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			out = append(out, "")
			continue
		}
		indent, j := 0, 0
		for j < len(line) && indent < minIndent {
			if line[j] == ' ' {
				indent++
			} else if line[j] == '\t' {
				indent += tabWidth
			} else {
				break
			}
			j++
		}
		out = append(out, strings.Repeat(" ", indent-minIndent)+strings.ReplaceAll(line[j:], "\t", strings.Repeat(" ", tabWidth)))
	}
	return strings.Join(out, "\n")
}

// parseFileURL parses a file:// URL and extracts the path and query parameters.
// Returns the file path and a map of query parameters.
// Example: "file://examples/inspect.lua?format=json&verbose=true" → ("examples/inspect.lua", {"format": "json", "verbose": "true"})
func parseFileURL(fileURL string) (string, map[string]string, error) {
	// Parse the URL
	u, err := url.Parse(fileURL)
	if err != nil {
		return "", nil, fmt.Errorf("invalid file URL: %w", err)
	}

	// Extract the path (everything after file://)
	filePath := strings.TrimPrefix(fileURL, "file://")
	if idx := strings.Index(filePath, "?"); idx != -1 {
		filePath = filePath[:idx]
	}

	// Extract query parameters
	params := make(map[string]string)
	for key, values := range u.Query() {
		if len(values) > 0 {
			params[key] = values[0] // Take first value if multiple
		}
	}

	return filePath, params, nil
}

// RunTestCases executes one or more test cases with full isolation.
// Configures the peripheral based on test case requirements before setup.
// Runs each test case as a separate subtest using suite.Run().
// Handles both subscription-based tests and standalone script tests.
func (suite *LuaApiSuite) RunTestCases(testCases ...TestCase) {
	for _, tc := range testCases {
		// Capture test case for closure
		testCase := tc

		// Configure peripheral BEFORE SetupSubTest runs (which calls createLuaApi)
		if len(testCase.Peripheral) > 0 {
			// Explicit peripheral configuration provided - use it
			for _, svcOpts := range testCase.Peripheral {
				suite.ensurePeripheralService(svcOpts.Service, svcOpts.Characteristics)
			}
		} else if len(testCase.Subscription.Services) > 0 && testCase.ExpectErrorMessage == "" {
			// Auto-populate peripheral services from subscription configuration
			// Skip for error test cases (ExpectErrorMessage != "") as they may have invalid UUIDs
			for _, svcOpts := range testCase.Subscription.Services {
				suite.ensurePeripheralService(svcOpts.Service, svcOpts.Characteristics)
			}
		}

		suite.Run(testCase.Name, func() {
			suite.runTestCase(testCase)
		})
	}
}

// runTestCase executes a single test case
func (suite *LuaApiSuite) runTestCase(testCase TestCase) {
	var scriptErr error

	// Handle standalone Script field (mutually exclusive with Subscription)
	if testCase.Script != "" {
		var scriptContent string
		var params map[string]string

		// Check if Script is a file:// URL
		if strings.HasPrefix(testCase.Script, "file://") {
			// Parse file URL to extract path and query parameters
			filePath, urlParams, err := parseFileURL(testCase.Script)
			if err != nil {
				scriptErr = fmt.Errorf("failed to parse script URL %q: %w", testCase.Script, err)
			} else {
				params = urlParams
				// Load script content
				content, err := testutils.LoadScript(filePath)
				if err != nil {
					scriptErr = fmt.Errorf("failed to load script file %q: %w", filePath, err)
				} else {
					scriptContent = content
				}
			}
		} else {
			// Inline script
			scriptContent = testCase.Script
		}

		// Execute a script with arg[] table populated from URL params
		if scriptErr == nil {
			// Build arg[] table initialization
			var argTable strings.Builder
			argTable.WriteString("arg = {}\n")
			for key, value := range params {
				// Escape the value for Lua string
				escapedValue := strings.ReplaceAll(value, "\\", "\\\\")
				escapedValue = strings.ReplaceAll(escapedValue, "\"", "\\\"")
				fmt.Fprintf(&argTable, "arg[%q] = %q\n", key, escapedValue)
			}

			// Combine arg[] initialization with script content
			fullScript := argTable.String() + scriptContent

			if err := suite.LuaApi.LoadScript(fullScript, testCase.Name); err != nil {
				scriptErr = err
			} else if err := suite.LuaApi.ExecuteScript(context.Background(), ""); err != nil {
				scriptErr = err
			}
		}

		// Check for errors - fail test cleanly and return
		if scriptErr != nil {
			if testCase.ExpectErrorMessage != "" {
				suite.Require().Contains(scriptErr.Error(), testCase.ExpectErrorMessage, "Error message should contain expected text")
			} else {
				suite.Require().NoError(scriptErr, "Script execution failed")
			}
			return
		}
	}

	// Handle subscription-based tests
	subscription := testCase.Subscription
	if len(subscription.Services) > 0 || testCase.ExpectErrorMessage != "" {
		mode := subscription.Mode
		if mode == "" {
			mode = "EveryUpdate"
		}

		// Check if CallbackScript is a file reference (starts with "file://")
		callbackScript := subscription.CallbackScript
		if strings.HasPrefix(callbackScript, "file://") {
			// Extract file path from file://path/to/file.lua
			filePath := strings.TrimPrefix(callbackScript, "file://")

			// Read the file content using testutils.LoadScript (finds project root)
			fileContent, err := testutils.LoadScript(filePath)
			if err != nil {
				scriptErr = fmt.Errorf("failed to load script file %q: %w", filePath, err)
			} else {
				callbackScript = fileContent
			}
		}

		if scriptErr == nil {
			script := suite.CreateSubscriptionJsonScript(mode, subscription.MaxRate, callbackScript, subscription.Services...)
			if err := suite.LuaApi.LoadScript(script, testCase.Name); err != nil {
				scriptErr = err
			} else if err := suite.LuaApi.ExecuteScript(context.Background(), ""); err != nil {
				scriptErr = err
			}
		}
	}

	// Check for the expected error
	if testCase.ExpectErrorMessage != "" {
		suite.Require().Error(scriptErr, "Expected an error but got none")
		suite.Require().Contains(scriptErr.Error(), testCase.ExpectErrorMessage, "Error message should contain expected text")
		return // Don't execute steps if we expected an error
	}

	// If we didn't expect an error, require no error
	suite.Require().NoError(scriptErr)

	// Sort steps by time to ensure correct execution order
	steps := make([]TestStep, len(testCase.Steps))
	copy(steps, testCase.Steps)
	sort.Slice(steps, func(i, j int) bool {
		return steps[i].At < steps[j].At
	})

	// Execute test steps with timing
	startTime := time.Now()
	for _, step := range steps {
		if step.At > 0 {
			elapsed := time.Since(startTime)
			if step.At > elapsed {
				time.Sleep(step.At - elapsed)
			}
		}

		// Simulate data for this step
		simulator := suite.NewPeripheralDataSimulator()
		if testCase.AllowMultiValue {
			simulator.AllowMultiValue()
		}
		for _, svc := range step.Services {
			svcBuilder := simulator.WithService(svc.Service)
			for _, charVal := range svc.Values {
				svcBuilder.WithCharacteristic(charVal.Characteristic, charVal.Value)
			}
		}

		_, err := simulator.Simulate(true)
		suite.Require().NoError(err)

		// Wait after step execution
		if waitAfter := step.WaitAfter; waitAfter > 0 {
			time.Sleep(waitAfter)
		} else if waitAfter := testCase.WaitAfter; waitAfter > 0 {
			time.Sleep(waitAfter)
		} else {
			time.Sleep(DefaultStepWaitDuration)
		}

		// Validate step output if expected output is provided
		if len(step.ExpectedJSONOutput) > 0 {
			suite.validateJSONOutput(step.ExpectedJSONOutput, true)
		}
	}

	// Validate expectations at test case level
	suite.ValidateExpectations(testCase)
}

// ValidateExpectations validates test case output against expected results.
// Waits for output processing to complete before validation.
// Validates both JSON output (expected_json_output) and plain text output (expected_stdout).
// Extension point for child suites (e.g., BridgeSuite) to add custom validation logic.
func (suite *LuaApiSuite) ValidateExpectations(testCase TestCase) {
	// Wait if needed for output to be processed
	if len(testCase.ExpectedJSONOutput) > 0 || testCase.ExpectedStdout != "" {
		waitDuration := testCase.WaitAfter
		if waitDuration == 0 {
			waitDuration = DefaultStepWaitDuration
		}
		time.Sleep(waitDuration)
	}

	// Validate ExpectedJSONOutput
	if len(testCase.ExpectedJSONOutput) > 0 {
		// Scripts use isSubscription=false, subscriptions use isSubscription=true
		suite.validateJSONOutput(testCase.ExpectedJSONOutput, testCase.Script == "")
	}

	// Validate ExpectedStdout (plain text output from standalone scripts)
	if testCase.ExpectedStdout != "" {
		actualOutput, err := suite.luaOutputCapture.ConsumePlainText()
		suite.Require().NoError(err, "Should be able to consume plain text output")
		suite.Require().Equal(testCase.ExpectedStdout, actualOutput, "Stdout output should match expected")
	}
}

// validateJSONOutput is the unified validation logic for both subscription callbacks and script output
func (suite *LuaApiSuite) validateJSONOutput(expectedOutput []map[string]interface{}, isSubscription bool) {
	var actual interface{}

	if isSubscription {
		// Get subscription callback JSON data
		jsonData, err := suite.LuaOutputAsJSON()
		suite.Require().NoError(err, "Failed to get Lua output")
		actual = jsonData
	} else {
		// Get script stdout and parse as JSON
		output, err := suite.luaOutputCapture.ConsumePlainText()
		suite.Require().NoError(err, "Failed to get Lua stdout")

		var actualData map[string]interface{}
		err = json.Unmarshal([]byte(strings.TrimSpace(output)), &actualData)
		suite.Require().NoError(err, "Failed to parse JSON output from script")

		actual = []map[string]interface{}{actualData}
	}

	// Normalize the expected output (convert byte arrays to strings to match Lua's JSON encoding)
	normalizedExpected := suite.normalizeExpectedOutput(expectedOutput)

	// Add default call_count only for subscription callbacks
	if isSubscription {
		for i, item := range normalizedExpected {
			if _, hasCallCount := item["call_count"]; !hasCallCount {
				item["call_count"] = i + 1
			}
		}
	}

	// Convert string values to hex representation for better diff visibility
	actualWithHex := suite.convertStringsToHex(actual)
	expectedWithHex := suite.convertStringsToHex(normalizedExpected)

	// Wrap arrays in objects since jsonassert doesn't support root-level arrays
	actualWrapped := map[string]interface{}{"array": actualWithHex}
	expectedWrapped := map[string]interface{}{"array": expectedWithHex}

	// Create asserter with optional ignored fields (only for subscriptions)
	ja := testutils.NewJSONAsserter(suite.T())
	if isSubscription {
		ja = ja.WithOptions(testutils.WithIgnoredFields("TsUs", "Seq", "Flags", "timestamp"))
	}

	actualJSON, _ := json.Marshal(actualWrapped)
	expectedJSON, _ := json.Marshal(expectedWrapped)

	ja.Assert(string(actualJSON), string(expectedJSON))
}

// convertStringsToHex converts binary string values to hex representation for better diff readability
func (suite *LuaApiSuite) convertStringsToHex(data interface{}) interface{} {
	switch v := data.(type) {
	case []LuaSubscriptionCallbackData:
		result := make([]map[string]interface{}, len(v))
		for i, item := range v {
			recordMap := make(map[string]interface{})
			if item.Record.Values != nil {
				recordMap["Values"] = suite.convertValuesMapToHex(item.Record.Values)
			}
			if item.Record.BatchValues != nil {
				recordMap["BatchValues"] = suite.convertBatchValuesMapToHex(item.Record.BatchValues)
			}
			itemMap := map[string]interface{}{
				"call_count": item.CallCount,
				"record":     recordMap,
			}
			// Include errors if present
			if len(item.Errors) > 0 {
				itemMap["errors"] = item.Errors
			}
			result[i] = itemMap
		}
		return result
	case []map[string]interface{}:
		result := make([]map[string]interface{}, len(v))
		for i, item := range v {
			result[i] = suite.convertMapToHex(item)
		}
		return result
	default:
		return data
	}
}

// convertMapToHex recursively converts string values to hex in a map
func (suite *LuaApiSuite) convertMapToHex(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range m {
		switch val := v.(type) {
		case string:
			result[k] = suite.stringToHex(val)
		case map[string]interface{}:
			if k == "Values" {
				result[k] = suite.convertValuesMapToHex(val)
			} else if k == "BatchValues" {
				// Convert map[string]interface{} to map[string][]interface{} for BatchValues
				batchValues := make(map[string][]interface{})
				for charUUID, arrayVal := range val {
					if arr, ok := arrayVal.([]interface{}); ok {
						batchValues[charUUID] = arr
					}
				}
				result[k] = suite.convertBatchValuesMapToHex(batchValues)
			} else {
				result[k] = suite.convertMapToHex(val)
			}
		default:
			result[k] = v
		}
	}
	return result
}

// convertValuesMapToHex converts characteristic values to hex representation
func (suite *LuaApiSuite) convertValuesMapToHex(values map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range values {
		if str, ok := v.(string); ok {
			result[k] = suite.stringToHex(str)
		} else {
			result[k] = v
		}
	}
	return result
}

// convertBatchValuesMapToHex converts batched characteristic values to a hex representation
func (suite *LuaApiSuite) convertBatchValuesMapToHex(batchValues map[string][]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, values := range batchValues {
		hexValues := make([]interface{}, len(values))
		for i, v := range values {
			if str, ok := v.(string); ok {
				hexValues[i] = suite.stringToHex(str)
			} else {
				hexValues[i] = v
			}
		}
		result[k] = hexValues
	}
	return result
}

// stringToHex converts a string to a hex representation
func (suite *LuaApiSuite) stringToHex(s string) string {
	if len(s) == 0 {
		return s
	}

	// Check if a string contains non-printable characters
	hasNonPrintable := false
	for _, b := range []byte(s) {
		if b < 32 || b > 126 {
			hasNonPrintable = true
			break
		}
	}

	if !hasNonPrintable {
		return s // Keep printable strings as-is
	}

	// Convert to hex representation
	var hexStr strings.Builder
	bytes := []byte(s)
	hexStr.Grow(len(bytes)*3 - 1) // Pre-allocate: 2 hex chars + 1 space per byte, minus 1 for no trailing space
	for i, b := range bytes {
		if i > 0 {
			hexStr.WriteByte(' ')
		}
		fmt.Fprintf(&hexStr, "%02x", b)
	}
	return hexStr.String()
}

// normalizeExpectedOutput converts byte arrays in expected output to strings
// to match how Lua's JSON encoder represents binary data (modifies maps in-place)
func (suite *LuaApiSuite) normalizeExpectedOutput(expected []map[string]interface{}) []map[string]interface{} {
	for _, item := range expected {
		suite.normalizeMapInPlace(item)
	}
	return expected
}

// normalizeMapInPlace recursively converts byte arrays to strings in a map (modifies in-place)
func (suite *LuaApiSuite) normalizeMapInPlace(m map[string]interface{}) {
	for k, v := range m {
		m[k] = suite.normalizeValue(v)
	}
}

// normalizeValue converts a value, handling byte arrays specially
func (suite *LuaApiSuite) normalizeValue(v interface{}) interface{} {
	switch val := v.(type) {
	case []interface{}:
		// Check if this is a byte array (all elements are numbers 0-255)
		if isByteArray(val) {
			bytes := make([]byte, len(val))
			for i, item := range val {
				if num, ok := toNumber(item); ok {
					bytes[i] = byte(num)
				}
			}
			return string(bytes)
		}
		// Otherwise normalize each element
		result := make([]interface{}, len(val))
		for i, item := range val {
			result[i] = suite.normalizeValue(item)
		}
		return result
	case map[string]interface{}:
		// Normalize nested maps in-place
		suite.normalizeMapInPlace(val)
		return val
	default:
		return v
	}
}

// isByteArray checks if an array contains only numbers 0-255
func isByteArray(arr []interface{}) bool {
	if len(arr) == 0 {
		return false
	}
	for _, item := range arr {
		if num, ok := toNumber(item); !ok || num < 0 || num > 255 {
			return false
		}
	}
	return true
}

// toNumber converts various numeric types to int
func toNumber(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	default:
		return 0, false
	}
}

// PeripheralDataSimulatorBuilder builds multiservice peripheral data simulations.
// It provides a fluent API for configuring multiple BLE services and their characteristics
// with data, then simulating all notifications at once.
//
// The builder supports configuring multiple services, each with multiple characteristics,
// and executes all configured data simulations when Simulate() is called.
//
// # Multi-Value Mode
//
// By default, calling WithCharacteristic() multiple times with the same characteristic UUID
// will overwrite the previous value (only the last value is kept). To send multiple notifications
// to the same characteristic, enable multi-value mode using AllowMultiValue():
//
//	suite.NewPeripheralDataSimulator().
//	    AllowMultiValue().  // Enable multiple values for the same characteristic
//	    WithService("1234").
//	        WithCharacteristic("5678", []byte{0x01, 0x02}).  // First notification
//	        WithCharacteristic("5678", []byte{0x03, 0x04}).  // Second notification
//	        WithCharacteristic("5678", []byte{0x05, 0x06}).  // Third notification
//	    Simulate()
//
// # Round-Robin Simulation
//
// When Simulate() is called, notifications are sent in a round-robin fashion across all
// characteristics, index-by-index. This ensures that notifications are interleaved naturally
// as they would be in real BLE communication:
//
//	suite.NewPeripheralDataSimulator().
//	    AllowMultiValue().
//	    WithService("180D").
//	        WithCharacteristic("2A37", []byte{60}).   // Heart rate - index 0
//	        WithCharacteristic("2A37", []byte{62}).   // Heart rate - index 1
//	        WithCharacteristic("2A38", []byte{1}).    // Control point - index 0
//	    Simulate()
//	// Sends: 2A37[60] → 2A38[1] → 2A37[62]
//
// If a characteristic has fewer values than others, it's skipped in subsequent rounds.
//
// # Basic Usage
//
// Single value per characteristic (default behavior):
//
//	suite.NewPeripheralDataSimulator().
//	    WithService("180D").
//	        WithCharacteristic("2A37", []byte{60}).  // Heart rate
//	        WithCharacteristic("2A38", []byte{1}).   // Control point
//	    Simulate()
//
// Multiple services with different characteristics:
//
//	suite.NewPeripheralDataSimulator().
//	    WithService("1234").
//	        WithCharacteristic("5678", []byte("XYZ")).
//	    WithService("180D").
//	        WithCharacteristic("2A37", []byte{60}).
//	        WithCharacteristic("2A38", []byte{1}).
//	    WithService("180F").
//	        WithCharacteristic("2A19", []byte{85}).  // Battery level
//	    Simulate()
//
// Alternative: Call Simulate() from service level (simulates all configured services):
//
//	suite.NewPeripheralDataSimulator().
//	    WithService("180D").
//	        WithCharacteristic("2A37", []byte{62}).
//	        WithCharacteristic("2A38", []byte{2}).
//	        Simulate() // Executes all services, not just the current one
type PeripheralDataSimulatorBuilder struct {
	suite           *LuaApiSuite
	allowMultiValue bool                                                                     // Allow multiple values for the same characteristic
	serviceData     *orderedmap.OrderedMap[string, *orderedmap.OrderedMap[string, [][]byte]] // Maintains insertion order: serviceUUID -> (charUUID -> []data)
	logf            func(format string, args ...any)
}

// ServiceDataSimulatorBuilder builds data simulation for a specific service.
// Returned by PeripheralDataSimulatorBuilder.WithService() to configure characteristics
// for a single service. Use Build() to return to the parent builder, or call WithService()
// again to add another service.
//
// See PeripheralDataSimulatorBuilder for detailed usage examples and multi-value behavior.
type ServiceDataSimulatorBuilder struct {
	parent      *PeripheralDataSimulatorBuilder
	serviceUUID string
}

// NewPeripheralDataSimulator creates a new builder for multi-service data simulation
func (suite *LuaApiSuite) NewPeripheralDataSimulator() *PeripheralDataSimulatorBuilder {
	return &PeripheralDataSimulatorBuilder{
		suite:           suite,
		serviceData:     orderedmap.New[string, *orderedmap.OrderedMap[string, [][]byte]](),
		allowMultiValue: false,
		logf:            suite.T().Logf,
	}
}

// AllowMultiValue enables sending multiple values to the same characteristic
func (b *PeripheralDataSimulatorBuilder) AllowMultiValue() *PeripheralDataSimulatorBuilder {
	b.allowMultiValue = true
	return b
}

// WithService adds a service to the simulation and returns a service-specific builder
func (b *PeripheralDataSimulatorBuilder) WithService(serviceUUID string) *ServiceDataSimulatorBuilder {
	_, exists := b.serviceData.Get(serviceUUID)
	if !exists {
		b.serviceData.Set(serviceUUID, orderedmap.New[string, [][]byte]())
	}

	return &ServiceDataSimulatorBuilder{
		parent:      b,
		serviceUUID: serviceUUID,
	}
}

// WithCharacteristic adds a characteristic with data to this service
func (s *ServiceDataSimulatorBuilder) WithCharacteristic(charUUID string, data []byte) *ServiceDataSimulatorBuilder {
	charMap, exists := s.parent.serviceData.Get(s.serviceUUID)
	if !exists {
		panic(fmt.Sprintf("WithCharacteristic: must call WithService() before adding characteristics (attempted to add characteristic %q to service %q)", charUUID, s.serviceUUID))
	}

	if s.parent.allowMultiValue {
		// Append to existing values
		existing, _ := charMap.Get(charUUID)
		charMap.Set(charUUID, append(existing, data))
	} else {
		// Overwrite: keep only the last value at index 0
		existing, hasExisting := charMap.Get(charUUID)
		if !hasExisting || len(existing) == 0 {
			charMap.Set(charUUID, [][]byte{data})
		} else {
			existing[0] = data
			charMap.Set(charUUID, existing)
		}
	}
	return s
}

// WithService adds another service to the simulation and returns a service-specific builder
func (s *ServiceDataSimulatorBuilder) WithService(serviceUUID string) *ServiceDataSimulatorBuilder {
	return s.parent.WithService(serviceUUID)
}

// Build returns the parent PeripheralDataSimulatorBuilder for continued chaining
func (s *ServiceDataSimulatorBuilder) Build() *PeripheralDataSimulatorBuilder {
	if s.parent == nil {
		panic("BUG: ServiceDataSimulatorBuilder.parent is nil - builder was not properly initialized")
	}
	return s.parent
}

// Simulate executes all configured characteristic data simulations for this service and all others
func (s *ServiceDataSimulatorBuilder) Simulate(verbose bool) *ServiceDataSimulatorBuilder {
	if _, err := s.parent.Simulate(verbose); err != nil {
		panic(fmt.Sprintf("ServiceDataSimulatorBuilder.Simulate: %v", err))
	}
	return s
}

// Simulate executes all configured characteristic data simulations
// It sends notifications index-by-index across all characteristics (round-robin style) in insertion order
func (b *PeripheralDataSimulatorBuilder) Simulate(verbose bool) (*PeripheralDataSimulatorBuilder, error) {
	conn := b.suite.LuaApi.GetDevice().GetConnection()
	b.suite.NotNil(conn, "Connection should be available")

	bleConn, ok := conn.(*device.BLEConnection)
	if !ok {
		panic(fmt.Sprintf("BUG: connection is not a *device.BLEConnection (got %T) - this indicates a test setup error", conn))
	}

	// Find maximum number of values across all characteristics
	maxIndex := 0
	serviceCount := 0
	charCount := 0

	for servicePair := b.serviceData.Oldest(); servicePair != nil; servicePair = servicePair.Next() {
		serviceCount++
		charMap := servicePair.Value
		for charPair := charMap.Oldest(); charPair != nil; charPair = charPair.Next() {
			charCount++
			dataList := charPair.Value
			if len(dataList) > maxIndex {
				maxIndex = len(dataList)
			}
		}
	}

	// Always log simulation starts with a summary
	b.logf("DEBUG: Starting BLE simulation - services=%d characteristics=%d max_notifications_per_char=%d",
		serviceCount, charCount, maxIndex)

	// Iterate index-by-index across all characteristics IN INSERTION ORDER
	notificationCount := 0
	errorCount := 0
	for idx := 0; idx < maxIndex; idx++ {
		for servicePair := b.serviceData.Oldest(); servicePair != nil; servicePair = servicePair.Next() {
			serviceUUID := servicePair.Key
			charMap := servicePair.Value

			for charPair := charMap.Oldest(); charPair != nil; charPair = charPair.Next() {
				charUUID := charPair.Key
				dataList := charPair.Value

				// Skip if this characteristic doesn't have a value at this index
				if idx >= len(dataList) {
					continue
				}

				testChar, err := conn.GetCharacteristic(serviceUUID, charUUID)
				if err != nil {
					errorCount++
					b.logf("ERROR: Failed to get characteristic %s:%s - %v", serviceUUID, charUUID, err)
					if !b.suite.NoError(err, "Should be able to get characteristic %s:%s", serviceUUID, charUUID) {
						continue
					}
				}

				data := dataList[idx]
				notificationCount++

				if verbose {
					b.logf("DEBUG: Sending notification #%d: service=%s char=%s data=%q (len=%d)",
						notificationCount, serviceUUID, charUUID, data, len(data))
				}

				bleConn.ProcessCharacteristicNotification(testChar, data)
			}
		}
	}

	// Always log completion summary
	b.logf("INFO: BLE simulation completed - sent=%d notifications, errors=%d", notificationCount, errorCount)

	return b, nil
}
