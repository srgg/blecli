package lua

import (
	"errors"
	"fmt"
	"io/ioutil"
	"strings"
	"sync"
	"time"

	"github.com/aarzilli/golua/lua"
	"github.com/sirupsen/logrus"

	_ "embed"
)

//go:embed lua-libs/json.lua
var jsonLua string // json.lua is embedded into this string

// LuaOutputRecord represents a single output record from Lua script execution
type LuaOutputRecord struct {
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"` // "stdout" or "stderr"
}

// LuaError represents detailed Lua execution errors
type LuaError struct {
	Type       string // "syntax", "runtime", "api"
	Message    string
	Line       int
	Source     string
	StackTrace string
	Underlying error
}

func (e *LuaError) Error() string {
	parts := []string{}
	if e.Source != "" {
		parts = append(parts, fmt.Sprintf("in %s", e.Source))
	}
	if e.Line > 0 {
		parts = append(parts, fmt.Sprintf("line %d", e.Line))
	}

	prefix := "Lua error"
	if len(parts) > 0 {
		prefix = fmt.Sprintf("Lua %s error (%s)", e.Type, strings.Join(parts, ", "))
	}
	result := fmt.Sprintf("%s: %s", prefix, e.Message)
	if e.StackTrace != "" {
		result += "\n" + e.StackTrace
	}
	return result
}

func (e *LuaError) Unwrap() error {
	return e.Underlying
}

func (e *LuaError) Is(target error) bool {
	if target == nil {
		return false
	}
	var luaErr *LuaError
	if errors.As(target, &luaErr) {
		return e.Type == luaErr.Type
	}
	return false
}

// LuaEngine represents the Lua engine with full output capture
type LuaEngine struct {
	state      *lua.State
	stateMutex sync.Mutex
	logger     *logrus.Logger
	scriptCode string
	outputChan *RingChannel[LuaOutputRecord] // ring buffer for Lua outputs
}

// NewLuaEngine creates a new Lua engine with full stdout/stderr capture
func NewLuaEngine(logger *logrus.Logger) *LuaEngine {
	engine := &LuaEngine{
		logger:     logger,
		outputChan: NewRingChannel[LuaOutputRecord](100),
	}

	engine.Reset()

	logger.Info("LuaEngine2 initialized with full Lua output capture")
	return engine
}

func (e *LuaEngine) DoWithState(callback func(*lua.State) interface{}) interface{} {
	e.stateMutex.Lock()
	defer e.stateMutex.Unlock()

	if e.state == nil {
		return nil
	}
	return callback(e.state)
}

func (e *LuaEngine) doWithStateInternal(callback func(*lua.State) interface{}) interface{} {
	if e.state == nil {
		return nil
	}
	return callback(e.state)
}

func (e *LuaEngine) registerPrintCaptureInternal() {
	e.doWithStateInternal(func(L *lua.State) interface{} {
		// Override print
		L.PushGoFunction(func(L *lua.State) int {
			top := L.GetTop()
			parts := make([]string, 0, top)

			for i := 1; i <= top; i++ {
				if L.IsNil(i) {
					parts = append(parts, "nil")
				} else if L.IsBoolean(i) {
					if L.ToBoolean(i) {
						parts = append(parts, "true")
					} else {
						parts = append(parts, "false")
					}
				} else if L.IsNumber(i) {
					parts = append(parts, fmt.Sprintf("%v", L.ToNumber(i)))
				} else if L.IsString(i) {
					parts = append(parts, L.ToString(i))
				} else {
					// For tables, functions, threads, userdata: call Lua tostring()
					L.GetGlobal("tostring") // push global tostring
					L.PushValue(i)          // push value as argument
					L.Call(1, 1)            // call tostring(value)
					parts = append(parts, L.ToString(-1))
					L.Pop(1) // pop result
				}
			}

			// Join with tabs and append a newline
			line := strings.Join(parts, "\t") + "\n"

			// Send to RingChannel
			e.outputChan.ForceSend(LuaOutputRecord{
				Content:   line,
				Timestamp: time.Now(),
				Source:    "stdout",
			})

			return 0
		})

		L.SetGlobal("print")

		return nil
	})
}

// preloadJSONLibInternal loads the embedded JSON.lua library directly into the package.loaded["json"]
// This avoids the package.preload callback issues and follows the RegisterLibrary pattern.
// The embedded JSON library provides json.encode() and json.decode() functions to Lua scripts.
func (e *LuaEngine) preloadJSONLibInternal() {
	e.doWithStateInternal(func(L *lua.State) interface{} {
		// Load and execute the JSON Lua module directly
		if err := L.LoadString(jsonLua); err != 0 {
			e.logger.Error("Failed to load embedded json.lua")
			return nil
		}

		// Execute the chunk to get the JSON module table
		L.Call(0, 1) // runs chunk -> pushes module table

		// Put it directly into the package.loaded["json"] like RegisterLibrary does
		L.GetField(lua.LUA_GLOBALSINDEX, "package")
		L.GetField(-1, "loaded")
		L.PushValue(-3)        // Push the JSON module table
		L.SetField(-2, "json") // package.loaded["json"] = module
		L.Pop(2)               // Pop package and loaded
		return nil
	})
}

// OutputChannel returns the output channel
func (e *LuaEngine) OutputChannel() <-chan LuaOutputRecord {
	return e.outputChan.C()
}

// parseLuaError extracts detailed info from Lua error messages
func (e *LuaEngine) parseLuaError(errType, source string) *LuaError {
	if e.state.GetTop() == 0 {
		return &LuaError{Type: errType, Message: "unknown Lua error", Source: source}
	}

	errMsg := ""
	if e.state.IsString(-1) {
		errMsg = e.state.ToString(-1)
	} else {
		errMsg = "non-string error object"
	}
	e.state.Pop(1)

	line := 0
	message := errMsg
	if strings.Contains(errMsg, ":") {
		parts := strings.SplitN(errMsg, ":", 3)
		if len(parts) >= 3 {
			if parsed, err := fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &line); err == nil && parsed == 1 {
				message = strings.TrimSpace(parts[2])
			}
		}
	}

	return &LuaError{
		Type:    errType,
		Message: message,
		Line:    line,
		Source:  source,
	}
}

// Thread-safe execution
func (e *LuaEngine) safeExecuteScript(script string) error {
	var execErr error
	e.DoWithState(func(L *lua.State) interface{} {
		if err := L.DoString(script); err != nil {
			luaErr := e.parseLuaError("syntax", "")
			e.outputChan.ForceSend(LuaOutputRecord{
				Content:   fmt.Sprintf("Lua syntax error: %s", luaErr.Message),
				Timestamp: time.Now(),
				Source:    "stderr",
			})
			execErr = fmt.Errorf("lua execution failed: %w", err)
		}
		return nil
	})
	return execErr
}

// LoadScriptFile loads a Lua script from a file
func (e *LuaEngine) LoadScriptFile(filename string) error {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read script %s: %w", filename, err)
	}
	return e.LoadScript(string(content), filename)
}

// LoadScript loads a Lua script string and validates it
func (e *LuaEngine) LoadScript(script, name string) error {
	if script == "" {
		return &LuaError{Type: "api", Message: "empty script", Source: name}
	}

	e.scriptCode = script

	var loadErr error
	e.DoWithState(func(L *lua.State) interface{} {
		if status := L.LoadString(script); status != 0 {
			luaErr := e.parseLuaError("syntax", name)
			e.outputChan.Send(LuaOutputRecord{
				Content:   fmt.Sprintf("Lua syntax error: %s", luaErr.Message),
				Timestamp: time.Now(),
				Source:    "stderr",
			})
			L.Pop(1)
			loadErr = luaErr
			return nil
		}
		L.Pop(1)
		return nil
	})
	return loadErr
}

// ExecuteScript runs the loaded Lua script
func (e *LuaEngine) ExecuteScript(script string) error {
	if script != "" {
		e.LoadScript(script, "ad-hoc script")
	}
	if e.scriptCode == "" {
		return &LuaError{Type: "api", Message: "no script loaded"}
	}
	return e.safeExecuteScript(e.scriptCode)
}

func (e *LuaEngine) resetInternal() {
	if e.state != nil {
		e.state.Close()
	}

	e.state = lua.NewState()
	e.state.OpenLibs()

	e.registerPrintCaptureInternal()
	e.preloadJSONLibInternal()
}

// Reset recreates the Lua state
func (e *LuaEngine) Reset() {
	e.stateMutex.Lock()
	defer e.stateMutex.Unlock()
	e.resetInternal()
}

// Close cleans up the engine
func (e *LuaEngine) Close() {
	e.stateMutex.Lock()
	defer e.stateMutex.Unlock()

	if e.state != nil {
		e.state.Close()
		e.state = nil
	}
}

// ExecuteFunction executes a specific Lua function by name
func (e *LuaEngine) ExecuteFunction(functionName string) error {
	var funcErr error
	e.DoWithState(func(L *lua.State) interface{} {
		// Get the function from a global scope
		L.GetGlobal(functionName)
		if !L.IsFunction(-1) {
			L.Pop(1)
			funcErr = fmt.Errorf("function %s not found or not a function", functionName)
			return nil
		}

		// Call the function with no arguments
		if err := L.Call(0, 0); err != nil {
			funcErr = fmt.Errorf("failed to call function %s: %w", functionName, err)
		}
		return nil
	})

	if funcErr == nil && e.state == nil {
		return fmt.Errorf("lua state not initialized")
	}

	return funcErr
}

// SetGlobal sets a global variable in the Lua state
func (e *LuaEngine) SetGlobal(name string, value interface{}) error {
	res := e.DoWithState(func(state *lua.State) any {
		switch v := value.(type) {
		case string:
			state.PushString(v)
		case int:
			state.PushInteger(int64(v))
		case int64:
			state.PushInteger(v)
		case float64:
			state.PushNumber(v)
		case bool:
			state.PushBoolean(v)
		default:
			return fmt.Errorf("unsupported type for global variable %s", name)
		}

		state.SetGlobal(name)
		return nil
	})

	// Type assert the result as an error
	if err, ok := res.(error); ok {
		return err
	}
	return nil
}

// GetGlobal gets a global variable from the Lua state
func (e *LuaEngine) GetGlobal(name string) interface{} {
	return e.DoWithState(func(state *lua.State) any {
		state.GetGlobal(name)
		defer state.Pop(1)

		switch {
		case state.IsString(-1):
			return state.ToString(-1)
		case state.IsNumber(-1):
			return state.ToNumber(-1)
		case state.IsBoolean(-1):
			return state.ToBoolean(-1)
		default:
			return nil // unsupported type
		}
	})
}

// GetGlobalInteger gets an integer global variable from the Lua state
func (e *LuaEngine) GetGlobalInteger(name string) (int, error) {
	var result int
	var err error

	e.DoWithState(func(state *lua.State) interface{} {
		state.GetGlobal(name)
		defer state.Pop(1)

		if !state.IsNumber(-1) {
			err = fmt.Errorf("global variable %s is not a number", name)
			return nil
		}

		result = int(state.ToInteger(-1))
		return nil
	})

	return result, err
}

// GetTableValue gets a string value from a Lua table by key
func (e *LuaEngine) GetTableValue(tableName, key string) (string, error) {
	var result string
	var err error

	e.DoWithState(func(state *lua.State) interface{} {
		state.GetGlobal(tableName)
		if !state.IsTable(-1) {
			state.Pop(1)
			err = fmt.Errorf("global %s is not a table", tableName)
			return nil
		}

		state.PushString(key)
		state.GetTable(-2)
		defer state.Pop(2) // pop value and table

		switch {
		case state.IsString(-1):
			result = state.ToString(-1)
		case state.IsNil(-1):
			err = fmt.Errorf("key %s not found in table %s", key, tableName)
		default:
			err = fmt.Errorf("value for key %s in table %s is not a string", key, tableName)
		}

		return nil
	})

	return result, err
}

// GetGlobalString gets a string global variable from the Lua state
func (e *LuaEngine) GetGlobalString(name string) (string, error) {
	var result string
	var err error

	e.DoWithState(func(state *lua.State) interface{} {
		state.GetGlobal(name)
		defer state.Pop(1)

		if !state.IsString(-1) {
			err = fmt.Errorf("global variable %s is not a string", name)
			return nil
		}

		result = state.ToString(-1)
		return nil
	})

	return result, err
}
