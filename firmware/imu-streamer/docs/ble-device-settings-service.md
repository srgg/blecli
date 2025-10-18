# BLE Device Settings Service (UUID: 0xFF20)

The **BLE Device Settings Service** is a custom GATT service that provides persistent configuration management for device settings and calibration data.
Using the 16-bit UUID `0xFF20` (vendor-specific range), it enables **read/write access to device configuration via JSON**, with support for **partial updates** (deep merge), **automatic persistence to NVS** (Non-Volatile Storage), and **real-time state notification**.

This architecture is designed to be **extensible** — while initially supporting IMU calibration, the JSON schema uses subsystem namespaces (`imu`, `servo`, etc.) to accommodate future configuration types without BLE API changes.

## Characteristics

### Configuration Data Characteristic (UUID: 0xFF21)

**Properties:** `READ`, `WRITE`
**Format:** JSON string
**Max Size:** 512 bytes
**Persistence:** Auto-saves to NVS on write

This characteristic stores all device configuration in a structured JSON format with deep merge semantics. When writing, only the fields present in the JSON payload are updated — all other fields remain unchanged. This enables efficient partial updates without requiring clients to read-modify-write the entire configuration.

#### JSON Schema

```json
{
  "settings": {
    "apply_calibration": false
  },
  "imu": {
    "accel": {
      "zerog": [0.0, 0.0, 0.0]
    },
    "gyro": {
      "zerorate": [0.0, 0.0, 0.0]
    },
    "mag": {
      "hardiron": [0.0, 0.0, 0.0],
      "field": 50.0,
      "softiron": [
        [1.0, 0.0, 0.0],
        [0.0, 1.0, 0.0],
        [0.0, 0.0, 1.0]
      ]
    }
  },
  "metadata": {
    "version": 1,
    "timestamp": 0
  }
}
```

#### Configuration Fields

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `settings.apply_calibration` | boolean | Apply calibration coefficients to IMU stream (true) or stream raw data (false) | `false` |
| `imu.accel.zerog` | float[3] | Accelerometer zero-g offset [X, Y, Z] in m/s² | `[0, 0, 0]` |
| `imu.gyro.zerorate` | float[3] | Gyroscope zero-rate offset [X, Y, Z] in dps | `[0, 0, 0]` |
| `imu.mag.hardiron` | float[3] | Magnetometer hard-iron offset [X, Y, Z] in µT | `[0, 0, 0]` |
| `imu.mag.field` | float | Expected magnetic field magnitude in µT | `50.0` |
| `imu.mag.softiron` | float[3][3] | Soft-iron correction matrix (3×3, row-major) | Identity matrix |
| `metadata.version` | integer | JSON schema version | `1` |
| `metadata.timestamp` | integer | Last update timestamp (milliseconds since boot) | auto |

#### Deep Merge Semantics

The characteristic implements **deep merge** for partial updates. Only the fields specified in the write payload are updated; all other fields retain their current values.

**Example: Partial Update**

Write only the magnetometer hard-iron calibration:
```json
{
  "imu": {
    "mag": {
      "hardiron": [-12.5, 8.3, -4.7]
    }
  }
}
```
All other fields (accel, gyro, softiron, settings, etc.) remain unchanged.

**Example: Update Multiple Fields**

```json
{
  "settings": {
    "apply_calibration": true
  },
  "imu": {
    "mag": {
      "hardiron": [-12.5, 8.3, -4.7],
      "field": 48.2
    }
  }
}
```
This updates the calibration toggle and magnetometer parameters, leaving accelerometer and gyroscope untouched.

#### Validation

- JSON payloads larger than 512 bytes are rejected
- Invalid JSON syntax returns an error (logged via serial)
- Partial JSON that doesn't match schema fields is safely ignored

### Settings State Characteristic (UUID: 0xFF22)

**Properties:** `READ`, `WRITE`, `NOTIFY`
**Format:** Single byte (bit field)
**Persistence:** Auto-saves to NVS on write

This characteristic exposes device state flags as a bit field, with automatic notification on changes. Clients can subscribe to notifications to receive real-time updates when settings change (via BLE write, control point commands, or programmatic changes).

#### State Flags (Bit Field)

| Bit | Flag | Description | Default |
|-----|------|-------------|---------|
| 0 | `apply_calibration` | Apply calibration coefficients to IMU stream | `0` (disabled) |
| 1-7 | Reserved | Reserved for future use | `0` |

#### State Values

- `0x00` — Raw IMU data (no calibration applied)
- `0x01` — Calibrated IMU data (apply_calibration enabled)

#### Validation

- Reserved bits (1-7) must be 0; writes with reserved bits set trigger a warning but are still processed
- Empty writes (no data) are rejected
- State changes are automatically persisted to NVS and notify subscribed clients

### Control Point Characteristic (UUID: 0xFF23)

**Properties:** `WRITE`
**Format:** Single byte (command code)

This characteristic provides command execution for device-wide operations. Commands are validated and executed immediately, with results logged via serial output.

#### Commands

| Code | Command | Description |
|------|---------|-------------|
| `0x01` | Factory Reset | Reset all configuration to factory defaults (zero offsets, identity matrices, calibration disabled) and save to NVS |
| `0x02` | Reboot | Restart the ESP32 device immediately |

#### Response Codes (Reserved for Future Use)

The following response codes are defined for potential INDICATE support in future firmware versions:

| Code | Response | Description |
|------|----------|-------------|
| `0x00` | Success | Command executed successfully |
| `0x01` | Invalid Command | Unknown command code |
| `0x02` | Error | Execution failed |

#### Command Behavior

**Factory Reset (`0x01`):**
- Resets IMU calibration to identity/zero values
- Disables calibration application (`apply_calibration = false`)
- Saves configuration to NVS
- Notifies subscribed clients via Settings State characteristic (0xFF22)

**Reboot (`0x02`):**
- Immediately restarts the ESP32
- No response is sent (device disconnects)

**Invalid Command:**
- Unknown command codes are logged but do not affect device state

## BLE Descriptors

Each characteristic includes a **User Description Descriptor (0x2901)** to provide human-readable labels:

- **0xFF21:** `"Configuration data (JSON, supports partial updates, auto-saves)"`
- **0xFF22:** `"Settings state (Bit 0: apply calibration to stream)"`
- **0xFF23:** `"Control point (0x01=factory reset, 0x02=reboot)"`

## Calibration Workflow

### 1. Obtain Calibration Data

Use [MotionCal](https://www.pjrc.com/store/prop_shield.html) or a similar tool to generate calibration coefficients from raw IMU data.

### 2. Write Calibration via BLE

Update the configuration characteristic (0xFF21) with the calibration JSON:

```json
{
  "imu": {
    "accel": {
      "zerog": [0.12, -0.08, 0.05]
    },
    "gyro": {
      "zerorate": [0.3, -0.2, 0.1]
    },
    "mag": {
      "hardiron": [-12.5, 8.3, -4.7],
      "field": 48.2,
      "softiron": [
        [0.995, 0.003, -0.001],
        [0.003, 1.002, 0.004],
        [-0.001, 0.004, 1.003]
      ]
    }
  }
}
```

Configuration is automatically saved to NVS.

### 3. Enable Calibration

**Option A: Write to Settings State characteristic (0xFF22)**
```
Write: 0x01
```

**Option B: Write via Configuration Data characteristic (0xFF21)**
```json
{
  "settings": {
    "apply_calibration": true
  }
}
```

Both methods persist the change to NVS and notify subscribed clients.

### 4. Verify

- Read the IMU characteristic (0xFF11) to confirm that calibrated data is being streamed
- Subscribe to State notifications (0xFF22) to receive real-time updates when calibration is toggled

> All configuration changes persist across reboots via ESP32 NVS storage.

## Persistence and Factory Reset

### Non-Volatile Storage (NVS)

All configuration data is automatically persisted to ESP32 NVS:
- **Configuration writes** (0xFF21) auto-save after successful merge
- **State changes** (0xFF22) auto-save immediately
- **Factory reset** (0xFF23 command 0x01) saves defaults to NVS

Configuration is loaded from NVS on device boot. If no saved configuration exists, factory defaults are used.

### Factory Defaults

| Parameter | Factory Default |
|-----------|-----------------|
| `apply_calibration` | `false` (disabled) |
| Accelerometer offsets | `[0, 0, 0]` |
| Gyroscope offsets | `[0, 0, 0]` |
| Magnetometer hard-iron | `[0, 0, 0]` |
| Magnetometer field | `50.0 µT` |
| Magnetometer soft-iron | Identity matrix |

## BLE Best Practices

This service follows BLE specification best practices:

1. **Separate State and Control** — State flags (0xFF22) and commands (0xFF23) use separate characteristics
2. **Value Validation** — All writes are validated for size, format, and reserved bits
3. **Automatic Persistence** — Configuration changes auto-save to NVS without requiring explicit save commands
4. **Notifications** — State characteristic uses NOTIFY for real-time client updates
5. **Descriptors** — User Description descriptors provide human-readable context
6. **Deep Merge** — Partial updates minimize BLE traffic and simplify client implementation
7. **Extensible Schema** — JSON namespaces allow future configuration types without API changes

## Integration with IMU Stream

The `apply_calibration` flag controls whether calibration coefficients are applied to the IMU data stream (0xFF11).

- **Disabled (`0x00`):** IMU stream contains raw sensor readings
- **Enabled (`0x01`):** IMU stream contains calibrated data with offsets and corrections applied

This allows clients to:
- Capture raw data for calibration analysis
- Stream calibrated data for applications requiring corrected measurements
- Toggle between raw and calibrated modes without reconnecting

## Example Client Pseudo-code

```python
# Connect to device
device = connect("BLIM-IMU")

# Read current configuration
config_json = device.read_characteristic(0xFF21)
print(f"Current config: {config_json}")

# Update magnetometer calibration (partial)
calibration = {
  "imu": {
    "mag": {
      "hardiron": [-12.5, 8.3, -4.7],
      "field": 48.2
    }
  }
}
device.write_characteristic(0xFF21, json.dumps(calibration))

# Enable calibration
device.write_characteristic(0xFF22, bytes([0x01]))

# Subscribe to state notifications
def on_state_change(state_byte):
    print(f"Calibration: {'ON' if state_byte & 0x01 else 'OFF'}")

device.subscribe_characteristic(0xFF22, on_state_change)

# Stream calibrated IMU data
device.subscribe_characteristic(0xFF11, on_imu_data)
```

## Memory and Performance

- **Static Allocation:** All buffers use static allocation (512 bytes for JSON)
- **No Heap Usage:** No dynamic memory allocation for configuration management
- **NVS Overhead:** ~100 bytes per configuration write to flash
- **BLE MTU:** Handles configurations up to 512 bytes with automatic BLE fragmentation

## Future Extensibility

The JSON schema is designed for future expansion:

```json
{
  "settings": { ... },
  "imu": { ... },
  "servo": {
    "channels": [
      {"min": 1000, "max": 2000, "center": 1500}
    ]
  },
  "metadata": { ... }
}
```

New subsystems can be added without changing the BLE service structure or characteristic UUIDs.