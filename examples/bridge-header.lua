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
        local char_info = ble.characteristic(service_uuid, char_uuid) or {}
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