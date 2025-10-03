package lua

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/testutils"
	"github.com/stretchr/testify/suite"
	suitelib "github.com/stretchr/testify/suite"
)

// LuaEngineTestSuite
type LuaEngineTestSuite struct {
	suite.Suite

	helper *testutils.TestHelper // Test helper with logging and assertions
	logger *logrus.Logger        // Structured logger for test output

	luaEngine        *LuaEngine
	luaOutputCapture *LuaOutputCollector
}

func (suite *LuaEngineTestSuite) SetupSuite() {
	suite.helper = testutils.NewTestHelper(suite.T())
	suite.logger = suite.helper.Logger
}

func (suite *LuaEngineTestSuite) SetupTest() {
	suite.luaEngine = NewLuaEngine(suite.logger)

	// Create the string writer
	if lc, err := NewLuaOutputCollector(suite.luaEngine.OutputChannel(), 100, nil); err != nil {
		suite.NoError(fmt.Errorf("error creating lua output collector: %v", err))
	} else {
		suite.luaOutputCapture = lc
		suite.luaOutputCapture.Start()
	}
}

func (suite *LuaEngineTestSuite) TearDownTest() {
	suite.luaOutputCapture.Stop()
	suite.luaEngine.Close()
}

func (suite *LuaEngineTestSuite) SetupSubTest() {
	if suite.luaEngine != nil {
		suite.luaEngine.Close()
	}
	suite.luaEngine = NewLuaEngine(suite.logger)

	if suite.luaOutputCapture != nil {
		suite.luaOutputCapture.Stop()
	}

	if lc, err := NewLuaOutputCollector(suite.luaEngine.OutputChannel(), 100, nil); err != nil {
		suite.NoError(fmt.Errorf("error creating lua output collector: %v", err))
	} else {
		suite.luaOutputCapture = lc
		suite.luaOutputCapture.Start()
	}
}

func (suite *LuaEngineTestSuite) ExecuteScript(script string) error {
	err := suite.luaEngine.LoadScript(script, "test")
	suite.NoError(err, "Should load subscription script with nio errors")
	err = suite.luaEngine.ExecuteScript(context.Background(), "")
	return err
}

func (suite *LuaEngineTestSuite) TestCapturePrintVariants() {
	cases := []struct {
		name     string
		script   string
		expected *regexp.Regexp
	}{
		{"no args", `print()`, regexp.MustCompile(`^\n$`)},
		{"one string", `print("hello")`, regexp.MustCompile(`^hello\n$`)},
		{"two strings", `print("foo", "bar")`, regexp.MustCompile(`^foo\tbar\n$`)},
		{"number", `print(123)`, regexp.MustCompile(`^123\n$`)},
		{"boolean true", `print(true)`, regexp.MustCompile(`^true\n$`)},
		{"boolean false", `print(false)`, regexp.MustCompile(`^false\n$`)},
		{"nil value", `print(nil)`, regexp.MustCompile(`^nil\n$`)},

		// Mixed types
		{"string + number", `print("val:", 42)`, regexp.MustCompile(`^val:\t42\n$`)},
		{"boolean + string", `print(false, "end")`, regexp.MustCompile(`^false\tend\n$`)},

		// Expressions
		{"addition", `print(1+2)`, regexp.MustCompile(`^3\n$`)},
		{"concat", `print("a" .. "b")`, regexp.MustCompile(`^ab\n$`)},
		{"concat mixed", `print("val=" .. 123)`, regexp.MustCompile(`^val=123\n$`)},

		// Tables
		{"empty table", `print({})`, regexp.MustCompile(`^table: 0x[0-9a-fA-F]+\n$`)},
		{"table with values", `print({x=1, y=2})`, regexp.MustCompile(`^table: 0x[0-9a-fA-F]+\n$`)},

		// Functions and userdata
		{"function ref", `print(function() end)`, regexp.MustCompile(`^function: 0x[0-9a-fA-F]+\n$`)},
		{"coroutine", `print(coroutine.create(function() end))`, regexp.MustCompile(`^thread: 0x[0-9a-fA-F]+\n$`)},

		// Multiple args
		{"string num bool nil", `print("s", 9, true, nil)`, regexp.MustCompile(`^s\t9\ttrue\tnil\n$`)},

		// Newline preservation
		{"string with newline", `print("a\nb")`, regexp.MustCompile(`^a\nb\n$`)},

		// Empty string and spaces
		{"empty string", `print("")`, regexp.MustCompile(`^\n$`)},
		{"whitespace string", `print("   ")`, regexp.MustCompile(`^   \n$`)},
	}

	for _, c := range cases {
		suite.Run(c.name, func() {
			err := suite.ExecuteScript(c.script)
			suite.NoError(err, "Lua code should execute")

			// Give the writer a brief moment to consume the channel
			time.Sleep(10 * time.Millisecond)

			// Get all captured output as a single string
			if got, err := suite.luaOutputCapture.ConsumePlainText(); err != nil {
				suite.NoError(fmt.Errorf("should be able to consume plain text: %v", err))
			} else {
				if !c.expected.MatchString(got) {
					suite.Failf("Output mismatch", "got %q, want match %q", got, c.expected.String())
				}
			}
		})
	}
}

// TestCaptureIOWriteVariants tests io.write() capture with various argument types
func (suite *LuaEngineTestSuite) TestCaptureIOWriteVariants() {
	cases := []struct {
		name     string
		script   string
		expected *regexp.Regexp
	}{
		// Basic io.write tests - NO automatic newline unlike print
		{"one string", `io.write("hello")`, regexp.MustCompile(`^hello$`)},
		{"two strings", `io.write("foo", "bar")`, regexp.MustCompile(`^foobar$`)},
		{"number", `io.write(123)`, regexp.MustCompile(`^123$`)},
		{"boolean true", `io.write(true)`, regexp.MustCompile(`^true$`)},
		{"boolean false", `io.write(false)`, regexp.MustCompile(`^false$`)},
		{"nil value", `io.write(nil)`, regexp.MustCompile(`^nil$`)},

		// Mixed types - concatenated without separator
		{"string + number", `io.write("val:", 42)`, regexp.MustCompile(`^val:42$`)},
		{"boolean + string", `io.write(false, "end")`, regexp.MustCompile(`^falseend$`)},

		// Expressions
		{"addition", `io.write(1+2)`, regexp.MustCompile(`^3$`)},
		{"concat", `io.write("a" .. "b")`, regexp.MustCompile(`^ab$`)},
		{"concat mixed", `io.write("val=" .. 123)`, regexp.MustCompile(`^val=123$`)},

		// Manual newlines
		{"with newline", `io.write("hello\n")`, regexp.MustCompile(`^hello\n$`)},
		{"multiple lines", `io.write("line1\nline2\n")`, regexp.MustCompile(`^line1\nline2\n$`)},

		// Empty string and spaces
		{"empty string", `io.write("")`, regexp.MustCompile(`^$`)},
		{"whitespace string", `io.write("   ")`, regexp.MustCompile(`^   $`)},

		// Multiple io.write calls
		{"multiple calls", `io.write("a"); io.write("b")`, regexp.MustCompile(`^ab$`)},
		{"multiple calls with newlines", `io.write("a\n"); io.write("b\n")`, regexp.MustCompile(`^a\nb\n$`)},
	}

	for _, c := range cases {
		suite.Run(c.name, func() {
			err := suite.ExecuteScript(c.script)
			suite.NoError(err, "Lua code should execute")

			// Give the writer a brief moment to consume the channel
			time.Sleep(10 * time.Millisecond)

			// Get all captured output as a single string
			if got, err := suite.luaOutputCapture.ConsumePlainText(); err != nil {
				suite.NoError(fmt.Errorf("should be able to consume plain text: %v", err))
			} else {
				if !c.expected.MatchString(got) {
					suite.Failf("Output mismatch", "got %q, want match %q", got, c.expected.String())
				}
			}
		})
	}
}

// TestCaptureMixedPrintAndIOWrite tests that print() and io.write() can be mixed
func (suite *LuaEngineTestSuite) TestCaptureMixedPrintAndIOWrite() {
	script := `
		io.write("line1")
		print("line2")
		io.write("line3\n")
	`
	err := suite.ExecuteScript(script)
	suite.NoError(err, "Lua code should execute")

	time.Sleep(10 * time.Millisecond)

	got, err := suite.luaOutputCapture.ConsumePlainText()
	suite.NoError(err, "should be able to consume plain text")

	// Expected: "line1" + "line2\n" + "line3\n"
	expected := "line1line2\nline3\n"
	suite.Equal(expected, got, "Mixed print and io.write should preserve order and newline behavior")
}

// TestJSONLibraryAvailability tests if the JSON library is properly loaded
func (suite *LuaEngineTestSuite) TestJSONLibraryAvailability() {
	suite.NotEmpty(jsonLua, "Embedded JSON library should not be empty")

	// Test JSON library availability
	jsonTestScript := `
		-- Try to require the module
		local json = require("json")
		test_obj = {test = "hello", number = 42}
		json_string = json.encode(test_obj)
		print(json_string)
	`
	err := suite.ExecuteScript(jsonTestScript)
	suite.NoError(err, "JSON library should be available and working")

	// Allow some time for processing
	time.Sleep(10 * time.Millisecond)

	// Get captured JSON output
	if output, err := suite.luaOutputCapture.ConsumePlainText(); err != nil {
		suite.NoError(fmt.Errorf("should be able to consume plain text: %v", err))
	} else {

		suite.NotEmpty(output, "Should have captured JSON output")

		// Parse the JSON output to verify it's valid
		var jsonData struct {
			Test   string `json:"test"`
			Number int    `json:"number"`
		}

		err = json.Unmarshal([]byte(strings.TrimSpace(output)), &jsonData)
		suite.NoError(err, "Should be able to parse JSON output: %s", output)

		suite.Equal("hello", jsonData.Test, "JSON should contain correct test field")
		suite.Equal(42, jsonData.Number, "JSON should contain correct number field")
	}
}

func (suite *LuaEngineTestSuite) TestExecuteScript2_ContextCancellation() {
	suite.Run("CancellationInfiniteLoop", func() {
		script := `
			local count = 0
			while true do
				count = count + 1
			end
		`

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		//err := suite.luaEngine.ExecuteScript2(ctx, script, nil)
		err := suite.luaEngine.ExecuteScript(ctx, script)
		suite.Error(err, "Should return error on cancellation")
		suite.True(context.Canceled == err, "Error should be context.Canceled")
	})

	suite.Run("CompletesBeforeTimeout", func() {
		script := `print("Quick execution")`

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		//err := suite.luaEngine.ExecuteScript2(ctx, script, nil)
		err := suite.luaEngine.ExecuteScript(ctx, script)
		suite.NoError(err, "Should complete successfully before timeout")
	})

	suite.Run("AlreadyCancelled", func() {
		script := `print("Should not run")`

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel before execution

		//err := suite.luaEngine.ExecuteScript2(ctx, script, nil)
		err := suite.luaEngine.ExecuteScript(ctx, script)
		suite.Error(err, "Should fail with already cancelled context")
		suite.True(context.Canceled == err, "Should return context.Canceled")
	})
}

func (suite *LuaEngineTestSuite) TestExecuteScript_BlockedFunctions() {
	blockedFunctions := map[string]string{
		"os.execute": `os.execute("echo test")`,
		"os.exit":    `os.exit(0)`,
		"os.remove":  `os.remove("file.txt")`,
		"os.rename":  `os.rename("old.txt", "new.txt")`,
		"io.read":    `io.read()`,
		"io.lines":   `io.lines("file.txt")`,
		//"require":    `require("module")`,
		"dofile":   `dofile("script.lua")`,
		"loadfile": `loadfile("script.lua")`,
	}

	for name, script := range blockedFunctions {
		suite.Run(name, func() {
			//err := suite.luaEngine.ExecuteScript2(context.Background(), script, nil)
			err := suite.luaEngine.ExecuteScript(context.Background(), script)
			suite.Error(err, "Should error on blocked function: "+name)
			suite.Contains(err.Error(), "is blocked", "Error should mention function is blocked")
		})
	}
}

// TestLuaEngineTestSuite runs the test suite using testify/suite
func TestLuaEngineTestSuite(t *testing.T) {
	suitelib.Run(t, new(LuaEngineTestSuite))
}
