-- Bridge for MotionCal calibration
-- (https://learn.adafruit.com/adafruit-sensorlab-magnetometer-calibration/magnetic-calibration-with-motioncal/)
--
-- High-frequency sensor data processing at 50Hz

local ffi = require("ffi")
local bit = require("bit")
local FfiBuffer = require("ffi_buffer")

ffi.cdef[[
typedef struct {
    float accel_zerog[3];
    float gyro_zerorate[3];
    float mag_hardiron[3];
    float mag_field;
    float mag_softiron[9];
} ImuCal;
]]

local rxbuf = FfiBuffer.new(1024)
local cal = ffi.new("ImuCal")
local HEADER1, HEADER2 = 117, 84
local PACKET_SIZE = 68

local function crc16_update(crc, b)
    crc = bit.bxor(crc, b)
    for _ = 1, 8 do
        if bit.band(crc, 1) ~= 0 then
            crc = bit.bxor(bit.rshift(crc, 1), 0xA001)
        else
            crc = bit.rshift(crc, 1)
        end
    end
    return bit.band(crc, 0xFFFF)
end

local function verify_crc(buf, offset)
    local crc = 0xFFFF
    for i = 0, PACKET_SIZE - 1 do
        crc = crc16_update(crc, buf:byte(offset + i))
    end
    return crc == 0
end

local function extract_floats(buf, offset)
    local floats = ffi.new("float[16]")
    ffi.copy(floats, buf.data + buf.offset + offset, 64)
    return floats
end

local function printCalibration()
    print("IMU calibration:")
    print(string.format("  Accel zero-g:    %.4f %.4f %.4f",
        cal.accel_zerog[0], cal.accel_zerog[1], cal.accel_zerog[2]))
    print(string.format("  Gyro zero-rate:  %.4f %.4f %.4f",
        cal.gyro_zerorate[0], cal.gyro_zerorate[1], cal.gyro_zerorate[2]))
    print(string.format("  Mag hard-iron:   %.4f %.4f %.4f",
        cal.mag_hardiron[0], cal.mag_hardiron[1], cal.mag_hardiron[2]))
    print(string.format("  Mag field:       %.4f", cal.mag_field))
    print("  Mag soft-iron matrix:")
    for i = 0, 6, 3 do
        print(string.format("    %.4f %.4f %.4f",
            cal.mag_softiron[i], cal.mag_softiron[i+1], cal.mag_softiron[i+2]))
    end
end

function receiveCalibrationChunk(ptr)
    if not ptr or #ptr == 0 then return end

    rxbuf:append(ptr)

    while rxbuf.length >= PACKET_SIZE do
        -- Check for header at current position
        if rxbuf:byte(0) ~= HEADER1 or rxbuf:byte(1) ~= HEADER2 then
            -- Search for header
            local found = nil
            for i = 1, rxbuf.length - 2 do
                if rxbuf:byte(i) == HEADER1 and rxbuf:byte(i+1) == HEADER2 then
                    found = i
                    break
                end
            end

            if not found then
                -- No header found, keep last byte in case it's 117
                if rxbuf.length > 1 then
                    rxbuf:discard(rxbuf.length - 1)
                end
                return
            end

            rxbuf:discard(found)

            if rxbuf.length < PACKET_SIZE then
                return
            end
        end

        -- Verify CRC
        if not verify_crc(rxbuf, 0) then
            -- CRC failed - search for next header within this packet
            local found = nil
            for i = 2, PACKET_SIZE - 2 do
                if rxbuf:byte(i) == HEADER1 and rxbuf:byte(i+1) == HEADER2 then
                    found = i
                    break
                end
            end

            if found then
                rxbuf:discard(found)
            else
                -- Check if last byte is 117 (potential split header)
                if rxbuf:byte(PACKET_SIZE - 1) == HEADER1 then
                    rxbuf:discard(PACKET_SIZE - 1)
                else
                    rxbuf:discard(PACKET_SIZE)
                end
            end

            goto continue
        end

        -- Extract floats
        local f = extract_floats(rxbuf, 2)

        -- Map to calibration struct (matches Arduino exactly)
        cal.accel_zerog[0] = f[0]
        cal.accel_zerog[1] = f[1]
        cal.accel_zerog[2] = f[2]

        cal.gyro_zerorate[0] = f[3]
        cal.gyro_zerorate[1] = f[4]
        cal.gyro_zerorate[2] = f[5]

        cal.mag_hardiron[0] = f[6]
        cal.mag_hardiron[1] = f[7]
        cal.mag_hardiron[2] = f[8]

        cal.mag_field = f[9]

        cal.mag_softiron[0] = f[10]
        cal.mag_softiron[1] = f[13]
        cal.mag_softiron[2] = f[14]
        cal.mag_softiron[3] = f[13]
        cal.mag_softiron[4] = f[11]
        cal.mag_softiron[5] = f[15]
        cal.mag_softiron[6] = f[14]
        cal.mag_softiron[7] = f[15]
        cal.mag_softiron[8] = f[12]

        printCalibration()
        rxbuf:discard(PACKET_SIZE)

        ::continue::
    end
end



--function receiveCalibrationChunk(ptr)
--    -- Validate we have data
--    if not ptr or #ptr == 0 then
--      return
--    end
--
--
--    for i = 0, #ptr - 1 do
--        local b = ffi.cast("const uint8_t*", ptr)[i]
--
--        -- header sync
--        if calcount == 0 and b ~= HEADER1 then goto continue end
--        if calcount == 1 and b ~= HEADER2 then calcount = 0; goto continue end
--
--        caldata[calcount] = b
--        calcount = calcount + 1
--
--        if calcount < 68 then
--            goto continue
--        end
--
--        -- got full frame
--        local crc = 0xFFFF
--        for j = 0, 67 do crc = crc16_update(crc, caldata[j]) end
--        if crc == 0 then
--            -- good data
--            ffi.copy(cal, caldata + 2, ffi.sizeof(cal))
--            printCalibration()
--            calcount = 0
--        else
--            -- resync search inside buffer
--            local newcount = 0
--            for j = 2, 66 do
--                if caldata[j] == HEADER1 and caldata[j+1] == HEADER2 then
--                    newcount = 68 - j
--                    ffi.copy(caldata, caldata + j, newcount)
--                    break
--                end
--            end
--
--            if newcount > 0 then
--                calcount = newcount
--            elseif caldata[67] == HEADER1 then
--                caldata[0] = HEADER1
--                calcount = 1
--            else
--                calcount = 0
--            end
--        end
--        ::continue::
--    end
--end

blim.bridge.pty_on_data(receiveCalibrationChunk)

----------------------------------------------------------------------
-- TTY MotionCal raw data writer
----------------------------------------------------------------------

-- Define C struct matching the binary layout
ffi.cdef[[
  typedef struct {
      float accel_x, accel_y, accel_z;
      float gyro_x,  gyro_y,  gyro_z;
      float mag_x,   mag_y,   mag_z;
  } imu_data_t;
]]

-- Allocate once, then reuse
local imu_ptr = ffi.new("imu_data_t[1]")

-- Pre-allocate result table (IMPORTANT: reused on every call)
local result = {
    accel = { x = 0, y = 0, z = 0 },
    gyro  = { x = 0, y = 0, z = 0 },
    mag   = { x = 0, y = 0, z = 0 }
}

--- Parse IMU data with zero allocation
-- IMPORTANT: Returns the SAME table every time (for performance
-- ~0.1-0.5 µs per call at 50 Hz = 0.025-0.25% CPU)
-- @param data string Binary data (36 bytes)
-- @return table IMU data (reused table - copy if you need to store it)
function parse_imu_data(data)
    --  little-endian byte order
    --  4-byte IEEE 754 float (repeated 9 times)

    -- Validate data length BEFORE FFI operations
    if #data ~= 36 then
        error(string.format("Invalid IMU data length: expected 36 bytes, got %d", #data))
        return nil
    end

    -- Copy binary data directly into C struct (fast!)
    ffi.copy(imu_ptr, data, 36)
    local imu = imu_ptr[0]

    -- Update the reused table in-place
    result.accel.x = imu.accel_x
    result.accel.y = imu.accel_y
    result.accel.z = imu.accel_z
    result.gyro.x = imu.gyro_x
    result.gyro.y = imu.gyro_y
    result.gyro.z = imu.gyro_z
    result.mag.x = imu.mag_x
    result.mag.y = imu.mag_y
    result.mag.z = imu.mag_z

    return result
end

--- Subscribe
blim.subscribe {
    services = {
        {
            service = "FF10",
            chars = { "FF11" }
        }
    },

    Mode = "EveryUpdate",
    --MaxRate = 0,
    Callback = function(record)
        local data = record.Values["ff11"]
        local imu = parse_imu_data(data)

        -- Print for debug: Accel (m/s²), Gyro (dps), Mag (µT)
        --print(string.format( "Accel: %.2f,%.2f,%.2f | Gyro: %.2f,%.2f,%.2f | Mag: %.2f,%.2f,%.2f",
        -- imu.accel.x, imu.accel.y, imu.accel.z,
        --          imu.gyro.x, imu.gyro.y, imu.gyro.z,
        --          imu.mag.x, imu.mag.y, imu. mag.z))

        -- Send to MotionCal via pty
        -- Format: "Raw:accelX,accelY,accelZ,gyroX,gyroY,gyroZ,magX,magY,magZ"

        -- There is no place with a formal declaration in the MotionCal format, but in some forums:
        --> Accel in 'raw' format 2^13 (8192) integers
        --   Gyroscope in Degrees/s rounded to integers
        --   Mag in microteslas * 10
        --
        --> so I assume, it assumes data as I mentioned,
        --   accel +/- 2^13 int, gyro degrees/s * 16,
        --   mag *10...though, a confirmation would be great.`
        --(https://github.com/PaulStoffregen/MotionCal/issues/12)

        blim.bridge.pty_write(string.format("Raw:%d,%d,%d,%d,%d,%d,%d,%d,%d\r\n",
          imu.accel.x * 8192/9.8, imu.accel.y * 8192/9.8, imu.accel.z * 8192/9.8,
          imu.gyro.x * 57.29577793 * 16, imu.gyro.y * 57.29577793 * 16, imu.gyro.z * 57.29577793 * 16,
          imu.mag.x * 10, imu.mag.y * 10, imu.mag.z * 10)
        )
    end
}

-- Print header information after successful subscription
print("\n===  MotionCal Bridge  ===\n")
print(string.format("Device: %s", blim.device.address))

-- Display bridge information if available
if blim.bridge.tty_name() and blim.bridge.tty_name() ~= "" then
    print(string.format("TTY: %s", blim.bridge.tty_name()))
    if blim.bridge.tty_symlink() and blim.bridge.tty_symlink() ~= "" then
        print(string.format("Symlink: %s", blim.bridge.tty_symlink()))
    end
    print("")
end