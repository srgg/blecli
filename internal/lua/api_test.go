package lua

import (
	"context"
	"fmt"
	"io"
	"syscall"
	"testing"
	"time"

	_ "embed"

	suitelib "github.com/stretchr/testify/suite"
)

// MockStrategy implements io.ReadWriter for testing
type MockStrategy struct {
	WriteFunc func(data []byte) (int, error)
	ReadFunc  func(p []byte) (n int, err error)
	CloseFunc func() error
}

func (m *MockStrategy) Write(data []byte) (int, error) {
	if m.WriteFunc != nil {
		return m.WriteFunc(data)
	}
	return 0, fmt.Errorf("PTY operations not available (not running in bridge mode)")
}

func (m *MockStrategy) Read(p []byte) (int, error) {
	if m.ReadFunc != nil {
		return m.ReadFunc(p)
	}
	return 0, fmt.Errorf("PTY operations not available (not running in bridge mode)")
}

func (m *MockStrategy) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

// testBridgeInfo implements BridgeInfo for testing
type testBridgeInfo struct {
	ttyName        string
	ttySymlinkPath string
	ptyIO          *MockStrategy
	readCallback   func([]byte) // Store callback for testing
}

func (t *testBridgeInfo) GetTTYName() string {
	return t.ttyName
}

func (t *testBridgeInfo) GetTTYSymlink() string {
	return t.ttySymlinkPath
}

func (t *testBridgeInfo) GetPTY() io.ReadWriter {
	return t.ptyIO
}

func (t *testBridgeInfo) SetPTYReadCallback(cb func([]byte)) {
	t.readCallback = cb
}

// TriggerCallback simulates PTY data arrival for testing
func (t *testBridgeInfo) TriggerCallback(data []byte) {
	if t.readCallback != nil {
		t.readCallback(data)
	}
}

// BLEAPI2TestSuite
type LuaApiTestSuite struct {
	LuaApiSuite
}

// AssertLuaError verifies that an error is a *LuaError and contains the expected message
// Fails the current test but allows other tests to continue execution
func (suite *LuaApiTestSuite) AssertLuaError(err error, expectedMessage string, msgAndArgs ...interface{}) bool {
	// Check if there is an error
	if !suite.Error(err, msgAndArgs...) {
		return false
	}

	// Check that the error message contains the expected string
	// This works for both wrapped and unwrapped LuaErrors
	if !suite.Contains(err.Error(), expectedMessage, msgAndArgs...) {
		return false
	}

	return true
}

func (suite *LuaApiTestSuite) ExecuteScript(script string) error {
	err := suite.LuaApi.LoadScript(script, "test")
	suite.NoError(err, "Should load subscription script with nio errors")
	err = suite.LuaApi.ExecuteScript(context.Background(), "")
	return err
}

// TestErrorHandling tests error conditions and recovery
// NOTE: Most error handling tests have been moved to YAML format in lua-api-test-test-scenarios.yaml
//
//	The following tests remain in Go because they test Lua syntax errors that cannot be
//	generated through the YAML framework (which always generates valid subscription scripts)
func (suite *LuaApiTestSuite) TestErrorHandling() {
	suite.Run("Lua: Missing callback", func() {
		// GOAL: Verify blim.subscribe() returns a clear error when the Callback field is missing
		//
		// TEST SCENARIO: Call subscribing without Callback field → Lua error raised → verify error message

		err := suite.ExecuteScript(`
			blim.subscribe{
				services = {
					{
						service = "1234",
						chars = {"5678"}
					}
				},
				Mode = "EveryUpdate",
				MaxRate = 0
				-- Missing Callback
			}
		`)
		suite.AssertLuaError(err, "no callback specified in Lua subscription")
	})

	suite.Run("Lua: Invalid argument type", func() {
		// GOAL: Verify blim.subscribe() returns clear error when passed non-table argument
		//
		// TEST SCENARIO: Call subscribe() with string instead of table → Lua error raised → verify error message

		err := suite.ExecuteScript(`blim.subscribe("not a table")`)
		suite.AssertLuaError(err, "Error: subscribe() expects a lua table argument")
	})

	suite.Run("Lua: Callback causes panic", func() {
		// GOAL: Verify that panics in subscription callbacks are recovered and don't crash the system
		//
		// TEST SCENARIO: Create a subscription with callback that causes Lua panic → send notification → panic recovered → verify error logged

		// Create a subscription with a callback that will cause a panic when it tries to access nil values
		script := `
			blim.subscribe{
				services = {
					{
						service = "1234",
						chars = {"5678"}
					}
				},
				Mode = "EveryUpdate",
				MaxRate = 0,
				Callback = function(record)
					-- This will cause a panic by accessing nil table index deeply
					local x = nil
					local y = x.foo.bar.baz  -- This should cause a Lua error/panic
				end
			}
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should successfully create subscription even if callback will panic")

		// Simulate a notification to trigger the callback
		suite.NewPeripheralDataSimulator().
			WithService("1234").
			WithCharacteristic("5678", []byte{0x01, 0x02}).
			Simulate(false)

		// Give time for the async callback to execute and panic
		time.Sleep(50 * time.Millisecond)

		// The panic should be recovered and logged, but execution should continue
		// We verify this by checking that we can still execute Lua code
		err = suite.ExecuteScript(`print("Still working after panic")`)
		suite.NoError(err, "System should continue working after callback panic")
	})

	suite.Run("Lua: Callback handles missing UUID in record.Values", func() {
		// GOAL: Verify that accessing a non-existent UUID in the record.Values returns nil and can be gracefully handled
		//
		// TEST SCENARIO: Create subscription → send notification → callback accesses non-existent UUID → nil returned → no crash

		script := `
			received_count = 0
			nil_access_count = 0
			valid_data_count = 0

			blim.subscribe{
				services = {
					{
						service = "1234",
						chars = {"5678"}
					}
				},
				Mode = "EveryUpdate",
				MaxRate = 0,
				Callback = function(record)
					received_count = received_count + 1

					-- Access the valid UUID that exists in the notification
					local valid_data = record.Values["5678"]
					if valid_data then
						valid_data_count = valid_data_count + 1
					end

					-- Try to access a non-existent UUID - should return nil
					local missing_data = record.Values["9999"]
					if missing_data == nil then
						nil_access_count = nil_access_count + 1
					end

					-- Verify we can handle nil gracefully without crash
					assert(missing_data == nil, "non-existent UUID should return nil")
					assert(valid_data ~= nil, "valid UUID should have data")
				end
			}
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should successfully create subscription with nil-safe callback")

		// Send notification with the subscribed characteristic
		suite.NewPeripheralDataSimulator().
			WithService("1234").
			WithCharacteristic("5678", []byte{0x01, 0x02}).
			Simulate(false)

		// Give time for callback to execute
		time.Sleep(50 * time.Millisecond)

		// Verify callback was invoked and handled nil access gracefully
		script = `
			assert(received_count == 1, "callback should be invoked once")
			assert(valid_data_count == 1, "valid UUID should be present")
			assert(nil_access_count == 1, "non-existent UUID should return nil")
		`
		err = suite.ExecuteScript(script)
		suite.NoError(err, "Callback should handle missing UUID access gracefully")
	})

	// NOTE: The following error tests are in YAML (lua-api-test-test-scenarios.yaml):
	// - "Error Handling: Missing Services"
	// - "Error Handling: Non-existent Service"
	// - "Error Handling: Non-existent Characteristic"
}

// TestSubscriptionScenarios validates BLE subscription behavior across multiple streaming modes
// by executing YAML-defined test test-scenarios.
//
// Test test-scenarios are externalized in lua-api-test-test-scenarios.yaml for maintainability
// and clarity. Each scenario defines subscription configuration, simulation steps, and
// expected Lua callback outputs.
//
// See lua-api-test-test-scenarios.yaml for individual test case documentation.
func (suite *LuaApiTestSuite) TestSubscriptionScenarios() {
	suite.RunTestCasesFromFile("test-scenarios/lua-api-test-scenarios.yaml")
}

// TestCharacteristicFunction tests the blim.characteristic() function
func (suite *LuaApiTestSuite) TestCharacteristicFunction() {
	suite.Run("Characteristic without descriptors", func() {
		// GOAL: Verify characteristic handle has an empty descriptors array when no descriptors present
		//
		// TEST SCENARIO: Lookup characteristic without descriptors → descriptors is empty table → verify length is 0

		script := `
			local char = blim.characteristic("1234", "5678")
			assert(char ~= nil, "characteristic should not be nil")
			assert(type(char.descriptors) == "table", "descriptors should be a table")
			assert(#char.descriptors == 0, "should have 0 descriptors")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should handle empty descriptors array")
	})

	suite.Run("Properties field validation", func() {
		// GOAL: Verify property field is a table with at least one property sub-table (read/write/notify/indicate)
		//
		// TEST SCENARIO: Lookup characteristic → properties is table → verify at least one property is set (truthy)

		script := `
			local char = blim.characteristic("180D", "2A37")
			assert(char.properties ~= nil, "properties should not be nil")
			assert(type(char.properties) == "table", "properties should be a table")
			-- Check that at least one property is set
			local has_property = char.properties.read or char.properties.write or
			                     char.properties.notify or char.properties.indicate
			assert(has_property, "at least one property should be set")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should have valid properties field")
	})

	suite.Run("Error: Invalid service UUID", func() {
		// GOAL: Verify blim.characteristic() raises error when service UUID not found
		//
		// TEST SCENARIO: Lookup with non-existent service UUID → Lua error raised → verify error message

		script := `
			local char = blim.characteristic("9999", "5678")
		`
		err := suite.ExecuteScript(script)
		suite.AssertLuaError(err, "characteristic not found")
	})

	suite.Run("Error: Invalid characteristic UUID", func() {
		// GOAL: Verify blim.characteristic() raises error when characteristic UUID not found in service
		//
		// TEST SCENARIO: Lookup with valid service but invalid char UUID → Lua error raised → verify error message

		script := `
			local char = blim.characteristic("1234", "9999")
		`
		err := suite.ExecuteScript(script)
		suite.AssertLuaError(err, "characteristic not found")
	})

	suite.Run("Error: Insufficient arguments", func() {
		// GOAL: Verify blim.characteristic() raises error when insufficient arguments provided
		//
		// TEST SCENARIO: Call with zero or one argument → Lua error raised → verify error mentions two arguments required

		// Test with zero arguments
		script := `
			local char = blim.characteristic()
		`
		err := suite.ExecuteScript(script)
		suite.AssertLuaError(err, "expects two string arguments")

		// Test with one argument
		script = `
			local char = blim.characteristic("1234")
		`
		err = suite.ExecuteScript(script)
		suite.AssertLuaError(err, "expects two string arguments")
	})

	suite.Run("Error: Invalid argument type - number converted to string", func() {
		// GOAL: Verify blim.characteristic() handles Lua's implicit number-to-string conversion and fails lookup
		//       (Note: Lua ToString() converts numbers to strings, so 123 becomes "123", then lookup fails)
		//
		// TEST SCENARIO: Call with number instead of string → number converted to string → lookup fails → error raised

		script := `
			local char = blim.characteristic(123, "5678")
		`
		err := suite.ExecuteScript(script)
		// The number 123 gets converted to string "123", then lookup fails
		suite.AssertLuaError(err, "characteristic not found")
	})

	suite.Run("Error: Invalid argument type - table instead of string", func() {
		// GOAL: Verify blim.characteristic() raises error when passed table instead of string UUID
		//
		// TEST SCENARIO: Call with table as service UUID → Lua error raised → verify error mentions string arguments

		script := `
			local char = blim.characteristic({service="1234"}, "5678")
		`
		err := suite.ExecuteScript(script)
		suite.AssertLuaError(err, "expects two string arguments")
	})

	suite.Run("All metadata fields present", func() {
		// GOAL: Verify characteristic handle contains all required metadata fields with correct types
		//
		// TEST SCENARIO: Lookup characteristic → verify uuid/service/properties/descriptors exist → verify correct types

		script := `
			local char = blim.characteristic("180F", "2A19")
			-- Verify all required fields exist
			assert(char.uuid ~= nil, "uuid field should exist")
			assert(char.service ~= nil, "service field should exist")
			assert(char.properties ~= nil, "properties field should exist")
			assert(char.descriptors ~= nil, "descriptors field should exist")

			-- Verify field types
			assert(type(char.uuid) == "string", "uuid should be string")
			assert(type(char.service) == "string", "service should be string")
			assert(type(char.properties) == "table", "properties should be table")
			assert(type(char.descriptors) == "table", "descriptors should be table")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should have all required metadata fields")
	})

	suite.Run("Descriptor array is 1-indexed", func() {
		// GOAL: Verify descriptors array follows Lua 1-indexed convention (index 0 is nil)
		//
		// TEST SCENARIO: Get characteristic → access descriptors[0] → verify it's nil (Lua arrays start at 1)

		script := `
			local char = blim.characteristic("180D", "2A37")
			-- Lua arrays are 1-indexed (even if empty)
			assert(char.descriptors[0] == nil, "index 0 should be nil")
			-- Note: descriptor count varies by characteristic
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should use 1-indexed descriptor array")
	})

	suite.Run("Multiple calls return consistent data", func() {
		// GOAL: Verify blim.characteristic() returns consistent metadata across multiple calls for same characteristic
		//
		// TEST SCENARIO: Call twice for same characteristic → compare all fields → verify identical metadata

		script := `
			local char1 = blim.characteristic("1234", "5678")
			local char2 = blim.characteristic("1234", "5678")
			assert(char1.uuid == char2.uuid, "uuid should be consistent")
			assert(char1.service == char2.service, "service should be consistent")

			-- Compare properties: check both presence (truthy/falsy) matches
			-- Properties are now tables with value/name, so compare their presence not reference
			assert((char1.properties.read ~= nil) == (char2.properties.read ~= nil), "read property presence should be consistent")
			assert((char1.properties.write ~= nil) == (char2.properties.write ~= nil), "write property presence should be consistent")
			assert((char1.properties.notify ~= nil) == (char2.properties.notify ~= nil), "notify property presence should be consistent")
			assert((char1.properties.indicate ~= nil) == (char2.properties.indicate ~= nil), "indicate property presence should be consistent")

			-- Verify property values match when both present - unconditional assertion using logical implication
			local both_have_read = (char1.properties.read ~= nil) and (char2.properties.read ~= nil)
			local neither_has_read = (char1.properties.read == nil) and (char2.properties.read == nil)
			assert(both_have_read or neither_has_read, "read property presence MUST match (verified above)")

			-- Unconditional assertion: if both have read property, values MUST match
			assert((not both_have_read) or (char1.properties.read.value == char2.properties.read.value),
				"read property value MUST be consistent when both have read property")

			assert(#char1.descriptors == #char2.descriptors, "descriptor count should be consistent")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should return consistent data across calls")
	})
}

// TestCharacteristicRead tests the characteristic.read() method
func (suite *LuaApiTestSuite) TestCharacteristicRead() {
	// Set up custom peripheral with Device Information Service and other services for read tests
	suite.WithPeripheral().FromJSON(`{
		"services": [
			{
				"uuid": "180A",
				"characteristics": [
					{ "uuid": "2A29", "properties": "read", "value": [66, 76, 73, 77, 67, 111] }
				]
			},
			{
				"uuid": "180F",
				"characteristics": [
					{ "uuid": "2A19", "properties": "read,notify", "value": [85] }
				]
			},
			{
				"uuid": "180D",
				"characteristics": [
					{ "uuid": "2A37", "properties": "read,notify", "value": [0, 75] }
				]
			},
			{
				"uuid": "1234",
				"characteristics": [
					{ "uuid": "5678", "properties": "read,notify", "value": [42] }
				]
			},
			{
				"uuid": "AAAA",
				"characteristics": [
					{ "uuid": "BBBB", "properties": "write", "value": [99] }
				]
			}
		]
	}`).Build()

	suite.Run("Successful read returns value and nil error", func() {
		// GOAL: Verify read() returns a non-nil value and nil error on successful read of a readable characteristic
		//
		// TEST SCENARIO: Read from readable characteristic → value returned with no error → verify value is non-empty string

		script := `
			local char = blim.characteristic("180A", "2A29")  -- Device Info: Manufacturer Name

			if not char.properties.read then
				error("Test setup error: characteristic should be readable")
			end

			local value, err = char.read()
			assert(value ~= nil, "read should return value")
			assert(err == nil, "read should not return error")
			assert(type(value) == "string", "value should be string")
			assert(#value > 0, "value should not be empty")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should successfully read readable characteristic")
	})

	suite.Run("Read returns nil and error on failure", func() {
		// GOAL: Verify read() returns nil value and error string when reading write-only characteristic
		//
		// TEST SCENARIO: Read the write-only characteristic → error returned with nil value → verify error message format

		script := `
			local char = blim.characteristic("AAAA", "BBBB")
			local value, err = char.read()

			-- MUST fail because the characteristic doesn't support read
			assert(err ~= nil, "read MUST fail on write-only characteristic")
			assert(value == nil, "value MUST be nil when error occurs")
			assert(type(err) == "string", "error MUST be string")
			assert(#err > 0, "error message must not be empty")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should properly error on non-readable characteristic")
	})

	suite.Run("Read multiple characteristics", func() {
		// GOAL: Verify read() can successfully read from multiple different characteristics in a loop
		//
		// TEST SCENARIO: Loop through all characteristics → read readable ones → count successful reads ≥ 1

		script := `
			local services = blim.list()
			local read_count = 0
			local total_checked = 0

			for _, service_uuid in ipairs(services) do
				local service_info = services[service_uuid]
				for _, char_uuid in ipairs(service_info.characteristics) do
					local char = blim.characteristic(service_uuid, char_uuid)
					total_checked = total_checked + 1

					-- Read ALWAYS and verify based on properties
					local value, err = char.read()

					-- MUST verify every read operation - unconditional assertions
					local is_readable = (char.properties.read ~= nil)
					local read_succeeded = (err == nil)
					local has_value = (value ~= nil)

					-- Assertions ALWAYS execute - test relationship between property and result
					assert(is_readable == read_succeeded,
						"read result MUST match readable property for " .. char_uuid ..
						" (readable=" .. tostring(is_readable) .. ", succeeded=" .. tostring(read_succeeded) .. ")")
					assert(is_readable == has_value,
						"value presence MUST match readable property for " .. char_uuid ..
						" (readable=" .. tostring(is_readable) .. ", has_value=" .. tostring(has_value) .. ")")

					if is_readable then
						read_count = read_count + 1
					end
				end
			end

			-- Unconditional assertions ALWAYS execute
			assert(total_checked > 0, "MUST have checked at least one characteristic")
			assert(read_count > 0, "MUST successfully read at least one characteristic")
			print("Successfully read " .. read_count .. " of " .. total_checked .. " characteristics")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should read multiple characteristics")
	})

	suite.Run("Read same characteristic multiple times", func() {
		// GOAL: Verify read() is idempotent and can be called multiple times on the same characteristic
		//
		// TEST SCENARIO: Read the same characteristic 3 times → all return success → verify consistent behavior

		script := `
			local char = blim.characteristic("180f", "2a19")  -- Battery Level

			local value1, err1 = char.read()
			local value2, err2 = char.read()
			local value3, err3 = char.read()

			assert(value1 ~= nil, "first read should succeed")
			assert(value2 ~= nil, "second read should succeed")
			assert(value3 ~= nil, "third read should succeed")

			-- All reads should succeed consistently
			assert(err1 == nil, "first read should not error")
			assert(err2 == nil, "second read should not error")
			assert(err3 == nil, "third read should not error")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should allow multiple reads")
	})

	suite.Run("Read value is binary safe", func() {
		// GOAL: Verify read() returns binary-safe byte string accessible via string.byte
		//
		// TEST SCENARIO: Read characteristic with byte value 85 → access via string.byte → verify numeric value in range 0-255

		script := `
			local char = blim.characteristic("180f", "2a19")  -- Battery Level
			local value, err = char.read()

			assert(err == nil, "read should succeed")
			assert(value ~= nil, "value should not be nil")
			assert(#value > 0, "value should not be empty")

			-- Test binary data access using string.byte
			local first_byte = string.byte(value, 1)
			assert(type(first_byte) == "number", "string.byte should return number")
			assert(first_byte >= 0 and first_byte <= 255, "byte should be 0-255")
			assert(first_byte == 85, "battery level should be 85")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should handle binary data correctly")
	})

	suite.Run("Error: read() on non-connected device", func() {
		// GOAL: Verify read() returns error when called on disconnected device
		//
		// TEST SCENARIO: Disconnect device → attempt read → error returned with nil value

		// Disconnect the device first
		disconnectErr := suite.LuaApi.GetDevice().Disconnect()
		suite.NoError(disconnectErr, "Should disconnect successfully")

		script := `
			local char = blim.characteristic("1234", "5678")
			local value, err = char.read()

			-- MUST fail because device is not connected
			assert(err ~= nil, "read MUST fail on non-connected device, got err=" .. tostring(err) .. " value=" .. tostring(value))
			assert(value == nil, "value MUST be nil when error occurs")
			assert(type(err) == "string", "error MUST be string")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should properly error on disconnected device")
	})

	suite.Run("Binary data parsing with string.byte", func() {
		// GOAL: Verify multibyte characteristic values can be parsed using string.byte for individual bytes
		//
		// TEST SCENARIO: Read Heart Rate characteristic (2 bytes: flags + bpm) → extract both bytes → verify numeric values

		script := `
			-- Read multi-byte characteristic (Heart Rate: flag byte + value)
			local hr = blim.characteristic("180d", "2a37")
			local hr_value, err = hr.read()

			assert(err == nil, "read should succeed")
			assert(hr_value ~= nil, "value should not be nil")
			assert(#hr_value >= 2, "heart rate value should have at least 2 bytes")

			local flags = string.byte(hr_value, 1)
			local bpm = string.byte(hr_value, 2)
			assert(type(flags) == "number", "flags should be number")
			assert(type(bpm) == "number", "bpm should be number")
			assert(flags >= 0 and flags <= 255, "flags byte should be 0-255")
			assert(bpm >= 0 and bpm <= 255, "bpm byte should be 0-255")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should parse multi-byte binary data")
	})

	suite.Run("Verify read() is a method not a field", func() {
		// GOAL: Verify read() is exposed as a callable method (userdata type in aarzilli/golua, not a field)
		//
		// TEST SCENARIO: Get a characteristic handle → check a read type is userdata/function → verify callable

		script := `
			local char = blim.characteristic("180f", "2a19")

			-- read should be a callable (in aarzilli/golua, Go functions are userdata type, not "function")
			-- The important thing is that it's not nil and can be called
			assert(char.read ~= nil, "read should not be nil")
			assert(type(char.read) == "function" or type(char.read) == "userdata",
			       "read should be callable (function or userdata), got: " .. type(char.read))

			-- Calling it should work
			local value, err = char.read()
			-- Result validation
			assert(value ~= nil or err ~= nil, "should return either value or error")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "read should be a callable method")
	})

	suite.Run("Read empty/zero-length values", func() {
		// GOAL: Verify read() handles values of any length, including zero-length strings
		//
		// TEST SCENARIO: Read characteristic → verify a string type and non-negative length (including zero)

		script := `
			local char = blim.characteristic("180a", "2a29")
			local value, err = char.read()

			-- Read should succeed
			assert(err == nil, "read should not error")
			assert(value ~= nil, "value should not be nil")
			-- Empty string is valid (length 0)
			assert(type(value) == "string", "should be string type")
			assert(#value >= 0, "length should be non-negative")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should handle empty values")
	})
}

// TestLuaBridgeAccess tests blim.bridge exposure to Lua
func (suite *LuaApiTestSuite) TestLuaBridgeAccess() {
	suite.Run("Bridge not set - raises error on getter function calls", func() {
		// GOAL: Verify blim.bridge exists, but raises an error when calling getter functions in non-bridge mode
		//
		// TEST SCENARIO: No SetBridge() called → blim.bridge exists → calling getter functions raises error

		// First verify blim.bridge exists for bridge mode detection
		script := `
			assert(blim.bridge ~= nil, "blim.bridge should exist for mode detection")
			assert(type(blim.bridge) == "table", "blim.bridge should be table")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "blim.bridge should exist for mode detection")

		// Verify calling tty_name() raises error
		script = `
			local pty = blim.bridge.tty_name()
		`
		err = suite.ExecuteScript(script)
		suite.AssertLuaError(err, "not available (not running in bridge mode)")

		// Verify calling tty_symlink() raises error
		script = `
			local symlink = blim.bridge.tty_symlink()
		`
		err = suite.ExecuteScript(script)
		suite.AssertLuaError(err, "not available (not running in bridge mode)")
	})

	suite.Run("Bridge is set - PTY and symlink info accessible", func() {
		// GOAL: Verify blim.bridge contains tty_name and tty_symlink when bridge is set
		//
		// TEST SCENARIO: SetBridge() called with test bridge → blim.bridge populated → verify fields accessible

		// Create test bridge with mock strategy
		mockStrategy := &MockStrategy{}
		testBridge := &testBridgeInfo{
			ttyName:        "/dev/ttys999",
			ttySymlinkPath: "/tmp/test-bridge-link",
			ptyIO:          mockStrategy,
		}

		// Set bridge info in Lua API
		suite.LuaApi.SetBridge(testBridge)

		script := `
			-- Verify blim.bridge table exists
			assert(blim.bridge ~= nil, "blim.bridge should exist")
			assert(type(blim.bridge) == "table", "blim.bridge should be table")

			-- Verify tty_name field
			assert(blim.bridge.tty_name() ~= nil, "tty_name should be set")
			assert(type(blim.bridge.tty_name()) == "string", "tty_name should be string")
			assert(blim.bridge.tty_name() == "/dev/ttys999", "tty_name should match mock value")

			-- Verify tty_symlink field
			assert(blim.bridge.tty_symlink() ~= nil, "tty_symlink should be set")
			assert(type(blim.bridge.tty_symlink()) == "string", "tty_symlink should be string")
			assert(blim.bridge.tty_symlink() == "/tmp/test-bridge-link", "tty_symlink should match mock value")

			print("✓ blim.bridge.tty_name: " .. blim.bridge.tty_name())
			print("✓ blim.bridge.tty_symlink: " .. blim.bridge.tty_symlink())
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should access bridge info when set")
	})
}

// TestPTYWrite tests pty_write() function through the Lua API
func (suite *LuaApiTestSuite) TestPTYWrite() {
	suite.Run("Successful write returns bytes written", func() {
		// GOAL: Verify pty_write() successfully writes data and returns byte count
		//
		// TEST SCENARIO: Set up bridge → call pty_write("test") → verify returns byte count and no error

		// Create a mock strategy to capture written data
		var writtenData []byte
		mockStrategy := &MockStrategy{
			WriteFunc: func(data []byte) (int, error) {
				writtenData = append(writtenData, data...)
				return len(data), nil
			},
		}

		testBridge := &testBridgeInfo{
			ttyName:        "/dev/ttys999",
			ttySymlinkPath: "",
			ptyIO:          mockStrategy,
		}

		suite.LuaApi.SetBridge(testBridge)

		script := `
			local bytes, err = blim.bridge.pty_write("Hello PTY")
			assert(err == nil, "pty_write should not return error, got: " .. tostring(err))
			assert(bytes == 9, "should write 9 bytes, got: " .. tostring(bytes))
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should successfully write to PTY")
		suite.Equal("Hello PTY", string(writtenData), "Should write correct data")
	})

	suite.Run("Handles binary data", func() {
		// GOAL: Verify pty_write() correctly handles binary data with null bytes and non-printable characters
		//
		// TEST SCENARIO: Write binary data with \x00 bytes → verify exact bytes written

		var writtenData []byte
		mockStrategy := &MockStrategy{
			WriteFunc: func(data []byte) (int, error) {
				writtenData = append(writtenData, data...)
				return len(data), nil
			},
		}

		testBridge := &testBridgeInfo{
			ttyName: "/dev/ttys999",
			ptyIO:   mockStrategy,
		}

		suite.LuaApi.SetBridge(testBridge)

		script := `
			local binary_data = "\x01\x02\x00\xFF\x03"
			local bytes, err = blim.bridge.pty_write(binary_data)
			assert(err == nil, "pty_write should not return error")
			assert(bytes == 5, "should write 5 bytes")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should handle binary data")
		suite.Equal([]byte{0x01, 0x02, 0x00, 0xFF, 0x03}, writtenData, "Should preserve binary data")
	})

	suite.Run("Error on write failure", func() {
		// GOAL: Verify pty_write() returns error when Write() fails
		//
		// TEST SCENARIO: Mock Write() returns error → pty_write() returns (nil, error_message)

		mockStrategy := &MockStrategy{
			WriteFunc: func(data []byte) (int, error) {
				return 0, fmt.Errorf("simulated write error")
			},
		}

		testBridge := &testBridgeInfo{
			ttyName: "/dev/ttys999",
			ptyIO:   mockStrategy,
		}

		suite.LuaApi.SetBridge(testBridge)

		script := `
			local bytes, err = blim.bridge.pty_write("test")
			assert(bytes == nil, "bytes should be nil on error")
			assert(err ~= nil, "should return error")
			assert(type(err) == "string", "error should be string")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should handle write errors")
	})

	suite.Run("Invalid argument type", func() {
		// GOAL: Verify pty_write() returns error when called with non-string argument
		//
		// TEST SCENARIO: Call pty_write(123) → error returned

		mockStrategy := &MockStrategy{}
		testBridge := &testBridgeInfo{
			ttyName: "/dev/ttys999",
			ptyIO:   mockStrategy,
		}

		suite.LuaApi.SetBridge(testBridge)

		script := `
			local bytes, err = blim.bridge.pty_write(123)
			assert(bytes == nil, "bytes should be nil on type error")
			assert(err ~= nil, "should return error for invalid type")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should reject non-string arguments")
	})

	suite.Run("Error when called without bridge", func() {
		// GOAL: Verify pty_write() returns error when bridge not set
		//
		// TEST SCENARIO: Call pty_write() without SetBridge() → error returned

		// Don't set bridge
		script := `
			blim.bridge.pty_write("test")
		`
		err := suite.ExecuteScript(script)
		suite.AssertLuaError(err, "not available (not running in bridge mode)")
	})

	suite.Run("Handles empty string write", func() {
		// GOAL: Verify pty_write() correctly handles empty string (0 bytes written)
		//
		// TEST SCENARIO: Write empty string → returns 0 bytes and no error

		var writeCallCount int
		mockStrategy := &MockStrategy{
			WriteFunc: func(data []byte) (int, error) {
				writeCallCount++
				return len(data), nil
			},
		}

		testBridge := &testBridgeInfo{
			ttyName: "/dev/ttys999",
			ptyIO:   mockStrategy,
		}

		suite.LuaApi.SetBridge(testBridge)

		script := `
			local bytes, err = blim.bridge.pty_write("")
			assert(err == nil, "pty_write MUST not return error for empty string")
			assert(bytes == 0, "MUST write 0 bytes for empty string, got: " .. tostring(bytes))
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should handle empty string write")
		suite.Equal(1, writeCallCount, "Write should be called once even for empty string")
	})
}

// TestPTYRead tests pty_read() function through the Lua API
func (suite *LuaApiTestSuite) TestPTYRead() {
	suite.Run("Successful read returns data", func() {
		// GOAL: Verify pty_read() successfully reads buffered data
		//
		// TEST SCENARIO: Mock Read() returns data → pty_read() returns (data, nil)

		mockStrategy := &MockStrategy{
			ReadFunc: func(p []byte) (int, error) {
				data := []byte("Response data")
				copy(p, data)
				return len(data), nil
			},
		}

		testBridge := &testBridgeInfo{
			ttyName: "/dev/ttys999",
			ptyIO:   mockStrategy,
		}

		suite.LuaApi.SetBridge(testBridge)

		script := `
			local data, err = blim.bridge.pty_read()
			assert(err == nil, "pty_read should not return error")
			assert(data == "Response data", "should read correct data")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should successfully read from PTY")
	})

	suite.Run("Handles binary data", func() {
		// GOAL: Verify pty_read() preserves binary data including null bytes
		//
		// TEST SCENARIO: Read binary data with \x00 → verify exact bytes returned

		mockStrategy := &MockStrategy{
			ReadFunc: func(p []byte) (int, error) {
				data := []byte{0xFF, 0x00, 0x01, 0x7F}
				copy(p, data)
				return len(data), nil
			},
		}

		testBridge := &testBridgeInfo{
			ttyName: "/dev/ttys999",
			ptyIO:   mockStrategy,
		}

		suite.LuaApi.SetBridge(testBridge)

		script := `
			local data, err = blim.bridge.pty_read()
			assert(err == nil, "should not return error")
			assert(#data == 4, "should read 4 bytes")
			assert(string.byte(data, 1) == 0xFF, "first byte should be 0xFF")
			assert(string.byte(data, 2) == 0x00, "second byte should be 0x00")
			assert(string.byte(data, 3) == 0x01, "third byte should be 0x01")
			assert(string.byte(data, 4) == 0x7F, "fourth byte should be 0x7F")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should preserve binary data")
	})

	suite.Run("Returns empty string when EAGAIN", func() {
		// GOAL: Verify pty_read() returns ("", nil) when no data available (EAGAIN)
		//
		// TEST SCENARIO: Mock Read() returns EAGAIN → pty_read() returns ("", nil)

		mockStrategy := &MockStrategy{
			ReadFunc: func(p []byte) (int, error) {
				return 0, syscall.EAGAIN
			},
		}

		testBridge := &testBridgeInfo{
			ttyName: "/dev/ttys999",
			ptyIO:   mockStrategy,
		}

		suite.LuaApi.SetBridge(testBridge)

		script := `
			local data, err = blim.bridge.pty_read()
			assert(err == nil, "should not return error for EAGAIN")
			assert(data == "", "should return empty string when no data available")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should handle EAGAIN gracefully")
	})

	suite.Run("Custom buffer size", func() {
		// GOAL: Verify pty_read() respects custom max_bytes parameter
		//
		// TEST SCENARIO: Call pty_read(128) → verify buffer size passed to Read()

		var requestedSize int
		mockStrategy := &MockStrategy{
			ReadFunc: func(p []byte) (int, error) {
				requestedSize = len(p)
				data := []byte("test")
				copy(p, data)
				return len(data), nil
			},
		}

		testBridge := &testBridgeInfo{
			ttyName: "/dev/ttys999",
			ptyIO:   mockStrategy,
		}

		suite.LuaApi.SetBridge(testBridge)

		script := `
			local data, err = blim.bridge.pty_read(128)
			assert(err == nil, "should not return error")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should accept custom buffer size")
		suite.Equal(128, requestedSize, "Should use custom buffer size")
	})

	suite.Run("Error when called without bridge", func() {
		// GOAL: Verify pty_read() returns error when bridge not set
		//
		// TEST SCENARIO: Call pty_read() without SetBridge() → error returned

		// Don't set the bridge
		script := `
			blim.bridge.pty_read()
		`
		err := suite.ExecuteScript(script)
		suite.AssertLuaError(err, "not available (not running in bridge mode)")
	})

	suite.Run("Error with zero max_bytes", func() {
		// GOAL: Verify pty_read() returns error when max_bytes is zero
		//
		// TEST SCENARIO: Call pty_read(0) → error returned with nil data

		mockStrategy := &MockStrategy{}
		testBridge := &testBridgeInfo{
			ttyName: "/dev/ttys999",
			ptyIO:   mockStrategy,
		}

		suite.LuaApi.SetBridge(testBridge)

		script := `
			local data, err = blim.bridge.pty_read(0)
			assert(data == nil, "data MUST be nil on error")
			assert(err ~= nil, "MUST return error for zero max_bytes")
			assert(type(err) == "string", "error MUST be string")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should reject zero max_bytes")
	})

	suite.Run("Error with negative max_bytes", func() {
		// GOAL: Verify pty_read() returns error when max_bytes is negative
		//
		// TEST SCENARIO: Call pty_read(-1) → error returned with nil data

		mockStrategy := &MockStrategy{}
		testBridge := &testBridgeInfo{
			ttyName: "/dev/ttys999",
			ptyIO:   mockStrategy,
		}

		suite.LuaApi.SetBridge(testBridge)

		script := `
			local data, err = blim.bridge.pty_read(-1)
			assert(data == nil, "data MUST be nil on error")
			assert(err ~= nil, "MUST return error for negative max_bytes")
			assert(type(err) == "string", "error MUST be string")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should reject negative max_bytes")
	})

	suite.Run("Returns empty string on EOF", func() {
		// GOAL: Verify pty_read() returns ("", nil) when Read() returns EOF
		//
		// TEST SCENARIO: Mock Read() returns EOF → pty_read() returns ("", nil)

		mockStrategy := &MockStrategy{
			ReadFunc: func(p []byte) (int, error) {
				return 0, io.EOF
			},
		}

		testBridge := &testBridgeInfo{
			ttyName: "/dev/ttys999",
			ptyIO:   mockStrategy,
		}

		suite.LuaApi.SetBridge(testBridge)

		script := `
			local data, err = blim.bridge.pty_read()
			assert(err == nil, "MUST not return error for EOF")
			assert(data == "", "MUST return empty string on EOF")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should handle EOF gracefully")
	})

	suite.Run("Error on non-EAGAIN read failure", func() {
		// GOAL: Verify pty_read() returns error when Read() fails with non-EAGAIN error
		//
		// TEST SCENARIO: Mock Read() returns generic error → pty_read() returns (nil, error_message)

		mockStrategy := &MockStrategy{
			ReadFunc: func(p []byte) (int, error) {
				return 0, fmt.Errorf("simulated read failure")
			},
		}

		testBridge := &testBridgeInfo{
			ttyName: "/dev/ttys999",
			ptyIO:   mockStrategy,
		}

		suite.LuaApi.SetBridge(testBridge)

		script := `
			local data, err = blim.bridge.pty_read()
			assert(data == nil, "data MUST be nil on error")
			assert(err ~= nil, "MUST return error on read failure")
			assert(type(err) == "string", "error MUST be string")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should handle read errors")
	})
}

// TestPTYOnData tests pty_on_data() callback registration and invocation
func (suite *LuaApiTestSuite) TestPTYOnData() {
	suite.Run("Register callback and receive data", func() {
		// GOAL: Verify pty_on_data() registers callback that receives data when PTY data arrives
		//
		// TEST SCENARIO: Register callback → simulate PTY data → callback invoked with correct data

		mockStrategy := &MockStrategy{}
		testBridge := &testBridgeInfo{
			ttyName: "/dev/ttys999",
			ptyIO:   mockStrategy,
		}

		suite.LuaApi.SetBridge(testBridge)

		script := `
			-- Storage for callback data
			received_data = nil

			-- Register callback
			blim.bridge.pty_on_data(function(data)
				received_data = data
			end)

			-- Callback registered, waiting for data
			assert(received_data == nil, "should not have received data yet")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should register callback")

		// Simulate PTY data arrival
		testBridge.TriggerCallback([]byte("test data"))

		// Verify callback was invoked
		script = `
			assert(received_data ~= nil, "callback MUST receive data")
			assert(received_data == "test data", "callback MUST receive correct data")
		`
		err = suite.ExecuteScript(script)
		suite.NoError(err, "Callback should receive data")
	})

	suite.Run("Unregister callback with nil", func() {
		// GOAL: Verify passing nil to pty_on_data() unregisters the callback
		//
		// TEST SCENARIO: Register callback → unregister with nil → trigger data → callback not invoked

		mockStrategy := &MockStrategy{}
		testBridge := &testBridgeInfo{
			ttyName: "/dev/ttys999",
			ptyIO:   mockStrategy,
		}

		suite.LuaApi.SetBridge(testBridge)

		script := `
			-- Storage for callback data
			call_count = 0

			-- Register callback
			blim.bridge.pty_on_data(function(data)
				call_count = call_count + 1
			end)
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should register callback")

		// Trigger once to verify it works
		testBridge.TriggerCallback([]byte("first"))

		script = `
			assert(call_count == 1, "callback MUST be called once")

			-- Unregister callback
			blim.bridge.pty_on_data(nil)
		`
		err = suite.ExecuteScript(script)
		suite.NoError(err, "Should unregister callback")

		// Trigger again - should not increment
		testBridge.TriggerCallback([]byte("second"))

		script = `
			assert(call_count == 1, "callback MUST not be called after unregister")
		`
		err = suite.ExecuteScript(script)
		suite.NoError(err, "Callback should not be invoked after unregister")
	})

	suite.Run("Error when called without bridge", func() {
		// GOAL: Verify pty_on_data() returns error when bridge not set
		//
		// TEST SCENARIO: Call pty_on_data() without SetBridge() → error returned

		// Don't set bridge
		script := `
			blim.bridge.pty_on_data(function(data) end)
		`
		err := suite.ExecuteScript(script)
		suite.AssertLuaError(err, "not available (not running in bridge mode)")
	})

	suite.Run("Error with invalid argument type", func() {
		// GOAL: Verify pty_on_data() returns error when called with non-function argument
		//
		// TEST SCENARIO: Call pty_on_data("string") → error returned

		mockStrategy := &MockStrategy{}
		testBridge := &testBridgeInfo{
			ttyName: "/dev/ttys999",
			ptyIO:   mockStrategy,
		}

		suite.LuaApi.SetBridge(testBridge)

		script := `
			blim.bridge.pty_on_data("not a function")
		`
		err := suite.ExecuteScript(script)
		suite.AssertLuaError(err, "expects a function or nil argument")
	})

	suite.Run("Callback receives binary data", func() {
		// GOAL: Verify pty_on_data() callback receives binary-safe data including null bytes
		//
		// TEST SCENARIO: Register callback → send binary data with \x00 → callback receives exact bytes

		mockStrategy := &MockStrategy{}
		testBridge := &testBridgeInfo{
			ttyName: "/dev/ttys999",
			ptyIO:   mockStrategy,
		}

		suite.LuaApi.SetBridge(testBridge)

		script := `
			-- Storage for callback data
			received_bytes = nil

			-- Register callback
			blim.bridge.pty_on_data(function(data)
				received_bytes = {}
				for i = 1, #data do
					received_bytes[i] = string.byte(data, i)
				end
			end)
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should register callback")

		// Trigger with binary data
		testBridge.TriggerCallback([]byte{0x01, 0x00, 0xFF, 0x7F})

		script = `
			assert(received_bytes ~= nil, "callback MUST receive data")
			assert(#received_bytes == 4, "callback MUST receive 4 bytes")
			assert(received_bytes[1] == 0x01, "first byte MUST be 0x01")
			assert(received_bytes[2] == 0x00, "second byte MUST be 0x00")
			assert(received_bytes[3] == 0xFF, "third byte MUST be 0xFF")
			assert(received_bytes[4] == 0x7F, "fourth byte MUST be 0x7F")
		`
		err = suite.ExecuteScript(script)
		suite.NoError(err, "Callback should receive binary data")
	})

	suite.Run("Multiple data arrivals invoke callback correctly", func() {
		// GOAL: Verify callback receives multiple consecutive data arrivals correctly
		//
		// TEST SCENARIO: Register callback → trigger 3 times with different data → verify all 3 received

		mockStrategy := &MockStrategy{}
		testBridge := &testBridgeInfo{
			ttyName: "/dev/ttys999",
			ptyIO:   mockStrategy,
		}

		suite.LuaApi.SetBridge(testBridge)

		script := `
			-- Storage for callback data
			received_data = {}

			-- Register callback
			blim.bridge.pty_on_data(function(data)
				table.insert(received_data, data)
			end)
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should register callback")

		// Trigger 3 times with different data
		testBridge.TriggerCallback([]byte("first"))
		testBridge.TriggerCallback([]byte("second"))
		testBridge.TriggerCallback([]byte("third"))

		script = `
			assert(#received_data == 3, "callback MUST be invoked 3 times")
			assert(received_data[1] == "first", "first data MUST be correct")
			assert(received_data[2] == "second", "second data MUST be correct")
			assert(received_data[3] == "third", "third data MUST be correct")
		`
		err = suite.ExecuteScript(script)
		suite.NoError(err, "Callback should receive all data arrivals")
	})

	suite.Run("Callback replacement updates handler correctly", func() {
		// GOAL: Verify registering new callback replaces old one and old callback is not invoked
		//
		// TEST SCENARIO: Register callback A → trigger data → register callback B → trigger data → only B receives second

		mockStrategy := &MockStrategy{}
		testBridge := &testBridgeInfo{
			ttyName: "/dev/ttys999",
			ptyIO:   mockStrategy,
		}

		suite.LuaApi.SetBridge(testBridge)

		script := `
			-- Storage for callbacks
			callback_a_count = 0
			callback_b_count = 0

			-- Register first callback
			blim.bridge.pty_on_data(function(data)
				callback_a_count = callback_a_count + 1
			end)
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should register first callback")

		// Trigger once for callback A
		testBridge.TriggerCallback([]byte("for_a"))

		script = `
			assert(callback_a_count == 1, "callback A MUST be called once")
			assert(callback_b_count == 0, "callback B MUST not be called yet")

			-- Register second callback (should replace first)
			blim.bridge.pty_on_data(function(data)
				callback_b_count = callback_b_count + 1
			end)
		`
		err = suite.ExecuteScript(script)
		suite.NoError(err, "Should register second callback")

		// Trigger once for callback B
		testBridge.TriggerCallback([]byte("for_b"))

		script = `
			assert(callback_a_count == 1, "callback A MUST not be called again")
			assert(callback_b_count == 1, "callback B MUST be called once")
		`
		err = suite.ExecuteScript(script)
		suite.NoError(err, "Only new callback should be invoked")
	})

	suite.Run("Callback error recovery prevents crash", func() {
		// GOAL: Verify panic in the callback is recovered and Lua API continues working
		//
		// TEST SCENARIO: Register callback that panics → trigger data → panic recovered → system still works

		mockStrategy := &MockStrategy{}
		testBridge := &testBridgeInfo{
			ttyName: "/dev/ttys999",
			ptyIO:   mockStrategy,
		}

		suite.LuaApi.SetBridge(testBridge)

		script := `
			-- Register callback that will panic
			blim.bridge.pty_on_data(function(data)
				-- This will cause a Lua error/panic
				local x = nil
				local y = x.foo.bar  -- attempt to index nil
			end)
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should register callback even if it will panic")

		// Trigger callback - should panic but be recovered
		testBridge.TriggerCallback([]byte("trigger panic"))

		// Give time for the async callback to execute and panic to be recovered
		time.Sleep(50 * time.Millisecond)

		// Verify system still works by registering a new callback
		script = `
			good_callback_count = 0

			-- Register new callback that works
			blim.bridge.pty_on_data(function(data)
				good_callback_count = good_callback_count + 1
			end)
		`
		err = suite.ExecuteScript(script)
		suite.NoError(err, "Should register new callback after panic recovery")

		// Trigger new callback to verify system works
		testBridge.TriggerCallback([]byte("after recovery"))

		script = `
			assert(good_callback_count == 1, "new callback MUST work after panic recovery")
		`
		err = suite.ExecuteScript(script)
		suite.NoError(err, "System should continue working after callback panic")
	})
}

// TestWellKnownDescriptorTypes validates well-known BLE descriptor value parsing and exposure
// by executing YAML-defined test scenarios.
//
// Test scenarios are externalized in well-known-descriptor-types-cases.yaml for maintainability.
// Each scenario verifies:
//   - Raw descriptor values (hex-encoded bytes)
//   - Parsed descriptor values (structured Lua tables)
//   - Error handling (read failures, parse errors, timeouts)
//
// See test-scenarios/lua-api-well-known-descriptor-types-scenarios.yaml for individual test case documentation.
func (suite *LuaApiTestSuite) TestWellKnownDescriptorTypes() {
	suite.RunTestCasesFromFile("test-scenarios/lua-api-well-known-descriptor-types-scenarios.yaml")
}

// TestLuaAPITestSuite runs the test suite using testify/suite
func TestLuaAPITestSuite(t *testing.T) {
	suitelib.Run(t, new(LuaApiTestSuite))
}
