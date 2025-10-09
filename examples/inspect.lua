-- BLE Inspect: Device Inspection
-- This script replicates the output format of the Go outputInspectText function

-- Device Information Service (DIS) characteristic mapping
local DIS_CHARACTERISTICS = {
    ["2A29"] = "Manufacturer Name",
    ["2A24"] = "Model Number",
    ["2A25"] = "Serial Number",
    ["2A26"] = "Firmware Revision",
    ["2A27"] = "Hardware Revision",
    ["2A28"] = "Software Revision",
    ["2A23"] = "System ID",
    ["2A50"] = "PnP ID"
}

-- Extract Device Information Service data
local function extract_dis_info(services)
    for _, service in ipairs(services) do
        -- Check if this is the Device Information Service (UUID 180A)
        if string.upper(service.uuid) == "180A" then
            local dis_data = {}
            for _, char in ipairs(service.characteristics) do
                local char_uuid_upper = string.upper(char.uuid)
                local char_name = DIS_CHARACTERISTICS[char_uuid_upper]
                if char_name and char.value and char.value ~= "" then
                    -- For string characteristics, use ASCII representation
                    if char_uuid_upper == "2A29" or char_uuid_upper == "2A24" or char_uuid_upper == "2A25" or
                       char_uuid_upper == "2A26" or char_uuid_upper == "2A27" or char_uuid_upper == "2A28" then
                        dis_data[char_name] = blim.to_ascii(char.value)
                    else
                        -- For other characteristics (System ID, PnP ID), keep as hex
                        dis_data[char_name] = blim.bytes_to_hex(char.value)
                    end
                end
            end
            return dis_data
        end
    end
    return nil
end

-- Collect all device and GATT data into a structured table
local function collect_device_data()
    local data = {}

    -- Device info
    data.device = {
        id = blim.device.id,
        address = blim.device.address,
        name = blim.device.name,
        rssi = blim.device.rssi,
        connectable = blim.device.connectable,
        tx_power = blim.device.tx_power,
        advertised_services = blim.device.advertised_services or {},
        manufacturer_data = blim.device.manufacturer_data,
        service_data = blim.device.service_data or {}
    }

    -- GATT Services
    data.services = {}
    local services = blim.list()

    -- Note: services table has both array part (for ordered iteration) and hash part (for UUID lookup)
    -- We use ipairs() to iterate in sorted order: services[1], services[2], etc.
    for _, service_uuid in ipairs(services) do
        local service_info = services[service_uuid]  -- Lookup service info by UUID
        local service_data = {
            uuid = service_uuid,
            characteristics = {}
        }

        if service_info.characteristics then
            -- Characteristics are already sorted by the BLE API
            for _, char_uuid in ipairs(service_info.characteristics) do
                local char_info = blim.characteristic(service_uuid, char_uuid) or {}

                -- Build properties object
                local props = {
                    read = char_info.properties and char_info.properties.read or false,
                    write = char_info.properties and char_info.properties.write or false,
                    notify = char_info.properties and char_info.properties.notify or false,
                    indicate = char_info.properties and char_info.properties.indicate or false
                }

                -- Try to read the characteristic value if it's readable
                local value = nil
                if props.read and char_info.read then
                    local val, err = char_info.read()
                    if err == nil then
                        value = val
                    end
                    -- Silently ignore read errors in inspect (characteristic may not be readable)
                end

                table.insert(service_data.characteristics, {
                    uuid = char_uuid,
                    properties = props,
                    value = value,
                    descriptors = char_info.descriptors or {}
                })
            end
        end

        table.insert(data.services, service_data)
    end

    -- Extract Device Information Service data
    data.device_info = extract_dis_info(data.services)

    return data
end

-- Format and output as human-readable text
local function output_text(data)
    -- Device info section
    io.write("Device info:\n")
    io.write(string.format("  ID: %s\n", data.device.id))
    io.write(string.format("  Address: %s\n", data.device.address))

    if data.device.name and data.device.name ~= "" then
        io.write(string.format("  Name: %s\n", data.device.name))
    end

    io.write(string.format("  RSSI: %d\n", data.device.rssi))
    io.write(string.format("  Connectable: %s\n", tostring(data.device.connectable)))

    if data.device.tx_power then
        io.write(string.format("  TxPower: %d dBm\n", data.device.tx_power))
    end

    -- Advertised Services section
    if #data.device.advertised_services > 0 then
        io.write("  Advertised Services:\n")
        for _, service_uuid in ipairs(data.device.advertised_services) do
            io.write(string.format("    - %s\n", service_uuid))
        end
    else
        io.write("  Advertised Services: none\n")
    end

    -- Manufacturer Data section
    if data.device.manufacturer_data and data.device.manufacturer_data ~= "" then
        io.write(string.format("  Manufacturer Data: %s\n", blim.bytes_to_hex(data.device.manufacturer_data)))
    else
        io.write("  Manufacturer Data: none\n")
    end

    -- Service Data section
    local service_data_keys = {}
    for k in pairs(data.device.service_data) do
        table.insert(service_data_keys, k)
    end

    if #service_data_keys > 0 then
        io.write("  Service Data:\n")
        table.sort(service_data_keys)
        for _, k in ipairs(service_data_keys) do
            io.write(string.format("    - %s: %s\n", k, blim.bytes_to_hex(data.device.service_data[k])))
        end
    else
        io.write("  Service Data: none\n")
    end

    -- Device Information Service section
    if data.device_info and next(data.device_info) ~= nil then
        io.write("  Device Information Service:\n")
        -- Define display order for DIS fields
        local dis_order = {
            "Manufacturer Name",
            "Model Number",
            "Serial Number",
            "Hardware Revision",
            "Firmware Revision",
            "Software Revision",
            "System ID",
            "PnP ID"
        }
        for _, field_name in ipairs(dis_order) do
            if data.device_info[field_name] then
                io.write(string.format("    %s: %s\n", field_name, data.device_info[field_name]))
            end
        end
    end

    -- GATT Services section
    io.write(string.format("  GATT Services: %d\n", #data.services))

    -- List services with characteristics
    for service_index, service in ipairs(data.services) do
        -- Show service name if it's DIS
        local service_name = service.uuid
        if string.upper(service.uuid) == "180A" then
            service_name = "Device Information Service (0x180A)"
        else
            service_name = string.format("0x%s", service.uuid)
        end
        io.write(string.format("\n[%d] Service: %s\n", service_index, service_name))

        for char_index, char in ipairs(service.characteristics) do
            -- Format properties as hex flags
            local props = 0x00
            if char.properties.read then props = props + 0x02 end
            if char.properties.write then props = props + 0x08 end
            if char.properties.notify then props = props + 0x10 end
            if char.properties.indicate then props = props + 0x20 end

            -- Show characteristic name if it's a known DIS characteristic
            local char_name = DIS_CHARACTERISTICS[string.upper(char.uuid)]
            local char_display = char_name and string.format("%s (0x%s)", char_name, char.uuid) or string.format("0x%s", char.uuid)

            io.write(string.format("  [%d.%d] Characteristic: %s (props: 0x%02X)\n",
                service_index, char_index, char_display, props))

            -- Show characteristic value if available
            if char.value and char.value ~= "" then
                local value_hex = blim.bytes_to_hex(char.value)
                local value_ascii = blim.to_ascii(char.value)

                if value_hex ~= "" then
                    io.write(string.format("      value (hex):   %s\n", value_hex))
                end
                if value_ascii ~= "" then
                    io.write(string.format("      value (ascii): %s\n", value_ascii))
                end
            end

            -- Show descriptors if available
            if #char.descriptors > 0 then
                for _, descriptor_uuid in ipairs(char.descriptors) do
                    io.write(string.format("      descriptor: %s\n", descriptor_uuid))
                end
            end
        end
    end
end

-- Format and output as JSON using the json library
local function output_json(data)
    local json = require("json")
    print(json.encode(data))
end

-- Check for format argument (default to "text")
-- Supports both URL params (arg["format"]) and positional args (arg[1])
local format = "text"
if arg then
    if arg["format"] and arg["format"] ~= "" then
        format = arg["format"]
    elseif arg[1] and arg[1] ~= "" then
        format = arg[1]
    end
end

-- Collect device data once
local data = collect_device_data()

-- Output in requested format
if format == "json" then
    output_json(data)
else
    output_text(data)
end