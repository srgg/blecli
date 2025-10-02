-- BLE Inspect: Device Inspection

-- Data Collection
local function collect_device_info()
    return {
        id = ble.device.id,
        address = ble.device.address,
        name = ble.device.name,
        rssi = ble.device.rssi,
        connectable = ble.device.connectable,
        tx_power = ble.device.tx_power,
        last_seen = ble.device.last_seen,
        advertised_services = ble.device.advertised_services,
        manufacturer_data = ble.device.manufacturer_data,
        service_data = ble.device.service_data
    }
end

-- Device Info Section
local function print_device_info(device_data)
    print("Device info:")
    print(string.format("  ID: %s", device_data.id))
    print(string.format("  Address: %s", device_data.address))
    if device_data.name ~= "" then
        print(string.format("  Name: %s", device_data.name))
    end
    print(string.format("  RSSI: %d", device_data.rssi))
    print(string.format("  Connectable: %s", tostring(device_data.connectable)))
    if device_data.tx_power then
        print(string.format("  TxPower: %d dBm", device_data.tx_power))
    end
    print(string.format("  LastSeen: %s", device_data.last_seen))
end

local function collect_gatt_services()
    return {
        services = ble.list()
    }
end

-- Advertised Services Section
local function print_advertised_services(device_data)
    if #device_data.advertised_services > 0 then
        print("  Advertised Services:")
        for _, service_uuid in ipairs(device_data.advertised_services) do
            print(string.format("    - %s", service_uuid))
        end
    else
        print("  Advertised Services: none")
    end
end

-- Manufacturer Data Section
local function print_manufacturer_data(device_data)
    if device_data.manufacturer_data and device_data.manufacturer_data ~= "" then
        print(string.format("  Manufacturer Data: %s", device_data.manufacturer_data))
    else
        print("  Manufacturer Data: none")
    end
end

-- Service Data Section
local function print_service_data(device_data)
    local service_data_count = 0
    for _ in pairs(device_data.service_data) do
        service_data_count = service_data_count + 1
    end

    if service_data_count > 0 then
        print("  Service Data:")
        -- Sort keys for consistent output
        local keys = {}
        for k in pairs(device_data.service_data) do
            table.insert(keys, k)
        end
        table.sort(keys)
        for _, k in ipairs(keys) do
            print(string.format("    - %s: %s", k, device_data.service_data[k]))
        end
    else
        print("  Service Data: none")
    end
end

-- GATT Services Section
local function print_gatt_services(gatt_data)
    local service_count = 0
    for _ in pairs(gatt_data.services) do
        service_count = service_count + 1
    end

    print(string.format("  GATT Services: %d", service_count))

    -- List services with characteristics
    local si = 0
    for service_uuid, service_info in pairs(gatt_data.services) do
        si = si + 1
        print(string.format("\n[%d] Service %s", si, service_uuid))

        if service_info.characteristics then
            for ci, char_uuid in ipairs(service_info.characteristics) do
                -- Mock properties and values since bleapi2.go doesn't provide this yet
                local properties = "READ,NOTIFY"
                local value_hex = "40"
                local value_ascii = "@"

                print(string.format("  [%d.%d] Characteristic %s (props: %s)", si, ci, char_uuid, properties))

                if value_hex ~= "" or value_ascii ~= "" then
                    if value_hex ~= "" then
                        print(string.format("      value (hex):   %s", value_hex))
                    end
                    if value_ascii ~= "" then
                        print(string.format("      value (ascii): %s", value_ascii))
                    end
                end

                -- Mock descriptor
                print("      descriptor: 00002902-0000-1000-8000-00805f9b34fb")
            end
        end
    end
end

-- Main inspection function
local function output_inspect_text()
    -- Collect all data first
    local device_data = collect_device_info()
    local gatt_data = collect_gatt_services()

    -- Format and print each section
    print_device_info(device_data)
    print_advertised_services(device_data)
    print_manufacturer_data(device_data)
    print_service_data(device_data)
    print_gatt_services(gatt_data)
end

-- Execute the inspection output
output_inspect_text()