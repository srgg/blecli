-- Example Lua script demonstrating the new BLE subscription API
-- This replaces the old TTY bridge with direct subscription functionality

print("Setting up BLE subscriptions...")

-- Example 1: Basic heart rate monitoring
ble:subscribe{
    services = {
        {
            service = "0000180d-0000-1000-8000-00805f9b34fb",  -- Heart Rate Service
            chars = {"00002a37-0000-1000-8000-00805f9b34fb"}   -- Heart Rate Measurement
        }
    },
    Pattern = "EveryUpdate",  -- Process every BLE notification immediately
    MaxRate = 0,              -- Ignored for EveryUpdate pattern
    Callback = function(record)
        print(string.format("Heart Rate Update [%d]: flags=%d", record.Seq, record.Flags))
        for uuid, data in pairs(record.Values) do
            print(string.format("  %s: %s", uuid, data))
        end
    end
}

-- Example 2: Multi-service subscription with batching
ble:subscribe{
    services = {
        {
            service = "0000180d-0000-1000-8000-00805f9b34fb",  -- Heart Rate Service
            chars = {"00002a37-0000-1000-8000-00805f9b34fb"}   -- Heart Rate Measurement
        },
        {
            service = "1000180d-0000-1000-8000-00805f9b34fb",  -- Custom Service
            chars = {"10002a37-0000-1000-8000-00805f9b34fb"}   -- Custom Characteristic
        }
    },
    Pattern = "Batched",      -- Collect data in batches
    MaxRate = 1000,           -- Process batches every 1000ms
    Callback = function(record)
        print(string.format("Batch Update [%d] at %d:", record.Seq, record.TsUs))
        local count = 0
        for uuid, data in pairs(record.Values) do
            count = count + 1
            print(string.format("  [%d] %s: %s", count, uuid, data))
        end
        if count == 0 then
            print("  No data in this batch")
        end
    end
}

-- Example 3: Aggregated data collection
ble:subscribe{
    services = {
        {
            service = "0000180d-0000-1000-8000-00805f9b34fb",
            chars = {"00002a37-0000-1000-8000-00805f9b34fb"}
        },
        {
            service = "1000180d-0000-1000-8000-00805f9b34fb",
            chars = {"10002a37-0000-1000-8000-00805f9b34fb"}
        }
    },
    Pattern = "Aggregated",   -- Get latest value from each characteristic
    MaxRate = 2000,           -- Aggregate every 2000ms
    Callback = function(record)
        print(string.format("Aggregated snapshot [%d] at %d:", record.Seq, record.TsUs))
        for uuid, data in pairs(record.Values) do
            print(string.format("  %s: %s", uuid, data))
        end
        if record.Flags ~= 0 then
            print(string.format("  Warning: Some data may be missing (flags: %d)", record.Flags))
        end
    end
}

print("BLE subscriptions configured! Waiting for data...")