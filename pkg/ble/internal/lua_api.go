package internal

import (
	"fmt"
	"time"

	"github.com/aarzilli/golua/lua"
	"github.com/sirupsen/logrus"
	"github.com/srg/blecli/pkg/device"
)

// LuaApiFactory creates a Lua API instance (can be overridden in tests)
var LuaApiFactory = func(address string, logger *logrus.Logger) (*BLEAPI2, error) {
	dev := device.NewDeviceWithAddress(address, logger)
	return NewBLEAPI2(dev, logger), nil
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

func (api *BLEAPI2) ExecuteScript(script string) error {
	return api.LuaEngine.ExecuteScript(script)
}

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

		// Set global 'ble' variable
		L.SetGlobal("ble")

		return nil
	})
}

// registerSubscribeFunction registers the ble.subscribe() function
func (api *BLEAPI2) registerSubscribeFunction(L *lua.State) {
	L.PushString("subscribe")
	L.PushGoFunction(func(L *lua.State) int {
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
func (api *BLEAPI2) registerListFunction(L *lua.State) {
	connection := api.device.GetConnection()

	L.PushString("list")
	L.PushGoFunction(func(L *lua.State) int {
		services := connection.GetServices()
		L.NewTable()
		for uuid, service := range services {
			L.PushString(uuid)
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

			L.SetTable(-3)
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

		// Last Seen
		L.PushString("last_seen")
		L.PushString(dev.GetLastSeen().Format("2006-01-02T15:04:05Z07:00"))
		L.SetTable(-3)

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
func (api *BLEAPI2) callLuaCallback(callbackRef int, record *device.Record) {
	if callbackRef == lua.LUA_NOREF {
		return
	}

	api.LuaEngine.DoWithState(func(L *lua.State) interface{} {
		// Push the callback function onto the stack using reference
		L.RawGeti(lua.LUA_REGISTRYINDEX, callbackRef)

		// Create record table
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
		if err := L.Call(1, 0); err != nil {
			// Log the error for debugging
			api.logger.Errorf("Lua callback execution failed: %v", err)

			// Send error to output channel for user visibility
			api.LuaEngine.outputChan.ForceSend(LuaOutputRecord{
				Content:   fmt.Sprintf("Callback error: %v", err),
				Timestamp: time.Now(),
				Source:    "stderr",
			})
		}

		return nil
	})
}

// Close cleans up the API resources
func (api *BLEAPI2) Close() {
	api.LuaEngine.Close()
}
