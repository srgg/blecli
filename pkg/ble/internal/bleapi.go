package internal

import (
	"sync"

	"github.com/aarzilli/golua/lua"
	"github.com/go-ble/ble"
)

// BLECharacteristic represents a BLE characteristic with its metadata
type BLECharacteristic struct {
	UUID        string
	Value       []byte
	Descriptors map[string][]byte
	Properties  map[string]bool
	mutex       sync.RWMutex
}

// BLEAPI represents the BLE API exposed to Lua scripts
// Implements the BLE API from the design document:
// ble:list()                  -- returns an array of all available characteristic UUIDs
// ble[uuid]                   -- read value only (read-only in BLE→TTY)
// ble[uuid].value             -- read or write value (write only in TTY→BLE)
// ble[uuid].descriptors       -- map of descriptor UUID -> value (read-only)
// ble[uuid].properties        -- map of property -> boolean (read-only)
type BLEAPI struct {
	characteristics map[string]*BLECharacteristic
	mode            string // "ble_to_tty" or "tty_to_ble"
	mutex           sync.RWMutex
}

// NewBLEAPI creates a new BLE API instance
func NewBLEAPI(mode string) *BLEAPI {
	return &BLEAPI{
		characteristics: make(map[string]*BLECharacteristic),
		mode:            mode,
	}
}

// AddCharacteristic adds a characteristic to the BLE API
func (api *BLEAPI) AddCharacteristic(char *ble.Characteristic) {
	api.mutex.Lock()
	defer api.mutex.Unlock()

	uuid := char.UUID.String()

	// Extract properties
	properties := make(map[string]bool)
	properties["read"] = (char.Property & ble.CharRead) != 0
	properties["write"] = (char.Property & ble.CharWrite) != 0
	properties["write_without_response"] = (char.Property & ble.CharWriteNR) != 0
	properties["notify"] = (char.Property & ble.CharNotify) != 0
	properties["indicate"] = (char.Property & ble.CharIndicate) != 0

	// Extract descriptors
	descriptors := make(map[string][]byte)
	for _, desc := range char.Descriptors {
		descriptors[desc.UUID.String()] = nil // Will be populated when read
	}

	api.characteristics[uuid] = &BLECharacteristic{
		UUID:        uuid,
		Value:       nil, // Will be populated when read
		Descriptors: descriptors,
		Properties:  properties,
	}
}

// UpdateCharacteristicValue updates the value of a characteristic
func (api *BLEAPI) UpdateCharacteristicValue(uuid string, value []byte) {
	api.mutex.Lock()
	defer api.mutex.Unlock()

	if char, exists := api.characteristics[uuid]; exists {
		char.mutex.Lock()
		char.Value = make([]byte, len(value))
		copy(char.Value, value)
		char.mutex.Unlock()
	}
}

// GetCharacteristicValue gets the value of a characteristic
func (api *BLEAPI) GetCharacteristicValue(uuid string) []byte {
	api.mutex.RLock()
	defer api.mutex.RUnlock()

	if char, exists := api.characteristics[uuid]; exists {
		char.mutex.RLock()
		defer char.mutex.RUnlock()

		result := make([]byte, len(char.Value))
		copy(result, char.Value)
		return result
	}
	return nil
}

// SetCharacteristicValue sets the value of a characteristic (for TTY→BLE mode)
func (api *BLEAPI) SetCharacteristicValue(uuid string, value []byte) {
	api.mutex.RLock()
	defer api.mutex.RUnlock()

	if char, exists := api.characteristics[uuid]; exists {
		char.mutex.Lock()
		char.Value = make([]byte, len(value))
		copy(char.Value, value)
		char.mutex.Unlock()
	}
}

// ListCharacteristics returns all characteristic UUIDs
func (api *BLEAPI) ListCharacteristics() []string {
	api.mutex.RLock()
	defer api.mutex.RUnlock()

	uuids := make([]string, 0, len(api.characteristics))
	for uuid := range api.characteristics {
		uuids = append(uuids, uuid)
	}
	return uuids
}

// RegisterBLEAPI registers the BLE API functions in the Lua state
func RegisterBLEAPI(L *lua.State, api *BLEAPI) {
	// Create ble table
	L.NewTable()

	// ble:list() function
	L.PushString("list")
	L.PushGoFunction(func(L *lua.State) int {
		uuids := api.ListCharacteristics()
		L.NewTable()
		for i, uuid := range uuids {
			L.PushInteger(int64(i + 1))
			L.PushString(uuid)
			L.SetTable(-3)
		}
		return 1
	})
	L.SetTable(-3)

	// ble.get(uuid) function - get characteristic value
	L.PushString("get")
	L.PushGoFunction(func(L *lua.State) int {
		uuid := L.ToString(1) // First argument when called with dot notation

		api.mutex.RLock()
		char, exists := api.characteristics[uuid]
		mode := api.mode
		api.mutex.RUnlock()

		if !exists {
			L.PushNil()
			return 1
		}

		// For BLE→TTY mode, return just the value (shortcut)
		if mode == "ble_to_tty" {
			char.mutex.RLock()
			value := string(char.Value)
			char.mutex.RUnlock()
			L.PushString(value)
			return 1
		}

		// For TTY→BLE mode, return characteristic object with all metadata
		L.NewTable()

		// .value property
		L.PushString("value")
		char.mutex.RLock()
		value := string(char.Value)
		char.mutex.RUnlock()
		L.PushString(value)
		L.SetTable(-3)

		// .descriptors property (read-only)
		L.PushString("descriptors")
		L.NewTable()
		for descUUID, descValue := range char.Descriptors {
			L.PushString(descUUID)
			if descValue != nil {
				L.PushString(string(descValue))
			} else {
				L.PushNil()
			}
			L.SetTable(-3)
		}
		L.SetTable(-3)

		// .properties property (read-only)
		L.PushString("properties")
		L.NewTable()
		for prop, enabled := range char.Properties {
			L.PushString(prop)
			L.PushBoolean(enabled)
			L.SetTable(-3)
		}
		L.SetTable(-3)

		return 1
	})
	L.SetTable(-3)

	// ble.set(uuid, value) function - set characteristic value (TTY→BLE mode only)
	L.PushString("set")
	L.PushGoFunction(func(L *lua.State) int {
		uuid := L.ToString(1)  // First argument when called with dot notation
		value := L.ToString(2) // Second argument

		if api.mode == "tty_to_ble" {
			api.SetCharacteristicValue(uuid, []byte(value))
		}
		// Ignore writes in BLE→TTY mode (read-only)

		return 0
	})
	L.SetTable(-3)

	// Set global 'ble' variable
	L.SetGlobal("ble")
}
