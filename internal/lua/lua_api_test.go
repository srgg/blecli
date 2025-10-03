package lua

import (
	"context"
	"testing"

	_ "embed"

	suitelib "github.com/stretchr/testify/suite"
)

//go:embed scenarios/lua-api-test-scenarios.yaml
var testCases string

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

//func (suite *LuaApiTestSuite) ExecuteScript2(script string) error {
//	err := suite.LuaApi.LoadScript(script, "test")
//	suite.NoError(err, "Should load subscription script with nio errors")
//	err = suite.LuaApi.ExecuteScript2(context.Background(), "")
//	return err
//}

// TestErrorHandling tests error conditions and recovery
// NOTE: Most error handling tests have been moved to YAML format in lua-api-test-scenarios.yaml
//
//	The following tests remain in Go because they test Lua syntax errors that cannot be
//	generated through the YAML framework (which always generates valid subscription scripts)
func (suite *LuaApiTestSuite) TestErrorHandling() {
	suite.Run("Lua: Missing callback", func() {
		err := suite.ExecuteScript(`
			ble.subscribe{
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
		// Should handle invalid input gracefully
		err := suite.ExecuteScript(`ble.subscribe("not a table")`)
		suite.AssertLuaError(err, "Error: subscribe() expects a lua table argument")
	})

	// NOTE: The following error tests are in YAML (lua-api-test-scenarios.yaml):
	// - "Error Handling: Missing Services"
	// - "Error Handling: Non-existent Service"
	// - "Error Handling: Non-existent Characteristic"
}

// TestSubscriptionScenarios validates BLE subscription behavior across multiple streaming modes
// by executing YAML-defined test scenarios.
//
// Test scenarios are externalized in lua-api-test-scenarios.yaml for maintainability
// and clarity. Each scenario defines subscription configuration, simulation steps, and
// expected Lua callback outputs.
//
// See lua-api-test-scenarios.yaml for individual test case documentation.
func (suite *LuaApiTestSuite) TestSubscriptionScenarios() {
	suite.RunTestCasesFromYAML(testCases)
}

// TestCharacteristicFunction tests the ble.characteristic() function
func (suite *LuaApiTestSuite) TestCharacteristicFunction() {
	suite.Run("Valid characteristic lookup", func() {
		script := `
			local char = ble.characteristic("1234", "5678")
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
		script := `
			local char = ble.characteristic("1234", "5678")
			assert(char ~= nil, "characteristic should not be nil")
			assert(type(char.descriptors) == "table", "descriptors should be a table")
			assert(#char.descriptors == 0, "should have 0 descriptors")
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should handle empty descriptors array")
	})

	suite.Run("Properties field validation", func() {
		script := `
			local char = ble.characteristic("180D", "2A37")
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
		script := `
			local char = ble.characteristic("9999", "5678")
		`
		err := suite.ExecuteScript(script)
		suite.AssertLuaError(err, "characteristic not found")
	})

	suite.Run("Error: Invalid characteristic UUID", func() {
		script := `
			local char = ble.characteristic("1234", "9999")
		`
		err := suite.ExecuteScript(script)
		suite.AssertLuaError(err, "characteristic not found")
	})

	suite.Run("Error: Missing arguments", func() {
		script := `
			local char = ble.characteristic("1234")
		`
		err := suite.ExecuteScript(script)
		suite.AssertLuaError(err, "expects two string arguments")
	})

	suite.Run("Error: Invalid argument type - number converted to string", func() {
		// Note: Lua ToString() converts numbers to strings, so this becomes "123"
		// This tests that the error handling works even when Lua does implicit conversion
		script := `
			local char = ble.characteristic(123, "5678")
		`
		err := suite.ExecuteScript(script)
		// The number 123 gets converted to string "123", then lookup fails
		suite.AssertLuaError(err, "characteristic not found")
	})

	suite.Run("Error: Invalid argument type - table instead of string", func() {
		script := `
			local char = ble.characteristic({service="1234"}, "5678")
		`
		err := suite.ExecuteScript(script)
		suite.AssertLuaError(err, "expects two string arguments")
	})

	suite.Run("Error: No arguments provided", func() {
		script := `
			local char = ble.characteristic()
		`
		err := suite.ExecuteScript(script)
		suite.AssertLuaError(err, "expects two string arguments")
	})

	suite.Run("All metadata fields present", func() {
		script := `
			local char = ble.characteristic("180F", "2A19")
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
		script := `
			local char = ble.characteristic("180D", "2A37")
			-- Lua arrays are 1-indexed (even if empty)
			assert(char.descriptors[0] == nil, "index 0 should be nil")
			-- Note: descriptor count varies by characteristic
		`
		err := suite.ExecuteScript(script)
		suite.NoError(err, "Should use 1-indexed descriptor array")
	})

	suite.Run("Multiple calls return consistent data", func() {
		script := `
			local char1 = ble.characteristic("1234", "5678")
			local char2 = ble.characteristic("1234", "5678")
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

// TestBLEAPI2TestSuite runs the test suite using testify/suite
func TestBLEAPI2TestSuite(t *testing.T) {
	suitelib.Run(t, new(LuaApiTestSuite))
}
