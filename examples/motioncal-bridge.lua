--- MotionCal Bridge
-- Bidirectional bridge: BLE IMU sensor ↔ MotionCal desktop app via PTY.
-- Receives 50Hz IMU data (accel/gyro/mag), forwards to MotionCal,
-- parses calibration data responses.
-- @module motioncal-bridge
-- @see https://learn.adafruit.com/adafruit-sensorlab-magnetometer-calibration/magnetic-calibration-with-motioncal/

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

--- Update CRC16 with one byte (MODBUS polynomial 0xA001).
-- @param crc Current CRC value
-- @param b Byte to process (0-255)
-- @return Updated 16-bit CRC value
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

--- Verify CRC16 checksum for packet.
-- @param buf Buffer containing packet data
-- @param offset Starting offset of packet in buffer
-- @return true if CRC valid (computed CRC == 0)
local function verify_crc(buf, offset)
    local crc = 0xFFFF
    for i = 0, PACKET_SIZE - 1 do
        crc = crc16_update(crc, buf:byte(offset + i))
    end
    return crc == 0
end

--- Extract 16 floats from buffer via FFI memory copy.
-- @param buf Buffer containing float data
-- @param offset Starting offset in buffer
-- @return float[16] cdata array (64 bytes, little-endian IEEE 754)
local function extract_floats(buf, offset)
    local floats = ffi.new("float[16]")
    ffi.copy(floats, buf.data + buf.offset + offset, 64)
    return floats
end

--- Print IMU calibration parameters.
-- Displays: accel zero-g (m/s²), gyro zero-rate (rad/s),
-- mag hard-iron (µT), mag field (µT), mag soft-iron matrix (3×3).
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

--- Process calibration data from MotionCal via PTY.
-- Streaming protocol: header sync, CRC validation, auto-resync.
-- Packet: [117, 84, 64B data, 2B CRC16] = 68 bytes.
-- @param ptr Raw binary data chunk from PTY
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

        -- Soft-iron matrix: corrects magnetic distortion from nearby ferromagnetic materials.
        -- The correction is a symmetric 3x3 matrix because magnetic permeability is symmetric
        -- (physics: the material's response to magnetic fields is the same in both directions).
        -- Arduino protocol exploits this symmetry to send only 6 values instead of 9:
        --   [0 1 2]     [f10 f13 f14]
        --   [1 4 5]  =  [f13 f11 f15]  (symmetric: [1]=[3], [2]=[6], [5]=[7])
        --   [2 5 8]     [f14 f15 f12]
        cal.mag_softiron[0] = f[10]
        cal.mag_softiron[1] = f[13]
        cal.mag_softiron[2] = f[14]
        cal.mag_softiron[3] = f[13] -- Mirror of [1]
        cal.mag_softiron[4] = f[11]
        cal.mag_softiron[5] = f[15]
        cal.mag_softiron[6] = f[14] -- Mirror of [2]
        cal.mag_softiron[7] = f[15] -- Mirror of [5]
        cal.mag_softiron[8] = f[12]

        printCalibration()
        rxbuf:discard(PACKET_SIZE)

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

--- Parse IMU binary data (zero-allocation).
-- Format: 9×float (36 bytes, little-endian IEEE 754).
-- WARNING: Returns SAME table every call (reused for performance).
-- Copy values if storage needed.
-- @param data Binary data (exactly 36 bytes)
-- @return table with accel/gyro/mag fields (reused, ~0.1-0.5µs)
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

        -- MotionCal expects:
        --   Accel: ±2g range as 16-bit signed integers (±8192 = ±2g)
        --   Gyro: degrees/s * 16 (fixed-point with 4 fractional bits)
        --   Mag: microtesla * 10 (single decimal precision)
        --  See https://github.com/PaulStoffregen/MotionCal/issues/12
        blim.bridge.pty_write(string.format("Raw:%d,%d,%d,%d,%d,%d,%d,%d,%d\r\n",
          -- Convert m/s² to ±2g raw format (8192 LSB/g, 9.8 m/s² = 1g)
          imu.accel.x * 8192/9.8, imu.accel.y * 8192/9.8, imu.accel.z * 8192/9.8,

          -- Convert rad/s to deg/s (* 180/π ≈ 57.29577793) then to raw (* 16 LSB/deg/s)
          imu.gyro.x * 57.29577793 * 16, imu.gyro.y * 57.29577793 * 16, imu.gyro.z * 57.29577793 * 16,

          -- Convert µT to MotionCal format (µT * 10)
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