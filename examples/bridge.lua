-- Bridge Monitor: Subscribe to all characteristics and dump values

-- Load shared utilities

-- Discover all services and characteristics
local services_table = blim.list()

-- Check if any services were found
local service_count = 0
for _ in pairs(services_table) do
    service_count = service_count + 1
end

if service_count == 0 then
    print("")
    print(string.format("Device: %s", blim.device.address))
    print("Characteristics: 0 (service not found)")
    print("")
    error("No services found - cannot start bridge")
end

-- Build subscription services array - only include characteristics that support notify/indicate
-- Note: services_table has both array part (for ordered iteration) and hash part (for UUID lookup)
-- We use ipairs() to iterate in sorted order: services_table[1], services_table[2], etc.
local services = {}
local total_chars = 0
for _, service_uuid in ipairs(services_table) do
    local service_info = services_table[service_uuid]  -- Lookup service info by UUID

    -- Filter characteristics to only those that support notifications
    local notifiable_chars = {}
    for i, char_uuid in ipairs(service_info.characteristics) do
        local char_info = blim.characteristic(service_uuid, char_uuid) or {}
        if char_info.properties and (char_info.properties.notify or char_info.properties.indicate) then
            table.insert(notifiable_chars, char_uuid)
        end
    end

    -- Only add service if it has notifiable characteristics
    if #notifiable_chars > 0 then
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
blim.subscribe{
    services = services,
    Mode = "EveryUpdate",
    MaxRate = 0,
    Callback = function(record)
        -- Print compact notification for each characteristic
        for uuid, data in pairs(record.Values) do
            local hex = blim.to_hex(data)
            local ascii = blim.to_ascii(data)
            local short = blim.short_uuid(uuid)

            -- Compact format: [seq] UUID: hex | ascii (len bytes)
            print(string.format("[%d] %s: %s | %s (%db)",
                record.Seq, short, hex, ascii, #data))
        end
    end
}
