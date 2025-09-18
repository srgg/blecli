package internal

import (
	"fmt"
	"sync"

	"github.com/aarzilli/golua/lua"
	"github.com/sirupsen/logrus"
)

// EngineError represents an error from the Lua engine
type EngineError struct {
	Fatal   bool
	Message string
	Script  string
}

func (e *EngineError) Error() string {
	severity := "non-fatal"
	if e.Fatal {
		severity = "fatal"
	}
	return fmt.Sprintf("%s error in %s: %s", severity, e.Script, e.Message)
}

// LuaEngine represents the Lua transformation engine
type LuaEngine struct {
	state          *lua.State
	bleToTTYScript string
	ttyToBLEScript string
	bleAPI         *BLEAPI
	ttyBuffer      *Buffer
	bleBuffer      *Buffer
	logger         *logrus.Logger
	mutex          sync.Mutex
	apisRegistered bool
}

// NewLuaEngine creates a new Lua transformation engine
func NewLuaEngine(logger *logrus.Logger) *LuaEngine {
	if logger == nil {
		logger = logrus.New()
	}

	engine := &LuaEngine{
		state:     lua.NewState(),
		bleAPI:    NewBLEAPI(""), // Mode will be set per transformation
		ttyBuffer: NewBuffer("tty"),
		bleBuffer: NewBuffer("ble"),
		logger:    logger,
	}

	// Initialize Lua state with standard libraries
	engine.state.OpenLibs()

	return engine
}

// SetScripts sets the Lua scripts for BLE→TTY and TTY→BLE transformations
func (e *LuaEngine) SetScripts(bleToTTY, ttyToBLE string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	e.bleToTTYScript = bleToTTY
	e.ttyToBLEScript = ttyToBLE
}

// LoadScriptFile loads a Lua script file containing ble_to_tty() and tty_to_ble() functions
func (e *LuaEngine) LoadScriptFile(filename string) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if ret := e.state.LoadFile(filename); ret != 0 {
		return fmt.Errorf("failed to load script file %s: %v", filename, e.state.ToString(-1))
	}

	// Execute the script to define functions
	if err := e.state.Call(0, 0); err != nil {
		return fmt.Errorf("failed to execute script file %s: %v", filename, err)
	}

	return nil
}

// LoadScript loads and compiles a Lua script string
func (e *LuaEngine) LoadScript(script, name string) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if ret := e.state.LoadString(script); ret != 0 {
		return fmt.Errorf("failed to load %s script: %v", name, e.state.ToString(-1))
	}

	// Execute the script to define functions
	if err := e.state.Call(0, 0); err != nil {
		return fmt.Errorf("failed to execute %s script: %v", name, err)
	}

	return nil
}

// TransformBLEToTTY transforms data from BLE characteristics to TTY buffer
func (e *LuaEngine) TransformBLEToTTY() error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	// Set API mode for BLE→TTY (read-only BLE access)
	e.bleAPI.mode = "ble_to_tty"

	// Register APIs only if not already registered
	e.ensureAPIsRegistered()

	// Call ble_to_tty() function
	return e.callLuaFunction("ble_to_tty", "BLE→TTY")
}

// TransformTTYToBLE transforms data from TTY buffer to BLE characteristics
func (e *LuaEngine) TransformTTYToBLE() error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	// Set API mode for TTY→BLE (write-only BLE .value access)
	e.bleAPI.mode = "tty_to_ble"

	// Register APIs only if not already registered
	e.ensureAPIsRegistered()

	// Call tty_to_ble() function
	return e.callLuaFunction("tty_to_ble", "TTY→BLE")
}

// ensureAPIsRegistered registers the APIs once if not already registered
func (e *LuaEngine) ensureAPIsRegistered() {
	if !e.apisRegistered {
		// Debug: check BLE API state before registration
		charCount := len(e.bleAPI.characteristics)
		e.logger.Debugf("Registering BLE API with %d characteristics in mode '%s'", charCount, e.bleAPI.mode)

		// Register BLE API FIRST
		RegisterBLEAPI(e.state, e.bleAPI)

		// Register buffer API SECOND - it will use the appropriate buffer based on transformation direction
		// For BLE→TTY, we use ttyBuffer (output to TTY)
		// For TTY→BLE, we use bleBuffer (input from TTY)
		if e.bleAPI.mode == "ble_to_tty" {
			RegisterBufferAPI(e.state, e.ttyBuffer)
		} else {
			RegisterBufferAPI(e.state, e.bleBuffer)
		}

		e.apisRegistered = true
	}
}

// callLuaFunction calls a Lua function using pcall for error handling
func (e *LuaEngine) callLuaFunction(functionName, scriptName string) error {
	// Check if function exists
	e.state.GetGlobal(functionName)
	if e.state.IsNil(-1) {
		e.state.Pop(1) // Clean up nil function
		return &EngineError{
			Fatal:   true,
			Message: fmt.Sprintf("function '%s' not found", functionName),
			Script:  scriptName,
		}
	}

	// Call function with 2 expected return values to handle error conditions
	if err := e.state.Call(0, 2); err != nil {
		return &EngineError{
			Fatal:   true,
			Message: fmt.Sprintf("function call failed: %v", err),
			Script:  scriptName,
		}
	}

	// Check return values for non-fatal error conditions
	// Expected return pattern: return nil, "error message" for non-fatal errors
	// Expected return pattern: return data, nil for success
	if e.state.GetTop() >= 2 {
		// Get the second return value (error message)
		if e.state.IsString(-1) {
			errorMsg := e.state.ToString(-1)
			e.state.Pop(2) // Clean up return values
			if errorMsg != "" {
				return &EngineError{
					Fatal:   false,
					Message: errorMsg,
					Script:  scriptName,
				}
			}
		}
		e.state.Pop(2) // Clean up return values
	}

	return nil
}

// executeScriptSafely executes a script with pcall for error handling
func (e *LuaEngine) executeScriptSafely(script, scriptName string) error {
	// Push pcall function
	e.state.GetGlobal("pcall")

	// Load and push the script function
	if ret := e.state.LoadString(script); ret != 0 {
		return &EngineError{
			Fatal:   true,
			Message: fmt.Sprintf("script compilation failed: %v", e.state.ToString(-1)),
			Script:  scriptName,
		}
	}

	// Call pcall(script_function)
	if err := e.state.Call(2, 2); err != nil {
		return &EngineError{
			Fatal:   true,
			Message: fmt.Sprintf("pcall failed: %v", err),
			Script:  scriptName,
		}
	}

	// Check pcall result
	success := e.state.ToBoolean(-2)
	if !success {
		errorMsg := e.state.ToString(-1)
		e.state.Pop(2) // Clean up stack

		// Distinguish between fatal and non-fatal errors
		// Fatal errors are those thrown with error() or assert()
		// Non-fatal errors are those returned as nil, "message"
		if e.isNonFatalError(errorMsg) {
			e.logger.WithField("script", scriptName).Warn("Non-fatal error: ", errorMsg)
			return &EngineError{
				Fatal:   false,
				Message: errorMsg,
				Script:  scriptName,
			}
		}

		return &EngineError{
			Fatal:   true,
			Message: errorMsg,
			Script:  scriptName,
		}
	}

	e.state.Pop(2) // Clean up stack
	return nil
}

// isNonFatalError determines if an error is non-fatal based on the error message
func (e *LuaEngine) isNonFatalError(errorMsg string) bool {
	// Check if the error looks like a non-fatal return value
	// Non-fatal errors typically contain phrases like "waiting for", "incomplete", etc.
	nonFatalPatterns := []string{
		"waiting for",
		"incomplete",
		"not enough data",
		"need more",
		"partial",
	}

	for _, pattern := range nonFatalPatterns {
		if contains(errorMsg, pattern) {
			return true
		}
	}

	return false
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			(len(s) > len(substr) &&
				(s[:len(substr)] == substr ||
					s[len(s)-len(substr):] == substr ||
					indexContains(s, substr) >= 0)))
}

func indexContains(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// GetTTYBuffer returns the TTY buffer for external access
func (e *LuaEngine) GetTTYBuffer() *Buffer {
	return e.ttyBuffer
}

// GetBLEBuffer returns the BLE buffer for external access
func (e *LuaEngine) GetBLEBuffer() *Buffer {
	return e.bleBuffer
}

// GetBLEAPI returns the BLE API for external access
func (e *LuaEngine) GetBLEAPI() *BLEAPI {
	return e.bleAPI
}

// Close closes the Lua engine and cleans up resources
func (e *LuaEngine) Close() {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if e.state != nil {
		e.state.Close()
		e.state = nil
	}
}

// Reset clears all buffers and resets the engine state
func (e *LuaEngine) Reset() {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	e.ttyBuffer.Clear()
	e.bleBuffer.Clear()

	// Recreate Lua state
	if e.state != nil {
		e.state.Close()
	}
	e.state = lua.NewState()
	e.state.OpenLibs()

	// Reset API registration flag
	e.apisRegistered = false
}
