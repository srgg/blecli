package lua

import (
	"context"
	"testing"
	"time"

	_ "embed"

	"github.com/srg/blim/internal/testutils/mocks"
	suitelib "github.com/stretchr/testify/suite"
)

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
		// GOAL: Verify blim.subscribe() returns clear error when Callback field is missing
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
		// TEST SCENARIO: Call subscribe with string instead of table → Lua error raised → verify error message

		err := suite.ExecuteScript(`blim.subscribe("not a table")`)
		suite.AssertLuaError(err, "Error: subscribe() expects a lua table argument")
	})

	suite.Run("Lua: Callback causes panic", func() {
		// GOAL: Verify that panics in subscription callbacks are recovered and don't crash the system
		//
		// TEST SCENARIO: Create subscription with callback that causes Lua panic → send notification → panic recovered → verify error logged

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
	suite.Run("Valid characteristic lookup", func() {
		// GOAL: Verify blim.characteristic() returns handle with correct metadata fields (uuid, service, properties, descriptors)
		//
		// TEST SCENARIO: Lookup valid characteristic → handle returned → verify all metadata fields present

		script := `
			local char = blim.characteristic("1234", "5678")
			assert(char ~= nil, "characteristic should not be nil")
			assert(char.uuid == "5678", "uuid should match")
			assert(char.service == "1234", "service should match")
			assert(char.properties ~= nil, "properties should not be nil")
			assert(char.descriptors ~= nil, "descriptors should not be nil")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should execute valid characteristic lookup")
	})

	suite.Run("Characteristic without descriptors", func() {
		// GOAL: Verify characteristic handle has empty descriptors array when no descriptors present
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
		// GOAL: Verify properties field is table with at least one boolean property (read/write/notify/indicate)
		//
		// TEST SCENARIO: Lookup characteristic → properties is table → verify at least one property is set

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

	suite.Run("Error: Missing arguments", func() {
		// GOAL: Verify blim.characteristic() raises error when only one argument provided (needs two)
		//
		// TEST SCENARIO: Call with only service UUID → Lua error raised → verify error mentions two arguments

		script := `
			local char = blim.characteristic("1234")
		`
		err := suite.ExecuteScript(script)
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

	suite.Run("Error: No arguments provided", func() {
		// GOAL: Verify blim.characteristic() raises error when called with no arguments
		//
		// TEST SCENARIO: Call with no arguments → Lua error raised → verify error mentions two string arguments

		script := `
			local char = blim.characteristic()
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

			-- Compare properties table contents (tables aren't equal by reference in Lua)
			assert(char1.properties.read == char2.properties.read, "read property should be consistent")
			assert(char1.properties.write == char2.properties.write, "write property should be consistent")
			assert(char1.properties.notify == char2.properties.notify, "notify property should be consistent")
			assert(char1.properties.indicate == char2.properties.indicate, "indicate property should be consistent")

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

			for _, service_uuid in ipairs(services) do
				local service_info = services[service_uuid]
				for _, char_uuid in ipairs(service_info.characteristics) do
					local char = blim.characteristic(service_uuid, char_uuid)

					if char.properties.read then
						local value, err = char.read()
						assert(err == nil, "read should succeed for readable characteristic " .. char_uuid)
						assert(value ~= nil, "value should not be nil for readable characteristic " .. char_uuid)
						read_count = read_count + 1
					end
				end
			end

			assert(read_count > 0, "should successfully read at least one characteristic")
			print("Successfully read " .. read_count .. " characteristics")
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

	suite.Run("Check read property before reading", func() {
		// GOAL: Demonstrate the best practice of checking read property before calling read() (intentional conditional logic)
		//
		// TEST SCENARIO: Check properties.read flag → conditionally call read() → verify proper pattern usage

		script := `
			local char = blim.characteristic("180d", "2a37")  -- Heart Rate Measurement

			-- Always check if readable before reading
			if char.properties.read then
				local value, err = char.read()
				if value then
					assert(type(value) == "string", "value should be string")
				end
			else
				-- If not readable, read() might fail
				local value, err = char.read()
				-- Should handle gracefully either way
			end
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should check properties before reading")
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
	suite.Run("Bridge not set - raises error on field access", func() {
		// GOAL: Verify blim.bridge exists, but raises an error when accessing fields in non-bridge mode
		//
		// TEST SCENARIO: No SetBridge() called → blim.bridge exists → accessing fields raises error

		// First verify blim.bridge exists for bridge mode detection
		script := `
			assert(blim.bridge ~= nil, "blim.bridge should exist for mode detection")
			assert(type(blim.bridge) == "table", "blim.bridge should be table")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "blim.bridge should exist for mode detection")

		// Verify accessing pty_name raises error
		script = `
			local pty = blim.bridge.pty_name
		`
		err = suite.ExecuteScript(script)
		suite.AssertLuaError(err, "not available (not running in bridge mode)")

		// Verify accessing symlink_path raises error
		script = `
			local symlink = blim.bridge.symlink_path
		`
		err = suite.ExecuteScript(script)
		suite.AssertLuaError(err, "not available (not running in bridge mode)")
	})

	suite.Run("Bridge is set - PTY and symlink info accessible", func() {
		// GOAL: Verify blim.bridge contains pty_name and symlink_path when bridge is set
		//
		// TEST SCENARIO: SetBridge() called with mock bridge → blim.bridge populated → verify fields accessible

		// Create mock bridge using mockery-generated mock
		mockBridge := mocks.NewMockBridgeInfo(suite.T())
		mockBridge.On("GetPTYName").Return("/dev/ttys999")
		mockBridge.On("GetSymlinkPath").Return("/tmp/test-bridge-link")

		// Set bridge info in Lua API
		suite.LuaApi.SetBridge(mockBridge)

		script := `
			-- Verify blim.bridge table exists
			assert(blim.bridge ~= nil, "blim.bridge should exist")
			assert(type(blim.bridge) == "table", "blim.bridge should be table")

			-- Verify pty_name field
			assert(blim.bridge.pty_name ~= nil, "pty_name should be set")
			assert(type(blim.bridge.pty_name) == "string", "pty_name should be string")
			assert(blim.bridge.pty_name == "/dev/ttys999", "pty_name should match mock value")

			-- Verify symlink_path field
			assert(blim.bridge.symlink_path ~= nil, "symlink_path should be set")
			assert(type(blim.bridge.symlink_path) == "string", "symlink_path should be string")
			assert(blim.bridge.symlink_path == "/tmp/test-bridge-link", "symlink_path should match mock value")

			print("✓ blim.bridge.pty_name: " .. blim.bridge.pty_name)
			print("✓ blim.bridge.symlink_path: " .. blim.bridge.symlink_path)
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should access bridge info when set")
	})
}

// TestBLEAPI2TestSuite runs the test suite using testify/suite
func TestBLEAPI2TestSuite(t *testing.T) {
	suitelib.Run(t, new(LuaApiTestSuite))
}
