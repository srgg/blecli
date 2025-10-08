package lua

import "C"

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"github.com/aarzilli/golua/lua"
	"github.com/sirupsen/logrus"
	"github.com/srg/blim/internal/device"
)

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

//func (api *BLEAPI2) ExecuteScript2(ctx context.Context, script string) error {
//	return api.LuaEngine.ExecuteScript2(ctx, script, func(L *lua.State) {
//		// Register API
//		// Create ble table
//		L.NewTable()
//
//		// Register API functions
//		api.registerSubscribeFunction(L)
//		api.registerListFunction(L)
//		api.registerDeviceInfo(L)
//		api.registerCharacteristicFunction(L)
//
//		// Set global 'ble' variable
//		L.SetGlobal("ble")
//	})
//}

func (api *BLEAPI2) LoadScriptFile(filename string) error {
	return api.LuaEngine.LoadScriptFile(filename)
}

func (api *BLEAPI2) LoadScript(script, name string) error {
	return api.LuaEngine.LoadScript(script, name)
}

func (api *BLEAPI2) Reset() {
	api.LuaEngine.Reset()
	api.registerLuaAPI()
}

func (api *BLEAPI2) OutputChannel() <-chan LuaOutputRecord {
	return api.LuaEngine.OutputChannel()
}

// registerLuaAPI registers the BLE API functions in the Lua state
func (api *BLEAPI2) registerLuaAPI() {
	api.LuaEngine.DoWithState(func(L *lua.State) interface{} {
		// Create ble table
		L.NewTable()

		// Register API functions
		api.registerSubscribeFunction(L)
		api.registerListFunction(L)
		api.registerDeviceInfo(L)
		api.registerCharacteristicFunction(L)

		// Set global 'ble' variable
		L.SetGlobal("ble")

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

		// Device Address
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

		// Field: service_uuid
		L.PushString("service")
		L.PushString(serviceUUID)
		L.SetTable(-3)

		// Field: properties (table with boolean flags for each property)
		L.PushString("properties")
		L.NewTable()

		// Parse properties string (e.g., "Read|Write|Notify") into boolean flags
		propsStr := char.GetProperties()
		if strings.Contains(propsStr, "Read") {
			L.PushString("read")
			L.PushBoolean(true)
			L.SetTable(-3)
		}
		if strings.Contains(propsStr, "Write") {
			L.PushString("write")
			L.PushBoolean(true)
			L.SetTable(-3)
		}
		if strings.Contains(propsStr, "Notify") {
			L.PushString("notify")
			L.PushBoolean(true)
			L.SetTable(-3)
		}
		if strings.Contains(propsStr, "Indicate") {
			L.PushString("indicate")
			L.PushBoolean(true)
			L.SetTable(-3)
		}

		L.SetTable(-3)

		// Field: descriptors (array)
		L.PushString("descriptors")
		L.NewTable()
		descriptors := char.GetDescriptors()
		for i, desc := range descriptors {
			L.PushInteger(int64(i + 1))
			L.PushString(desc.GetUUID())
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

// Close cleans up the API resources
func (api *BLEAPI2) Close() {
	if api.logger != nil {
		api.logger.WithField("lua_api_ptr", fmt.Sprintf("%p", api)).Debug("Closing BLEAPI2...")
	}
	api.LuaEngine.Close()
	if api.logger != nil {
		api.logger.WithField("lua_api_ptr", fmt.Sprintf("%p", api)).Debug("BLEAPI2 closed")
	}
}
