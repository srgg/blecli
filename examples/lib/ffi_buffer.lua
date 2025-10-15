-- ffi_buffer.lua
local ffi = require("ffi")

local FfiBuffer = {}
FfiBuffer.__index = FfiBuffer

function FfiBuffer.new(initial_size)
    local self = setmetatable({}, FfiBuffer)
    self.size = initial_size or 4096
    self.data = ffi.new("uint8_t[?]", self.size)
    self.offset = 0
    self.length = 0
    return self
end

function FfiBuffer:append(str)
    local n = #str
    
    -- Check if we need to compact or grow
    if self.offset + self.length + n > self.size then
        if self.length + n <= self.size then
            -- Just compact
            ffi.copy(self.data, self.data + self.offset, self.length)
            self.offset = 0
        else
            -- Need to grow
            local newsize = math.max(self.length + n, self.size * 2)
            local newdata = ffi.new("uint8_t[?]", newsize)
            ffi.copy(newdata, self.data + self.offset, self.length)
            self.data = newdata
            self.size = newsize
            self.offset = 0
        end
    end
    
    ffi.copy(self.data + self.offset + self.length, str, n)
    self.length = self.length + n
end

function FfiBuffer:byte(i)
    if i < 0 or i >= self.length then return nil end
    return self.data[self.offset + i]
end

function FfiBuffer:discard(n)
    if n >= self.length then
        self.offset = 0
        self.length = 0
        return
    end
    
    self.offset = self.offset + n
    self.length = self.length - n
    
    -- Compact if offset gets too big
    if self.offset > self.size / 2 then
        ffi.copy(self.data, self.data + self.offset, self.length)
        self.offset = 0
    end
end

function FfiBuffer:clear()
    self.offset = 0
    self.length = 0
end

return FfiBuffer