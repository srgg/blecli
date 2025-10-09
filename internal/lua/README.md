# BLIM Lua API Reference

Lua scripting interface for Bluetooth Low Energy device interaction.

## Output Functions

### `print(...)`
Prints values to stdout with tab separators and automatic newline.

```lua
print("Hello", 42, true)  -- Output: Hello\t42\ttrue\n
```

### `io.write(...)`
Writes values to stdout without separators or automatic newline.

```lua
io.write("Hello")         -- Output: Hello
io.write("Line\n")        -- Output: Line\n
```

## JSON Library

### `json.encode(table)`
Encodes a Lua table to JSON string.

```lua
local json = require("json")
local data = {name = "sensor", value = 42}
print(json.encode(data))  -- {"name":"sensor","value":42}
```

### `json.decode(string)`
Decodes a JSON string to Lua table.

```lua
local json = require("json")
local obj = json.decode('{"temp":23.5}')
print(obj.temp)  -- 23.5
```

## BLIM API

The global `blim` table provides BLE functionality.

### `blim.device`
Read-only table containing device information.

**Fields:**
- `id` (string) - Device ID
- `address` (string) - MAC address (e.g., "AA:BB:CC:DD:EE:FF")
- `name` (string) - Device name (may be empty)
- `rssi` (number) - Signal strength in dBm
- `connectable` (boolean) - Whether device accepts connections
- `tx_power` (number, optional) - Transmit power in dBm
- `last_seen` (string) - ISO 8601 timestamp
- `advertised_services` (array) - Service UUIDs from advertisements
- `manufacturer_data` (string) - Hex-encoded manufacturer data
- `service_data` (table) - Map of service UUID to hex-encoded data

**Example:**
```lua
print("Device:", blim.device.name)
print("Address:", blim.device.address)
print("RSSI:", blim.device.rssi, "dBm")

-- Iterate advertised services
for i, uuid in ipairs(blim.device.advertised_services) do
    print("Service:", uuid)
end

-- Access service data
for uuid, data in pairs(blim.device.service_data) do
    print(uuid, "=>", data)
end
```

### `blim.bridge`
Read-only table containing bridge information (only available when running in bridge mode).

**Fields:**
- `pty_name` (string) - PTY device path (e.g., "/dev/ttys010")
- `symlink_path` (string) - Symlink path to PTY (empty if not created)

**Example:**
```lua
-- Check if running in bridge mode
if blim.bridge.pty_name and blim.bridge.pty_name ~= "" then
    print("Bridge PTY:", blim.bridge.pty_name)

    if blim.bridge.symlink_path ~= "" then
        print("Symlink:", blim.bridge.symlink_path)
    end
else
    print("Not running in bridge mode")
end
```

### `blim.list()`
Returns a table mapping service UUIDs to service info.

**Returns:** `{ [service_uuid] = { characteristics = {char_uuid, ...} } }`

**Example:**
```lua
local services = blim.list()

for service_uuid, service_info in pairs(services) do
    print("Service:", service_uuid)

    for i, char_uuid in ipairs(service_info.characteristics) do
        print("  Char:", char_uuid)
    end
end
```

**Output:**
```
Service: 180d
  Char: 2a37
  Char: 2a38
Service: 180f
  Char: 2a19
```

### `blim.subscribe(config)`
Subscribes to BLE characteristic notifications/indications.

**Parameters:**
- `config` (table) - Subscription configuration

**Config fields:**
- `services` (array) - List of service/characteristic subscriptions
  - Each entry: `{service="UUID", chars={"UUID", ...}}`
- `Mode` (string, optional) - Streaming mode (default: "EveryUpdate")
  - `"EveryUpdate"` - Every characteristic update triggers callback
  - `"Batched"` - Multiple updates batched together
  - `"Aggregated"` - Latest value per characteristic
- `MaxRate` (number, optional) - Max callback rate in milliseconds (0 = unlimited)
- `Callback` (function) - Called with each record: `function(record)`

**Record structure:**
- `TsUs` (number) - Timestamp in microseconds
- `Seq` (number) - Sequence number
- `Flags` (number) - Record flags
- `Values` (table, EveryUpdate/Aggregated) - Map of characteristic UUID to byte string
- `BatchValues` (table, Batched) - Map of characteristic UUID to array of byte strings

**Example: EveryUpdate mode**
```lua
local json = require("json")

blim.subscribe{
    services = {
        {service="180d", chars={"2a37"}},  -- Heart Rate
        {service="180f", chars={"2a19"}}   -- Battery
    },
    Mode = "EveryUpdate",
    MaxRate = 100,  -- Max 10 Hz
    Callback = function(record)
        -- Access characteristic values
        for uuid, data in pairs(record.Values) do
            -- Convert byte string to hex
            local hex = string.format("%02X", string.byte(data, 1))
            print(json.encode{
                seq = record.Seq,
                char = uuid,
                value = hex
            })
        end
    end
}
```

**Example: Batched mode**
```lua
blim.subscribe{
    services = {
        {service="180d", chars={"2a37", "2a38"}}
    },
    Mode = "Batched",
    MaxRate = 1000,  -- Max 1 Hz
    Callback = function(record)
        for uuid, values in pairs(record.BatchValues) do
            print("Characteristic:", uuid)
            print("  Received", #values, "updates")
            -- values is an array of byte strings
            for i, data in ipairs(values) do
                print("  [" .. i .. "]", string.byte(data, 1))
            end
        end
    end
}
```

**Example: Aggregated mode**
```lua
blim.subscribe{
    services = {
        {service="180d", chars={"2a37"}}
    },
    Mode = "Aggregated",
    MaxRate = 500,  -- Max 2 Hz
    Callback = function(record)
        -- Only latest value per characteristic
        for uuid, data in pairs(record.Values) do
            local value = string.byte(data, 1)
            print("Latest " .. uuid .. ":", value)
        end
    end
}
```

## Working with Binary Data

Characteristic values are Lua strings containing raw bytes.

**Convert bytes to hex:**
```lua
local function to_hex(bytes)
    local hex = {}
    for i = 1, #bytes do
        hex[i] = string.format("%02X", string.byte(bytes, i))
    end
    return table.concat(hex)
end

-- Usage in callback
Callback = function(record)
    for uuid, data in pairs(record.Values) do
        print(uuid, "=", to_hex(data))
    end
end
```

**Extract multi-byte values:**
```lua
-- Little-endian uint16
local function read_uint16_le(bytes, offset)
    offset = offset or 1
    local b1 = string.byte(bytes, offset)
    local b2 = string.byte(bytes, offset + 1)
    return b1 + b2 * 256
end

-- Parse heart rate measurement
Callback = function(record)
    local hr_data = record.Values["2a37"]
    if hr_data then
        local flags = string.byte(hr_data, 1)
        local is_16bit = (flags & 0x01) ~= 0

        local bpm
        if is_16bit then
            bpm = read_uint16_le(hr_data, 2)
        else
            bpm = string.byte(hr_data, 2)
        end

        print("Heart Rate:", bpm, "bpm")
    end
end
```

## Error Handling

Errors in Lua scripts are sent to stderr and logged.

```lua
-- This will raise an error
blim.subscribe{
    services = {},  -- ERROR: empty services array
    Callback = function(record) end
}

-- This will raise an error
blim.subscribe("invalid")  -- ERROR: expects table
```

## Complete Example: Heart Rate Monitor

```lua
local json = require("json")

print("Starting Heart Rate Monitor")
print("Device:", blim.device.name)

local sample_count = 0

blim.subscribe{
    services = {
        {service="180d", chars={"2a37"}}  -- Heart Rate Measurement
    },
    Mode = "EveryUpdate",
    MaxRate = 0,  -- No rate limiting
    Callback = function(record)
        sample_count = sample_count + 1

        local hr_data = record.Values["2a37"]
        if hr_data then
            local flags = string.byte(hr_data, 1)
            local is_16bit = (flags & 0x01) ~= 0

            local bpm
            if is_16bit then
                local b1 = string.byte(hr_data, 2)
                local b2 = string.byte(hr_data, 3)
                bpm = b1 + b2 * 256
            else
                bpm = string.byte(hr_data, 2)
            end

            print(json.encode{
                timestamp = record.TsUs,
                sample = sample_count,
                heart_rate = bpm
            })
        end
    end
}
```

### `blim.characteristic(service_uuid, char_uuid)` → `handle`
Returns a characteristic handle with metadata and methods.

**Handle fields:**
- `uuid` (string) - Characteristic UUID
- `service` (string) - Parent service UUID
- `properties` (table) - Boolean flags for each property:
  - `read` (boolean) - Supports read operations
  - `write` (boolean) - Supports write operations
  - `notify` (boolean) - Supports notifications
  - `indicate` (boolean) - Supports indications
- `descriptors` (array) - Array of descriptor UUIDs (1-indexed)

**Handle methods:**
- `read()` → `data, error` - Reads characteristic value from device

**Example: Read characteristic value**
```lua
local char = blim.characteristic("180a", "2a29")  -- Device Info: Manufacturer Name

if char.properties.read then
    local value, err = char.read()
    if value then
        print("Manufacturer:", value)
    else
        print("Read failed:", err)
    end
end
```

**Example: Inspect and read all readable characteristics**
```lua
local services = blim.list()

for service_uuid, service_info in pairs(services) do
    print("Service:", service_uuid)

    for _, char_uuid in ipairs(service_info.characteristics) do
        local char = blim.characteristic(service_uuid, char_uuid)

        io.write("  Char: " .. char_uuid .. " [")
        if char.properties.read then io.write("R") end
        if char.properties.write then io.write("W") end
        if char.properties.notify then io.write("N") end
        if char.properties.indicate then io.write("I") end
        io.write("]\n")

        -- Read value if readable
        if char.properties.read then
            local value, err = char.read()
            if value then
                print("    Value:", value)
            end
        end
    end
end
```

**Output:**
```
Service: 180d
  Char: 2a37 [RN]
  Char: 2a38 [R]
    Value: Body Sensor Location
Service: 180f
  Char: 2a19 [RN]
    Value: 85
```

---

## TODO: Upcoming API Extensions

The following functions are planned for future implementation to provide complete BLE interaction capabilities.

### Simple Function-based API (One-off Operations)

For simple scripts and one-time operations:

#### `ble.read(service_uuid, char_uuid)` → `data`
Performs a one-time read of a characteristic value.

```lua
-- Read battery level once
local battery_data = ble.read("180f", "2a19")
local battery_percent = string.byte(battery_data)
print("Battery:", battery_percent, "%")
```

#### `ble.write(service_uuid, char_uuid, data)`
Writes data to a characteristic (write without response).

```lua
-- Write configuration value
ble.write("1234", "5678", "\x01\x00")
```

#### `ble.write_with_response(service_uuid, char_uuid, data)`
Writes data to a characteristic and waits for response.

```lua
-- Write with acknowledgment
ble.write_with_response("1234", "5678", "\x01\x00")
```

#### `ble.unsubscribe(service_uuid, char_uuid)`
Unsubscribes from characteristic notifications.

```lua
-- Stop receiving heart rate updates
ble.unsubscribe("180d", "2a37")

-- Unsubscribe from all
ble.unsubscribe()
```

**Note:** The handle-based `read()` method is already implemented. The function-based API and other handle methods (`write()`, `subscribe()`, etc.) will be added in future updates.

---

### API Design Rationale

**Function-based** (`ble.read()`, `ble.write()`) is ideal for:
- Simple scripts with occasional operations
- One-off reads/writes
- Maximum code clarity

**Handle-based** (`blim.characteristic()` → handle) is ideal for:
- Tight loops with repeated operations
- Bulk data transfers
- When metadata access is needed
- 6× performance improvement for 1000+ operations

Both approaches will coexist - use whichever fits your use case.

---

## Current Status

**✅ Available features:**
- ✅ **Read operations** - `handle.read()` reads characteristic values on demand
- ✅ **Characteristic inspection** - `blim.characteristic()` returns metadata (UUID, service, properties, descriptors)
- ✅ **Service listing** - `blim.list()` enumerates all GATT services and characteristics
- ✅ **Device information** - `blim.device` provides device metadata and advertisement data
- ✅ **Subscriptions** - `blim.subscribe()` supports notifications/indications with multiple streaming modes

**⚠️ Planned features:**
- ⚠️ **Write operations** - Cannot write to characteristics yet
- ⚠️ **Unsubscribe** - Subscriptions run indefinitely (no way to stop them)
- ⚠️ **Function-based API** - Simplified `ble.read()`, `ble.write()` not yet available

These will be addressed by the upcoming API extensions described above.

---

# Developer Documentation

## Panic Recovery for Go Functions Exposed to Lua

### Critical Requirement

**ALL Go functions exposed to Lua MUST be wrapped with panic recovery** to prevent crashes and ensure proper error handling.

### Implementation

#### For BLE API Functions

Use `BLEAPI2.SafePushGoFunction()` helper:

```go
// Correct - wrapped with SafePushGoFunction
api.SafePushGoFunction(L, "read", func(L *lua.State) int {
    // Your implementation
    // Can safely use L.RaiseError() for expected errors
    L.RaiseError("invalid argument")
    return 0
})
L.SetTable(-3)
```

#### For Engine-Level Functions

Use `LuaEngine.SafeWrapGoFunction()`:

```go
// Correct - wrapped with SafeWrapGoFunction
L.PushGoFunction(e.SafeWrapGoFunction("print()", func(L *lua.State) int {
    // Your implementation
    return 0
}))
L.SetGlobal("print")
```

### Panic Recovery Behavior

The wrapper handles panics as follows:

1. **Expected Lua Errors** (`*lua.LuaError` from `L.RaiseError()`)
   - Re-panicked as-is to propagate to Lua runtime
   - Error message preserved exactly

2. **Unexpected Panics** (strings, structs, nil pointer, etc.)
   - Caught and logged with full stack trace for debugging
   - Converted to clean Lua error: `"function_name() panicked in Go"`
   - Prevents process crash

### Currently Wrapped Functions

All Go functions exposed to Lua are wrapped:

**BLE API (`lua_api.go`):**
- ✅ `blim.subscribe()`
- ✅ `blim.list()`
- ✅ `blim.characteristic()`
- ✅ `char.read()` (characteristic handle method)

**Engine Functions (`lua_engine.go`):**
- ✅ `print()` (overridden for output capture)
- ✅ `io.write()` (overridden for output capture)

**Blocked Functions:**
- Stub functions that call `L.RaiseError()` are simple and less critical

### Adding New Lua-Exposed Functions

When adding a new Go function for Lua:

```go
// ❌ WRONG - Direct PushGoFunction (crashes on panic)
L.PushGoFunction(func(L *lua.State) int {
    // dangerous - any panic crashes the process
    return 0
})

// ✅ CORRECT - Wrapped with SafePushGoFunction
api.SafePushGoFunction(L, "new_function", func(L *lua.State) int {
    // safe - panics are caught and converted to Lua errors
    return 0
})
```

### Testing

See `lua_engine_test.go::TestSafeWrapGoFunction` for comprehensive panic recovery tests covering:
- Expected Lua errors (from `L.RaiseError`)
- Unexpected panics (strings, structs, etc.)
- Normal execution
- Multiple wrapped functions