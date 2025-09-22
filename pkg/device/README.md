The LUA examples:

```lua

-- Assume `ble` is the Go-side BLEStreamManager exposed to Lua via cgo or GopherLua
-- For example, the Go manager registers a Lua table `ble` with `subscribe` function.

-- ---------------------------
-- 1️⃣ Simple subscription to a single characteristic
-- ---------------------------
ble:subscribe{
    services = {
        {    
            service = "0000180d-0000-1000-8000-00805f9b34fb",
            chars = {"00002a37-0000-1000-8000-00805f9b34fb"},
        },
        {
            service = "1000180d-0000-1000-8000-00805f9b34fb",
            chars = {"10002a37-0000-1000-8000-00805f9b34fb"},
        }
    },

    Pattern = "EveryUpdate",  -- every BLE notification triggers callback
    MaxRate = 0,              -- ignored for EveryUpdate
    Callback = function(record) 
        for uuid, val in pairs(record.Values) do
            print(string.format("UUID: %s, Data: %s, Flags: %d", uuid, val, record.Flags))
        end
    end
}

-- ---------------------------
-- 2️⃣ Batch updates at 20 Hz
-- ---------------------------
ble:subscribe{
    Chars = {"00002a37-0000-1000-8000-00805f9b34fb"},
    Pattern = "Batched",
    MaxRate = 50 * 1e6 / 1,  -- MaxRate in microseconds? Adjust per Go binding
    Callback = function(record)
        print("Batched record:")
        for uuid, val in pairs(record.Values) do
            print("  UUID:", uuid, "Data:", val, "Flags:", record.Flags)
        end
        if (record.Flags & 1) ~= 0 then
            print("Warning: dropped updates detected")
        end
    end
}

-- ---------------------------
-- 3️⃣ Aggregated subscription across multiple characteristics
-- ---------------------------
ble:subscribe{
    Chars = {
        "00002a37-0000-1000-8000-00805f9b34fb",
        "00002a38-0000-1000-8000-00805f9b34fb",
        "00002a39-0000-1000-8000-00805f9b34fb",
    },
    Pattern = "Aggregated",
    MaxRate = 50,  -- 50 Hz aggregation
    Callback = function(record)
        print("Aggregated record:")
        for uuid, val in pairs(record.Values) do
            local status = ""
            if (record.Flags & 1) ~= 0 then status = status .. " DROPPED" end
            if (record.Flags & 2) ~= 0 then status = status .. " MISSING" end
            print("  UUID:", uuid, "Data:", val, "Flags:", status)
        end
    end
}

-- ---------------------------
-- Notes:
-- ---------------------------
-- 1. Flags:
--    - FlagDropped = 1  (value lost in Go due to channel full)
--    - FlagMissing = 2  (characteristic had no new update at aggregation time)
--
-- 2. Lua never handles raw BLE; Go fully manages BLE subscriptions, memory reuse, and rate-limiting.
--    Lua only receives ready-to-use records.
--
-- 3. Aggregated pattern produces **aligned records** for multiple characteristics,
--    which is perfect for metrics that must be synchronized.


```