# BLE↔TTY Bidirectional Gateway Design

This document describes an Agile-friendly architecture for a **bidirectional BLE↔TTY transformation engine**. 
It focuses on **maximizing flexibility**, **keeping Go as a dumb engine**, and leaving **all protocol logic in Lua scripts**.

**Preferred Lua runtime:** Go-LuaHit for maximum performance.

---

## 2. Responsibilities Split

**Go Engine:**

* Collects and preserves raw data from BLE and TTY.
* Exposes Lua-accessible APIs (see Buffer API and BLE API sections) for accessing and manipulating raw data buffers.
* Does **not interpret or transform data**; Lua scripts implement all domain logic.
* Preserves buffers between Lua invocations so scripts can peek, read, rewind, and consume data as needed.
* **Preferred Lua runtime:** Go-LuaHit to ensure high-performance execution.

**Lua Scripts:**

* Implement domain logic for encoding (BLE→TTY) and decoding (TTY→BLE).
* Use the exposed Buffer API and BLE API to read and write values according to the direction rules.
* Signal errors back to Go (fatal and non-fatal) according to the error handling model.

---

## 3. APIs

### 3.1 Buffer API (Go → Lua)

```lua
buffer:read(n)      -- returns up to n bytes, consumes them
buffer:peek(n)      -- returns up to n bytes, does not consume
buffer:consume(n)   -- discards n bytes
buffer:append(data) -- append bytes (Go feeds input)
```

### 3.2 BLE API (Go → Lua)

```lua
ble:list()                  -- returns an array of all available characteristic UUIDs

ble[uuid]                    -- read value only (read-only in BLE→TTY)
ble[uuid].value              -- read or write value (write only in TTY→BLE)

ble[uuid].descriptors        -- map of descriptor UUID -> value (read-only)
ble[uuid].properties         -- map of property -> boolean (read-only)
```

**Notes:**

* BLE→TTY scripts: **read-only** (`.value`, `.descriptors`, `.properties`)
* TTY→BLE scripts: **write `.value` only**, metadata remains read-only
* Shortcut `ble[uuid]` is equivalent to `ble[uuid].value`

---

## 4. Error Handling

Error handling distinguishes between **fatal** and **non-fatal** cases.

### 4.1 Non-Fatal Errors

* Lua returns `nil, "message"`.
* Example: incomplete data, temporary parse failure.
* Go logs and continues.

### 4.2 Fatal Errors

* Lua calls `error("msg")` or `assert(cond, "msg")`.
* Example: corrupted input, impossible state.
* Go stops or restarts the pipeline.

---

## 5. Go Integration Notes

* Use `pcall` (or equivalent) when invoking Lua.
* On non-fatal: log + return control to main loop.
* On fatal: wrap error in `EngineError{Fatal}`, decide restart/shutdown.
* Preserve buffers between invocations so Lua can rewind/consume at its own pace.
* **Use Go-LuaHit runtime** for maximum execution performance.

---

## 6. Iteration Plan

* **Iteration 1:** Implement Go buffer + Lua bindings + error propagation.
* **Iteration 2:** Write reference Lua scripts (BLE→TTY, TTY→BLE).
* **Iteration 3:** Add logging, fatal/non-fatal dashboards.
* **Iteration 4:** Package as CLI tool, test in real BLE↔serial environments.

---

## 7. Example Usage

**BLE→TTY**

```lua
-- read characteristic value (read-only)
local temp_val = ble["2A6E"]

-- full object access
local temp_char = ble["2A6E"]
print(temp_char.value)
print(temp_char.descriptors["2901"])
print(temp_char.properties.notify)

-- transform to TTY bytes
local tty_bytes = string.pack(">h", temp_char.value)
buffer:append(tty_bytes)
```

**TTY→BLE**

```lua
-- peek buffer to see if enough data
local chunk = buffer:peek(2)
if #chunk < 2 then
    return nil, "waiting for more data"
end

-- decode value
local val = string.unpack(">h", chunk)
ble["2A6E"] = val   -- update value only
buffer:consume(2)
```
