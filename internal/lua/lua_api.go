package lua

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/aarzilli/golua/lua"
	"github.com/sirupsen/logrus"
	blim "github.com/srg/blim"
	"github.com/srg/blim/internal/device"
)

// BridgeInfo bridge information exposed to Lua
type BridgeInfo interface {
	GetTTYName() string    // TTY device name if created
	GetTTYSymlink() string // Symlink path (empty if not created)
	GetPTY() io.ReadWriter // PTY I/O as a standard Go interface (non-blocking reads /writes via ring buffers)
}

// LuaSubscriptionTable Lua subscription configuration
type LuaSubscriptionTable struct {
	Services    []device.SubscribeOptions `json:"services"`
	Mode        string                    `json:"mode"`
	MaxRate     int                       `json:"max_rate"`
	CallbackRef int                       `json:"-"` // Lua function reference
}

// BLEAPI2 represents the new BLE API that supports Lua subscriptions
// This replaces the old TTY-based bridge with direct subscription support
type BLEAPI2 struct {
	device    device.Device
	LuaEngine *LuaEngine
	logger    *logrus.Logger
	bridge    BridgeInfo // Optional bridge information
}

// NewBLEAPI2 creates a new BLE API instance with subscription support
func NewBLEAPI2(device device.Device, logger *logrus.Logger) *BLEAPI2 {
	r := &BLEAPI2{
		device:    device,
		logger:    logger,
		LuaEngine: NewLuaEngine(logger),
	}

	r.Reset()
	return r
}

func (api *BLEAPI2) GetDevice() device.Device {
	return api.device
}

// SetBridge sets the bridge information and updates the PTY strategy.
// When a bridge is set, the ptyio field is updated to use the bridge's PTY I/O strategy.
// When the bridge is nil, the ptyio field reverts to NilPTYIO.
func (api *BLEAPI2) SetBridge(bridge BridgeInfo) {
	api.logger.WithFields(logrus.Fields{
		"bridge_set": bridge != nil,
		"api_ptr":    fmt.Sprintf("%p", api),
	}).Debug("SetBridge called")
	api.bridge = bridge
}

// registerBridgeInfo registers the blim.bridge table with runtime bridge checking
// This is called internally during API registration within the DoWithState block
// Stack: expects _blim_internal table at top (-1)
func (api *BLEAPI2) registerBridgeInfo(L *lua.State) {
	L.PushString("bridge")

	// Create a bridge table with getter functions that check at runtime
	L.NewTable() // Stack: _blim_internal, "bridge", {}

	// Add tty_name as a getter function
	L.PushString("tty_name")
	L.PushGoFunction(api.LuaEngine.SafeWrapGoFunction("bridge.tty_name", func(L *lua.State) int {
		if api.bridge == nil {
			api.logger.WithField("api_ptr", fmt.Sprintf("%p", api)).Error("bridge.tty_name accessed but api.bridge is nil")
			L.RaiseError("bridge field 'tty_name' is not available (not running in bridge mode)")
			return 0
		}
		L.PushString(api.bridge.GetTTYName())
		return 1
	}))
	L.SetTable(-3)

	// Add symlink_path as a getter function
	L.PushString("tty_symlink")
	L.PushGoFunction(api.LuaEngine.SafeWrapGoFunction("bridge.tty_symlink", func(L *lua.State) int {
		if api.bridge == nil {
			L.RaiseError("bridge field 'tty_symlink' is not available (not running in bridge mode)")
			return 0
		}
		L.PushString(api.bridge.GetTTYSymlink())
		return 1
	}))
	L.SetTable(-3)

	// Add pty_write function - writes data to PTY via strategy pattern
	// Usage: blim.bridge.pty_write(data)
	// Returns: (bytes_written, nil) on success or (nil, error_message) on failure
	L.PushString("pty_write")
	L.PushGoFunction(api.LuaEngine.SafeWrapGoFunction("bridge.pty_write", func(L *lua.State) int {
		// Get PTY I/O strategy (minimal LuaPTY interface - no Close/TTYName exposed)
		ptyIO := api.bridge.GetPTY()

		// Validate argument - check actual type (IsString returns true for numbers too)
		if L.Type(1) != lua.LUA_TSTRING {
			L.PushNil()
			L.PushString("pty_write(data) expects a string argument")
			return 2
		}

		data := L.ToString(1)

		// DEBUG: Log the write attempt
		api.logger.Debugf("[pty_write] Writing %d bytes to PTY", len(data))

		// Write via strategy
		n, err := ptyIO.Write([]byte(data))
		if err != nil {
			api.logger.Warnf("[pty_write] Write failed: %v", err)
			L.PushNil()
			L.PushString(fmt.Sprintf("pty_write() failed: %v", err))
			return 2
		}

		// DEBUG: Log successful write
		api.logger.Debugf("[pty_write] Successfully wrote %d bytes", n)

		// Return (bytes_written, nil) on success
		L.PushInteger(int64(n))
		L.PushNil()
		return 2
	}))
	L.SetTable(-3)

	// Add pty_read function - reads buffered data via io.Reader interface
	// Usage: blim.bridge.pty_read([max_bytes])
	// Returns: (data, nil) on success, ("", nil) if no data available, or (nil, error_message) on failure
	// max_bytes defaults to 4096 if not specified
	L.PushString("pty_read")
	L.PushGoFunction(api.LuaEngine.SafeWrapGoFunction("bridge.pty_read", func(L *lua.State) int {
		// Get PTY I/O (io.ReadWriter interface)
		ptyIO := api.bridge.GetPTY()

		// Parse optional max_bytes argument (default: 4096)
		maxBytes := 4096
		if L.GetTop() >= 1 && L.IsNumber(1) {
			maxBytes = L.ToInteger(1)
			if maxBytes <= 0 {
				L.PushNil()
				L.PushString("pty_read(max_bytes) expects a positive integer")
				return 2
			}
		}

		// Allocate buffer for io.Reader
		buf := make([]byte, maxBytes)

		// Read via io.Reader (non-blocking, returns buffered data)
		n, err := ptyIO.Read(buf)
		if err != nil {
			// Handle expected non-blocking I/O errors
			if errors.Is(err, syscall.EAGAIN) || err == io.EOF {
				// No data available (EAGAIN) or EOF - return empty string, no error
				L.PushString("")
				L.PushNil()
				return 2
			}
			// Unexpected error - return error
			L.PushNil()
			L.PushString(fmt.Sprintf("pty_read() failed: %v", err))
			return 2
		}

		// Return (data, nil) on success
		L.PushString(string(buf[:n]))
		L.PushNil()
		return 2
	}))
	L.SetTable(-3)

	// Set _blim_internal.bridge = <table with getter functions>
	L.SetTable(-3)
}

// SafePushGoFunction pushes a function name and safe-wrapped Go function onto the Lua stack.
// The function will be automatically wrapped with panic recovery and error logging.
// After calling this, you typically call L.SetTable(-3) to add it to the parent table.
//
// Example:
//
//	api.SafePushGoFunction(L, "read", func(L *lua.State) int {
//	    // your implementation
//	})
//	L.SetTable(-3)
func (api *BLEAPI2) SafePushGoFunction(L *lua.State, name string, fn func(*lua.State) int) {
	L.PushString(name)
	L.PushGoFunction(api.LuaEngine.SafeWrapGoFunction(name+"()", fn))
}

// parseStreamPattern converts a string pattern to a device.StreamPattern
func parseStreamPattern(pattern string) device.StreamMode {
	switch pattern {
	case "EveryUpdate":
		return device.StreamEveryUpdate
	case "Batched":
		return device.StreamBatched
	case "Aggregated":
		return device.StreamAggregated
	default:
		return device.StreamEveryUpdate // Default fallback
	}
}

func (api *BLEAPI2) ExecuteScript(ctx context.Context, script string) error {
	return api.LuaEngine.ExecuteScript(ctx, script)
}

func (api *BLEAPI2) LoadScriptFile(filename string) error {
	return api.LuaEngine.LoadScriptFile(filename)
}

func (api *BLEAPI2) LoadScript(script, name string) error {
	return api.LuaEngine.LoadScript(script, name)
}

func (api *BLEAPI2) Reset() {
	api.LuaEngine.Reset()
	api.registerBlimAPI() // Register _blim_internal for Lua wrapper
}

func (api *BLEAPI2) OutputChannel() <-chan LuaOutputRecord {
	return api.LuaEngine.OutputChannel()
}

// registerBlimAPI registers the internal BLE API (_blim_internal) for Lua wrapper
// This demonstrates the CGO-like approach where Lua wraps Go functions
func (api *BLEAPI2) registerBlimAPI() {
	api.LuaEngine.DoWithState(func(L *lua.State) interface{} {
		// Create _blim_internal table
		L.NewTable()

		// Register API functions (same as ble)
		api.registerSubscribeFunction(L)
		api.registerListFunction(L)
		api.registerDeviceInfo(L)
		api.registerCharacteristicFunction(L)

		// Register utility functions
		api.registerSleepFunction(L)

		// Register bridge info if set
		api.registerBridgeInfo(L)

		// Set global '_blim_internal' variable
		L.SetGlobal("_blim_internal")

		// Preload the blim.lua scripts (creates global ble and blim)
		api.LuaEngine.PreloadLuaLibrary(blim.BlimLuaScript, "blim", "blim.lua")

		return nil
	})
}

// registerSubscribeFunction registers the ble.subscribe() function
func (api *BLEAPI2) registerSubscribeFunction(L *lua.State) {
	api.SafePushGoFunction(L, "subscribe", func(L *lua.State) int {
		// Expect a table as the first argument
		if !L.IsTable(1) {
			L.RaiseError("Error: subscribe() expects a lua table argument")
			return 0
		}

		// Parse the subscription table
		config, err := api.parseSubscriptionTable(L, 1)
		if err != nil {
			L.RaiseError("Error parsing subscription config: " + err.Error())
			return 0
		}

		// Execute the subscription
		err = api.executeSubscription(config)
		if err != nil {
			L.RaiseError("Error executing subscription: " + err.Error())
			return 0
		}

		return 0
	})
	L.SetTable(-3)
}

// registerListFunction registers the ble.list() function
//
// Returns a dual-purpose Lua table with both array and hash parts:
//
// In Lua, a table can have both an array part and a hash part at the same time:
//
// Array part (numeric indices):
//   - Keys: [1], [2], [3], etc.
//   - Accessed with ipairs() for ordered iteration
//   - Preserves insertion order
//
// Hash part (string/any keys):
//   - Keys: ["uuid"], ["name"], etc.
//   - Accessed with pairs() (order not guaranteed) or direct lookup table["uuid"]
//
// Example:
//
//	local t = {}
//	t[1] = "service1"           -- array part
//	t[2] = "service2"           -- array part
//	t["service1"] = {data=123}  -- hash part
//	t["service2"] = {data=456}  -- hash part
//
// For ble.list(), this allows:
//  1. Ordered iteration: for i, uuid in ipairs(services) do ... end
//  2. UUID-based lookup: services[uuid] to get service info
func (api *BLEAPI2) registerListFunction(L *lua.State) {
	api.SafePushGoFunction(L, "list", func(L *lua.State) int {
		// Get connection when function is called, not when registered
		connection := api.device.GetConnection()
		if connection == nil {
			L.NewTable() // Return empty table if no connection
			return 1
		}
		services := connection.GetServices()
		L.NewTable()

		// Add both indexed array (for ordered iteration) and keyed access (for lookup)
		arrayIndex := 1
		for _, service := range services {
			uuid := service.GetUUID()

			// Create service info table
			L.NewTable()

			// Add name field (only if known)
			if knownName := service.KnownName(); knownName != "" {
				L.PushString("name")
				L.PushString(knownName)
				L.SetTable(-3)
			}

			// Add characteristics array
			L.PushString("characteristics")
			L.NewTable()
			charIndex := 1
			for _, c := range service.GetCharacteristics() {
				L.PushInteger(int64(charIndex))
				L.PushString(c.GetUUID())
				L.SetTable(-3)
				charIndex++
			}
			L.SetTable(-3)

			// Stack: [main_table, service_info]
			// Store service info with UUID key (for lookup: table["uuid"])
			L.PushString(uuid) // Stack: [main_table, service_info, uuid]
			L.PushValue(-2)    // Stack: [main_table, service_info, uuid, service_info]
			L.SetTable(-4)     // main_table[uuid] = service_info; Stack: [main_table, service_info]

			// Store UUID in array part (for iteration: ipairs(table))
			L.PushInteger(int64(arrayIndex)) // Stack: [main_table, service_info, arrayIndex]
			L.PushString(uuid)               // Stack: [main_table, service_info, arrayIndex, uuid]
			L.SetTable(-4)                   // main_table[arrayIndex] = uuid; Stack: [main_table, service_info]

			// Pop the service info table
			L.Pop(1)

			arrayIndex++
		}
		return 1
	})
	L.SetTable(-3)
}

// registerDeviceInfo registers the ble.device table with device information
func (api *BLEAPI2) registerDeviceInfo(L *lua.State) {
	dev := api.device

	L.PushString("device")
	L.NewTable()

	if dev != nil {
		// Device ID
		L.PushString("id")
		L.PushString(dev.GetID())
		L.SetTable(-3)

		// Device BleAddress
		L.PushString("address")
		L.PushString(dev.GetAddress())
		L.SetTable(-3)

		// Device Name
		L.PushString("name")
		L.PushString(dev.GetName())
		L.SetTable(-3)

		// RSSI
		L.PushString("rssi")
		L.PushInteger(int64(dev.GetRSSI()))
		L.SetTable(-3)

		// Connectable
		L.PushString("connectable")
		L.PushBoolean(dev.IsConnectable())
		L.SetTable(-3)

		// TX Power (optional)
		if txPower := dev.GetTxPower(); txPower != nil {
			L.PushString("tx_power")
			L.PushInteger(int64(*txPower))
			L.SetTable(-3)
		}

		// Advertised Services
		L.PushString("advertised_services")
		L.NewTable()
		uuids := dev.GetAdvertisedServices()
		for i, uuid := range uuids {
			L.PushInteger(int64(i + 1))
			L.PushString(uuid)
			L.SetTable(-3)
		}
		L.SetTable(-3)

		// Manufacturer Data
		manufData := dev.GetManufacturerData()
		L.PushString("manufacturer_data")
		if len(manufData) > 0 {
			L.PushString(fmt.Sprintf("%X", manufData))
		} else {
			L.PushString("")
		}
		L.SetTable(-3)

		// Service Data
		L.PushString("service_data")
		L.NewTable()
		serviceData := dev.GetServiceData()
		for uuid, data := range serviceData {
			L.PushString(uuid)
			L.PushString(fmt.Sprintf("%X", data))
			L.SetTable(-3)
		}
		L.SetTable(-3)
	}

	L.SetTable(-3) // Set device subtable in ble table
}

// parseSubscriptionTable parses the Lua table into a LuaSubscriptionTable
func (api *BLEAPI2) parseSubscriptionTable(L *lua.State, tableIndex int) (*LuaSubscriptionTable, error) {
	config := &LuaSubscriptionTable{}

	// Convert relative index to absolute index
	if tableIndex < 0 {
		tableIndex = L.GetTop() + tableIndex + 1
	}

	// Parse services array
	L.PushString("services")
	L.GetTable(tableIndex)
	if L.IsTable(-1) {
		services, err := api.parseServicesArray(L, -1)
		if err != nil {
			L.Pop(1)
			return nil, err
		}
		config.Services = services
	}
	L.Pop(1)

	// Parse Mode
	L.PushString("Mode")
	L.GetTable(tableIndex)
	if L.IsString(-1) {
		config.Mode = L.ToString(-1)
	} else {
		config.Mode = "EveryUpdate" // Default
	}
	L.Pop(1)

	// Parse MaxRate
	L.PushString("MaxRate")
	L.GetTable(tableIndex)
	if L.IsNumber(-1) {
		config.MaxRate = L.ToInteger(-1)
	} else {
		config.MaxRate = 0 // Default
	}
	L.Pop(1)

	// Parse Callback function
	L.PushString("Callback")
	L.GetTable(tableIndex)
	if L.IsFunction(-1) {
		// Store reference to the function
		config.CallbackRef = L.Ref(lua.LUA_REGISTRYINDEX)
	} else {
		L.Pop(1) // Pop non-function value
	}

	return config, nil
}

// parseServicesArray parses the service array from the Lua table
func (api *BLEAPI2) parseServicesArray(L *lua.State, tableIndex int) ([]device.SubscribeOptions, error) {
	var services []device.SubscribeOptions

	// Convert relative index to absolute index for L.Next()
	if tableIndex < 0 {
		tableIndex = L.GetTop() + tableIndex + 1
	}

	// Iterate through the service array
	L.PushNil()
	for L.Next(tableIndex) != 0 {
		if L.IsTable(-1) {
			service := device.SubscribeOptions{}

			// Parse service UUID
			L.PushString("service")
			L.GetTable(-2)
			if L.IsString(-1) {
				service.Service = L.ToString(-1)
			}
			L.Pop(1)

			// Parse chars array
			L.PushString("chars")
			L.GetTable(-2)
			if L.IsTable(-1) {
				// Convert relative index to absolute index
				charsIndex := L.GetTop()
				chars := api.parseCharsArray(L, charsIndex)
				service.Characteristics = chars
			}
			L.Pop(1)

			services = append(services, service)
		}
		L.Pop(1) // Pop value, keep key for next iteration
	}

	return services, nil
}

// parseCharsArray parses the characteristic array from a service
func (api *BLEAPI2) parseCharsArray(L *lua.State, tableIndex int) []string {
	var chars []string

	// Convert relative index to absolute index for L.Next()
	if tableIndex < 0 {
		tableIndex = L.GetTop() + tableIndex + 1
	}

	L.PushNil()
	for L.Next(tableIndex) != 0 {
		if L.IsString(-1) {
			chars = append(chars, L.ToString(-1))
		}
		L.Pop(1) // Pop value, keep key for next iteration
	}

	return chars
}

// executeSubscription creates and starts the actual BLE subscription
func (api *BLEAPI2) executeSubscription(config *LuaSubscriptionTable) error {
	api.logger.WithFields(logrus.Fields{
		"services": len(config.Services),
		"mode":     config.Mode,
	}).Debug("executeSubscription called")

	// Convert SubscriptionConfig to device.SubscribeOptions
	var opts []*device.SubscribeOptions
	for _, serviceConfig := range config.Services {
		opt := &device.SubscribeOptions{
			Service:         serviceConfig.Service,
			Characteristics: serviceConfig.Characteristics,
		}
		opts = append(opts, opt)
	}

	// Parse mode and max rate
	pattern := parseStreamPattern(config.Mode)
	maxRate := time.Duration(config.MaxRate) * time.Millisecond

	// Create a callback that calls the Lua function (nil if no callback provided)
	var callback func(*device.Record)
	if config.CallbackRef != 0 {
		callback = func(record *device.Record) {
			api.callLuaCallback(config.CallbackRef, record)
		}
	}

	// Call Subscribe on the connection
	return api.device.GetConnection().Subscribe(opts, pattern, maxRate, callback)
}

// callLuaCallback calls the Lua callback function with the record data
func (api *BLEAPI2) callLuaCallback(callbackRef int, record *device.Record) error {
	if callbackRef == lua.LUA_NOREF {
		return nil
	}

	// Outer panic handler: catches ALL panics (including LuaError from StackTrace crashes)
	// This ensures one callback's error doesn't crash other subscriptions
	defer func() {
		if r := recover(); r != nil {
			// Log ALL panics (LuaError or otherwise) and recover gracefully
			stack := string(debug.Stack())
			api.logger.Errorf("Lua callback panic (recovered): %v\nStack:\n%s", r, stack)

			// Send error to stderr for user visibility
			api.LuaEngine.outputChan.ForceSend(LuaOutputRecord{
				Content:   fmt.Sprintf("Callback error: %v", r),
				Timestamp: time.Now(),
				Source:    "stderr",
			})

			// Clean up Lua state after panic
			api.LuaEngine.DoWithState(func(L *lua.State) interface{} {
				L.SetTop(0) // Reset stack to clean state
				return nil
			})

			// DO NOT re-panic - allow other subscriptions to continue
		}
	}()

	api.LuaEngine.DoWithState(func(L *lua.State) interface{} {
		// Inner panic handler: catches panics from L.Call() (including StackTrace crashes)
		defer func() {
			if r := recover(); r != nil {
				// Re-panic to outer handler for cleanup
				panic(r)
			}
		}()

		// Push the callback function onto the stack using reference
		L.RawGeti(lua.LUA_REGISTRYINDEX, callbackRef)

		// Create a record table
		L.NewTable()

		// Set TsUs
		L.PushString("TsUs")
		L.PushInteger(record.TsUs)
		L.SetTable(-3)

		// Set Seq
		L.PushString("Seq")
		L.PushInteger(int64(record.Seq))
		L.SetTable(-3)

		// Set Flags
		L.PushString("Flags")
		L.PushInteger(int64(record.Flags))
		L.SetTable(-3)

		// Set Values table (for EveryUpdate/Aggregated modes)
		if record.Values != nil {
			L.PushString("Values")
			L.NewTable()
			for uuid, data := range record.Values {
				L.PushString(uuid)
				// Note: Converting []byte to string may cause issues with binary data,
				// but using byte arrays (60x more operations) kills performance for high-frequency BLE data.
				// Lua can still manipulate bytes using string.byte() on the result.
				L.PushString(string(data))
				L.SetTable(-3)
			}
			L.SetTable(-3)
		}

		// Set BatchValues table (for Batched mode)
		if record.BatchValues != nil {
			L.PushString("BatchValues")
			L.NewTable()
			for uuid, dataArray := range record.BatchValues {
				L.PushString(uuid)
				L.NewTable()
				for i, data := range dataArray {
					L.PushInteger(int64(i + 1)) // Lua arrays are 1-indexed
					// Note: Converting []byte to string may cause issues with binary data,
					// but using byte arrays (60x more operations) kills performance for high-frequency BLE data.
					// Lua can still manipulate bytes using string.byte() on the result.
					L.PushString(string(data))
					L.SetTable(-3)
				}
				L.SetTable(-3)
			}
			L.SetTable(-3)
		}

		// Call the function with 1 argument (the record table)
		// This can panic if StackTrace() crashes while building LuaError
		if err := L.Call(1, 0); err != nil {
			// Log the error for debugging
			api.logger.Errorf("Lua callback execution failed: %v", err)

			// Send error to an output channel for user visibility
			api.LuaEngine.outputChan.ForceSend(LuaOutputRecord{
				Content:   fmt.Sprintf("Callback error: %v", err),
				Timestamp: time.Now(),
				Source:    "stderr",
			})

			// CRITICAL: Reset Lua stack after error to prevent corruption
			// When L.Call() fails, the stack may be left in an inconsistent state
			// Resetting ensures the next callback starts with a clean stack
			L.SetTop(0)
		}

		return nil
	})

	return nil
}

// registerCharacteristicFunction registers the ble.characteristic() function
func (api *BLEAPI2) registerCharacteristicFunction(L *lua.State) {
	api.SafePushGoFunction(L, "characteristic", func(L *lua.State) int {
		// Validate arguments
		if !L.IsString(1) || !L.IsString(2) {
			L.RaiseError("characteristic(service_uuid, char_uuid) expects two string arguments")
			return 0
		}

		serviceUUID := L.ToString(1)
		charUUID := L.ToString(2)

		// Get connection when function is called, not when registered
		connection := api.device.GetConnection()
		if connection == nil {
			L.RaiseError("no connection available")
			return 0
		}

		// Get characteristic from connection
		char, err := connection.GetCharacteristic(serviceUUID, charUUID)
		if err != nil {
			L.RaiseError(fmt.Sprintf("characteristic not found: %v", err))
			return 0
		}

		// Create handle table with metadata fields
		L.NewTable()

		// Field: uuid
		L.PushString("uuid")
		L.PushString(char.GetUUID())
		L.SetTable(-3)

		// Field: name (only if known)
		if knownName := char.KnownName(); knownName != "" {
			L.PushString("name")
			L.PushString(knownName)
			L.SetTable(-3)
		}

		// Field: service_uuid
		L.PushString("service")
		L.PushString(serviceUUID)
		L.SetTable(-3)

		// Field: properties (dual-purpose table with array and hash parts)
		// - Array part: ordered iteration with ipairs() in bit order
		// - Hash part: boolean checks (if properties.read then)
		L.PushString("properties")
		L.NewTable()

		// Convert Properties struct to Lua table
		props := char.GetProperties()

		// Helper function to add a property to both array and hash parts
		arrayIndex := 1
		addProp := func(prop device.Property, key string) {
			if prop != nil {
				// Create property sub-table
				L.NewTable()
				L.PushString("value")
				L.PushInteger(int64(prop.Value()))
				L.SetTable(-3)
				L.PushString("name")
				L.PushString(prop.KnownName())
				L.SetTable(-3)

				// Add to hash part (for named access: properties.read)
				L.PushString(key) // Stack: [props_table, prop_table, key]
				L.PushValue(-2)   // Stack: [props_table, prop_table, key, prop_table]
				L.SetTable(-4)    // props_table[key] = prop_table; Stack: [props_table, prop_table]

				// Add to array part (for ordered iteration: ipairs(properties))
				L.PushInteger(int64(arrayIndex)) // Stack: [props_table, prop_table, arrayIndex]
				L.PushValue(-2)                  // Stack: [props_table, prop_table, arrayIndex, prop_table]
				L.SetTable(-4)                   // props_table[arrayIndex] = prop_table; Stack: [props_table, prop_table]

				// Pop the property table
				L.Pop(1)
				arrayIndex++
			}
		}

		// Add properties in bit order (0x01, 0x02, 0x04, 0x08, 0x10, 0x20, 0x40, 0x80)
		addProp(props.Broadcast(), "broadcast")
		addProp(props.Read(), "read")
		addProp(props.WriteWithoutResponse(), "write_without_response")
		addProp(props.Write(), "write")
		addProp(props.Notify(), "notify")
		addProp(props.Indicate(), "indicate")
		addProp(props.AuthenticatedSignedWrites(), "authenticated_signed_writes")
		addProp(props.ExtendedProperties(), "extended_properties")

		L.SetTable(-3)

		// Field: descriptors (array of objects with uuid and name)
		L.PushString("descriptors")
		L.NewTable()
		descriptors := char.GetDescriptors()
		for i, desc := range descriptors {
			L.PushInteger(int64(i + 1))
			// Create descriptor object
			L.NewTable()
			// Add uuid
			L.PushString("uuid")
			L.PushString(desc.GetUUID())
			L.SetTable(-3)
			// Add name (only if known)
			if knownName := desc.KnownName(); knownName != "" {
				L.PushString("name")
				L.PushString(knownName)
				L.SetTable(-3)
			}
			L.SetTable(-3)
		}
		L.SetTable(-3)

		// Method: read() - reads the characteristic value from the device
		// Returns (value, nil) on success or (nil, error_message) on failure
		api.SafePushGoFunction(L, "read", func(L *lua.State) int {
			value, err := char.Read()
			if err != nil {
				// Return (nil, error_message) for expected errors
				L.PushNil()
				L.PushString(fmt.Sprintf("read() failed: %v", err))
				return 2
			}
			// Return (value, nil) on success
			L.PushString(string(value))
			L.PushNil()
			return 2
		})
		L.SetTable(-3)

		// TODO: Add methods (write, subscribe, unsubscribe) if needed

		return 1
	})
	L.SetTable(-3)
}

// registerSleepFunction registers the blim.sleep() utility function
// Usage: blim.sleep(milliseconds)
// Sleeps for the specified number of milliseconds
func (api *BLEAPI2) registerSleepFunction(L *lua.State) {
	api.SafePushGoFunction(L, "sleep", func(L *lua.State) int {
		// Validate argument
		if !L.IsNumber(1) {
			L.RaiseError("sleep(milliseconds) expects a number argument")
			return 0
		}

		ms := L.ToInteger(1)
		if ms < 0 {
			L.RaiseError("sleep(milliseconds) expects a non-negative number")
			return 0
		}

		// Sleep for the specified duration
		time.Sleep(time.Duration(ms) * time.Millisecond)

		return 0
	})
	L.SetTable(-3)
}

// Close cleans up the API resources
func (api *BLEAPI2) Close() {
	if api.logger != nil {
		api.logger.WithField("lua_api_ptr", fmt.Sprintf("%p", api)).Debug("Closing lua api...")
	}
	api.LuaEngine.Close()
	if api.logger != nil {
		api.logger.WithField("lua_api_ptr", fmt.Sprintf("%p", api)).Debug("Lua api closed")
	}
}
