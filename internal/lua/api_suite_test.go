package lua

import (
	"testing"

	suitelib "github.com/stretchr/testify/suite"
)

// LuaApiSuiteTestSuite tests the LuaApiSuite test infrastructure itself
type LuaApiSuiteTestSuite struct {
	LuaApiSuite
}

// TestSuiteScenarios runs test cases that verify the test suite infrastructure itself
func (suite *LuaApiSuiteTestSuite) TestSuiteScenarios() {
	// GOAL: Verify that the test suite infrastructure works correctly
	//
	// TEST SCENARIO: Load and execute all test cases from lua-api-suite-test-test-scenarios.yaml
	//
	// These tests verify error reporting, output validation, and other test infrastructure features
	suite.RunTestCasesFromFile("test-scenarios/lua-api-suite-test-scenarios.yaml")
}

// TestLuaApiSuiteTestSuite runs the suite test infrastructure tests
func TestLuaApiSuiteTestSuite(t *testing.T) {
	suitelib.Run(t, new(LuaApiSuiteTestSuite))
}
