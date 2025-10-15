-- Bridge for MotionCal calibration
-- (https://learn.adafruit.com/adafruit-sensorlab-magnetometer-calibration/magnetic-calibration-with-motioncal/)
--
-- High-frequency sensor data processing at 50Hz

local ffi = require("ffi")
--local buffer = require("string.buffer")
local bit = require("bit")

local function crc16_update(crc, a)
  crc = bit.bxor(crc, a)
  for _ = 1, 8 do
    if bit.band(crc, 1) ~= 0 then
      crc = bit.rshift(crc, 1)
      crc = bit.bxor(crc, 0xA001)
    else
      crc = bit.rshift(crc, 1)
    end
  end
  return crc
end


----------------------------------------------------------------------
-- TTY MotionCal calibration reader
----------------------------------------------------------------------

ffi.cdef[[
typedef struct {
    float accel_zerog[3];
    float gyro_zerorate[3];
    float mag_hardiron[3];
    float mag_field;
    float mag_softiron[9];
} ImuCal;
]]

local cal = ffi.new("ImuCal")
local caldata = ffi.new("uint8_t[68]")
local calcount = 0
local HEADER1, HEADER2 = 117, 84

-- CRC-16/IBM (same as Arduino's _crc16_update)
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

-- Prints the parsed calibration nicely
local function printCalibration()
    local f3 = function(a) return string.format("%.4f %.4f %.4f", a[0], a[1], a[2]) end
    print("IMU calibration:")
    print("  Accel zero-g:", f3(cal.accel_zerog))
    print("  Gyro  zero-rate:", f3(cal.gyro_zerorate))
    print("  Mag hard-iron:", f3(cal.mag_hardiron))
    print(string.format("  Mag field: %.4f", cal.mag_field))
    print("  Mag soft-iron matrix:")
    for i = 0, 8, 3 do
        print(string.format("    %.4f %.4f %.4f",
            cal.mag_softiron[i], cal.mag_softiron[i+1], cal.mag_softiron[i+2]))
    end
end

function receiveCalibrationChunk(ptr)
    -- Validate we have data
    if not ptr or #ptr == 0 then
      return
    end


    for i = 0, #ptr - 1 do
        local b = ffi.cast("const uint8_t*", ptr)[i]

        -- header sync
        if calcount == 0 and b ~= HEADER1 then goto continue end
        if calcount == 1 and b ~= HEADER2 then calcount = 0; goto continue end

        caldata[calcount] = b
        calcount = calcount + 1

        if calcount < 68 then
            goto continue
        end

        -- got full frame
        local crc = 0xFFFF
        for j = 0, 67 do crc = crc16_update(crc, caldata[j]) end
        if crc == 0 then
            -- good data
            ffi.copy(cal, caldata + 2, ffi.sizeof(cal))
            printCalibration()
            calcount = 0
        else
            -- resync search inside buffer
            local newcount = 0
            for j = 2, 66 do
                if caldata[j] == HEADER1 and caldata[j+1] == HEADER2 then
                    newcount = 68 - j
                    ffi.copy(caldata, caldata + j, newcount)
                    break
                end
            end

            if newcount > 0 then
                calcount = newcount
            elseif caldata[67] == HEADER1 then
                caldata[0] = HEADER1
                calcount = 1
            else
                calcount = 0
            end
        end
        ::continue::
    end
end

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
        print(string.format( "Accel: %.2f,%.2f,%.2f | Gyro: %.2f,%.2f,%.2f | Mag: %.2f,%.2f,%.2f",
         imu.accel.x, imu.accel.y, imu.accel.z,
                  imu.gyro.x, imu.gyro.y, imu.gyro.z,
                  imu.mag.x, imu.mag.y, imu. mag.z))

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