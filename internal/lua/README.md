# BLE Lua API Reference

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

## BLE API

The global `ble` table provides BLE functionality.

### `ble.device`
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
print("Device:", ble.device.name)
print("Address:", ble.device.address)
print("RSSI:", ble.device.rssi, "dBm")

-- Iterate advertised services
for i, uuid in ipairs(ble.device.advertised_services) do
    print("Service:", uuid)
end

-- Access service data
for uuid, data in pairs(ble.device.service_data) do
    print(uuid, "=>", data)
end
```

### `ble.list()`
Returns a table mapping service UUIDs to service info.

**Returns:** `{ [service_uuid] = { characteristics = {char_uuid, ...} } }`

**Example:**
```lua
local services = ble.list()

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

### `ble.subscribe(config)`
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

ble.subscribe{
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
ble.subscribe{
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
ble.subscribe{
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
ble.subscribe{
    services = {},  -- ERROR: empty services array
    Callback = function(record) end
}

-- This will raise an error
ble.subscribe("invalid")  -- ERROR: expects table
```

## Complete Example: Heart Rate Monitor

```lua
local json = require("json")

print("Starting Heart Rate Monitor")
print("Device:", ble.device.name)

local sample_count = 0

ble.subscribe{
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

### `ble.characteristic(service_uuid, char_uuid)` → `handle`
Returns a characteristic handle with metadata.

**Handle fields:**
- `uuid` (string) - Characteristic UUID
- `service` (string) - Parent service UUID
- `properties` (table) - Boolean flags for each property:
  - `read` (boolean) - Supports read operations
  - `write` (boolean) - Supports write operations
  - `notify` (boolean) - Supports notifications
  - `indicate` (boolean) - Supports indications
- `descriptors` (array) - Array of descriptor UUIDs (1-indexed)

**Example: Metadata inspection**
```lua
local hr_char = ble.characteristic("180d", "2a37")

print("UUID:", hr_char.uuid)
print("Service:", hr_char.service)

-- Check properties
if hr_char.properties.read then
    print("Supports READ")
end

if hr_char.properties.notify then
    print("Supports NOTIFY")
end

-- List descriptors
for i, desc_uuid in ipairs(hr_char.descriptors) do
    print("  Descriptor:", desc_uuid)
end
```

**Example: Inspect all characteristics**
```lua
local services = ble.list()

for service_uuid, service_info in pairs(services) do
    print("Service:", service_uuid)

    for _, char_uuid in ipairs(service_info.characteristics) do
        local char = ble.characteristic(service_uuid, char_uuid)

        io.write("  Char: " .. char_uuid .. " [")
        if char.properties.read then io.write("R") end
        if char.properties.write then io.write("W") end
        if char.properties.notify then io.write("N") end
        if char.properties.indicate then io.write("I") end
        io.write("]\n")
    end
end
```

**Output:**
```
Service: 180d
  Char: 2a37 [RN]
  Char: 2a38 [R]
Service: 180f
  Char: 2a19 [RN]
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

**Note:** The `ble.characteristic()` function is already implemented (see above), but currently only returns metadata. The handle methods (`read()`, `write()`, `subscribe()`, etc.) will be added in future updates.

---

### API Design Rationale

**Function-based** (`ble.read()`, `ble.write()`) is ideal for:
- Simple scripts with occasional operations
- One-off reads/writes
- Maximum code clarity

**Handle-based** (`ble.characteristic()` → handle) is ideal for:
- Tight loops with repeated operations
- Bulk data transfers
- When metadata access is needed
- 6× performance improvement for 1000+ operations

Both approaches coexist - use whichever fits your use case.

---

## Current Limitations

- ⚠️ **No read operations** - Cannot read characteristic values on demand
- ⚠️ **No write operations** - Cannot write to characteristics
- ⚠️ **No unsubscribe** - Subscriptions run indefinitely
- ⚠️ **No service discovery from Lua** - Must know service/characteristic UUIDs beforehand (use `ble.list()` after connection)

**Available features:**
- ✅ **Characteristic inspection** - `ble.characteristic()` returns metadata (UUID, service, properties, descriptors)
- ✅ **Service listing** - `ble.list()` enumerates all GATT services and characteristics
- ✅ **Device information** - `ble.device` provides device metadata and advertisement data
- ✅ **Subscriptions** - `ble.subscribe()` supports notifications/indications with multiple streaming modes

Remaining limitations will be addressed by the upcoming API extensions described above.