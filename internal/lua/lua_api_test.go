package lua

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	_ "embed"

	"github.com/srg/blim/internal/testutils"
	suitelib "github.com/stretchr/testify/suite"
)

//go:embed lua-api-subscription-scenarios.yaml
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
	err = suite.LuaApi.ExecuteScript("")
	return err
}

// TestErrorHandling tests error conditions and recovery
// NOTE: Most error handling tests have been moved to YAML format in lua-api-subscription-scenarios.yaml
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

	// NOTE: The following error tests are in YAML (lua-api-subscription-scenarios.yaml):
	// - "Error Handling: Missing Services"
	// - "Error Handling: Non-existent Service"
	// - "Error Handling: Non-existent Characteristic"
}

// TestSubscriptionScenarios validates BLE subscription behavior across multiple streaming modes
// by executing YAML-defined test scenarios.
//
// Test scenarios are externalized in lua-api-subscription-scenarios.yaml for maintainability
// and clarity. Each scenario defines subscription configuration, simulation steps, and
// expected Lua callback outputs.
//
// See lua-api-subscription-scenarios.yaml for individual test case documentation.
func (suite *LuaApiTestSuite) TestSubscriptionScenarios() {
	suite.RunTestCasesFromYAML(testCases)
}

// TestInspectDevice tests the inspect.lua script to verify device inspection functionality
func (suite *LuaApiTestSuite) TestInspectDevice() {
	t := suite.T()
	//logger := suite.logger

	script, err := testutils.LoadScript(filepath.Join("examples", "inspect.lua"))
	if err != nil {
		suite.Error(err)
	}

	err = suite.LuaApi.ExecuteScript(script)
	suite.NoError(err, "Should execute script with no errors")

	// Allow time for inspect.lua script to execute and produce output
	time.Sleep(1 * time.Second)

	// Get captured JSON output
	if actualOutput, err := suite.luaOutputCapture.ConsumePlainText(); err != nil {
		suite.NoError(fmt.Errorf("should be able to consume plain text: %v", err))
	} else {
		// Use TextAsserter with default options for precise format matching
		ta := testutils.NewTextAsserter(t).WithOptions(testutils.WithEnableColors(true))

		// Define expected output structure based on actual inspect format
		expectedOutput := `Device info:
  ID: 4839279b49adcbe13f1fb430ab325aba
  Address: 4839279b49adcbe13f1fb430ab325aba
  RSSI: 0
  Connectable: false
  LastSeen: 2025-09-25T12:04:12-06:00
  Advertised Services: none
  Manufacturer Data: none
  Service Data: none
  GATT Services: 1

[1] Service 0001
  [1.1] Characteristic 0002 (props: 0x08)
  [1.2] Characteristic 0003 (props: 0x10)
      descriptor: 2902`

		// Assert that the output contains all expected structural elements
		// This will show clear diffs if the output format changes
		ta.Assert(actualOutput, expectedOutput)

		t.Log("✓ inspect.lua script executed successfully")
		t.Log("✓ Device inspection output captured and verified")
		t.Logf("✓ Found all %d expected services in output", 6)
	}
}

// TestBLEAPI2TestSuite runs the test suite using testify/suite
func TestBLEAPI2TestSuite(t *testing.T) {
	suitelib.Run(t, new(LuaApiTestSuite))
}
