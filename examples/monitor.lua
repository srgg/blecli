-- Bridge Monitor: Subscribe to all characteristics and dump values

-- Helper: Convert byte string to hex representation
local function to_hex(data)
    local hex = {}
    for i = 1, #data do
        hex[i] = string.format("%02X", string.byte(data, i))
    end
    return table.concat(hex, " ")
end

-- Helper: Convert byte string to printable ASCII (non-printable as dots)
local function to_ascii(data)
    local ascii = {}
    for i = 1, #data do
        local b = string.byte(data, i)
        ascii[i] = (b >= 32 and b <= 126) and string.char(b) or "."
    end
    return table.concat(ascii)
end

-- Helper: Shorten UUID (show only first 8 chars for full UUIDs)
local function short_uuid(uuid)
    if #uuid > 8 then
        return uuid:sub(1, 8)
    end
    return uuid
end

-- Discover all services and characteristics
local services_table = ble.list()

-- Check if any services were found
local service_count = 0
for _ in pairs(services_table) do
    service_count = service_count + 1
end

if service_count == 0 then
    print("")
    print(string.format("Device: %s", ble.device.address))
    print("Characteristics: 0 (service not found)")
    print("")
    error("No services found - cannot start bridge")
end

-- Build subscription services array and count total characteristics
local services = {}
local total_chars = 0
for service_uuid, service_info in pairs(services_table) do
    local chars = {}
    for i, char_uuid in ipairs(service_info.characteristics) do
        table.insert(chars, char_uuid)
        total_chars = total_chars + 1
    end
    table.insert(services, {
        service = service_uuid,
        chars = chars
    })
end

-- Verify at least one characteristic exists
if total_chars == 0 then
    error("No characteristics found - cannot start bridge")
end

-- Print header in bridge.go format (only after validation)
print("")
print("=== BLE-PTY Bridge is Active ===")
print(string.format("Device: %s", ble.device.address))

-- Print all services and their characteristics
for service_uuid, service_info in pairs(services_table) do
    print(string.format("Service: %s", service_uuid))
    print(string.format("Characteristics: %d", #service_info.characteristics))
    for i, char_uuid in ipairs(service_info.characteristics) do
        print(string.format("  - %s", char_uuid))
    end
    print("")
end

print("Bridge is running. Press Ctrl+C to stop the bridge.")
print("")

-- Subscribe to all available characteristics with human-readable output
ble.subscribe{
    services = services,
    Mode = "EveryUpdate",
    MaxRate = 0,
    Callback = function(record)
        -- Print compact notification for each characteristic
        for uuid, data in pairs(record.Values) do
            local hex = to_hex(data)
            local ascii = to_ascii(data)
            local short = short_uuid(uuid)

            -- Compact format: [seq] UUID: hex | ascii (len bytes)
            print(string.format("[%d] %s: %s | %s (%db)",
                record.Seq, short, hex, ascii, #data))
        end
    end
}