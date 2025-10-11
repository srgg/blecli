-- BLIM API - Lua wrapper around Go-implemented functions
-- CGO-like approach: Lua functions call Go backends via _blim_internal

local blim = {}
local native = _blim_internal

-- Direct assignments (zero overhead - just references to Go functions)
blim.subscribe = native.subscribe
blim.list = native.list
blim.characteristic = native.characteristic
blim.device = native.device
blim.bridge = native.bridge
blim.sleep = native.sleep



-- Helper functions for Lua scripts

-- Convert byte string to hex representation with spaces between bytes
-- Example: "AB\x01" -> "41 42 01"
function blim.to_hex(data)
    if not data or data == "" then
        return ""
    end
    local hex = {}
    for i = 1, #data do
        hex[i] = string.format("%02X", string.byte(data, i))
    end
    return table.concat(hex, " ")
end

-- Convert byte string to hex representation without spaces (uppercase)
-- Example: "AB\x01" -> "4142FF"
function blim.bytes_to_hex(data)
    if not data or data == "" then
        return ""
    end
    return string.upper(data:gsub(".", function(c)
        return string.format("%02X", string.byte(c))
    end))
end

-- Convert byte string to printable ASCII (non-printable chars become '.')
-- Example: "Hello\x00World" -> "Hello.World"
function blim.to_ascii(data)
    if not data or data == "" then
        return ""
    end
    local ascii = {}
    for i = 1, #data do
        local b = string.byte(data, i)
        ascii[i] = (b >= 32 and b <= 126) and string.char(b) or "."
    end
    return table.concat(ascii)
end


-- Shorten UUID (show only first eight chars for long UUIDs)
-- Example: "6e400001-b5a3-f393-e0a9-e50e24dcca9e" -> "6e400001"
function blim.short_uuid(uuid)
    if not uuid then
        return ""
    end
    if #uuid > 8 then
        return uuid:sub(1, 8)
    end
    return uuid
end


-- Export as global blim
_G.blim = blim

return blim