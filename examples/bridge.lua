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

-- Sort service UUIDs for consistent output
local service_uuids = {}
for uuid in pairs(services_table) do
    table.insert(service_uuids, uuid)
end
table.sort(service_uuids)

-- Build subscription services array - only include characteristics that support notify/indicate
local services = {}
local total_chars = 0
for _, service_uuid in ipairs(service_uuids) do
    local service_info = services_table[service_uuid]

    -- Filter characteristics to only those that support notifications
    local notifiable_chars = {}
    for i, char_uuid in ipairs(service_info.characteristics) do
        local char_info = ble.characteristic(service_uuid, char_uuid) or {}
        if char_info.properties and (char_info.properties.notify or char_info.properties.indicate) then
            table.insert(notifiable_chars, char_uuid)
        end
    end

    -- Only add service if it has notifiable characteristics
    if #notifiable_chars > 0 then
        -- Sort characteristics for deterministic order
        table.sort(notifiable_chars)

        total_chars = total_chars + #notifiable_chars
        table.insert(services, {
            service = service_uuid,
            chars = notifiable_chars
        })
    end
end

-- Verify at least one notifiable characteristic exists
if total_chars == 0 then
    error("No notifiable characteristics found - cannot start bridge")
end

-- Subscribe FIRST (before printing header) - this validates the subscription
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

-- If we reach here, subscription succeeded - print header
print("")
print("=== BLE-PTY Bridge is Active ===")
print(string.format("Device: %s", ble.device.address))

-- Print only services with notifiable characteristics
for _, svc in ipairs(services) do
    print(string.format("Service: %s", svc.service))
    print(string.format("Characteristics: %d", #svc.chars))
    for i, char_uuid in ipairs(svc.chars) do
        print(string.format("  - %s", char_uuid))
    end
    print("")
end

print("Bridge is running. Press Ctrl+C to stop the bridge.")
print("")