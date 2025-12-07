# Environmental Sensor Hub - BLE Test Peripheral

A realistic IoT environmental monitoring device firmware that naturally demonstrates comprehensive BLE features. Use this device to test BLE CLI tools against real-world patterns.

## Device Concept

A battery-powered environmental monitoring device with:
- Temperature & humidity sensing
- Battery monitoring
- Configurable sampling and alert thresholds
- Command/control interface for device management
- Diagnostic logging

## Services

| Service | UUID | Purpose |
|---------|------|---------|
| **DeviceInfoService** | 0x180A | Standard device info (from blex library) |
| **SensorService** | 0x181A | Environmental + battery readings |
| **ControlService** | E5700001-7BAC-429A-B4CE-57FF900F479D | Device management & control |

## BLE Features Covered

### Advertising
| Feature | Value                | Description |
|---------|----------------------|-------------|
| **Device Name** | Blim ESH             | Short name in advertising |
| **Long Name** | Blim Sensor Hub      | Complete local name |
| **Manufacturer Data** | 0xFFFF + TLV payload | Vendor-specific data with device type |
| **TX Power** | 3 dBm                | Transmission power level |
| **Appearance** | Sensor (0x0540)      | GAP appearance category |
| **Advertising Interval** | 100-200 ms           | Min/max intervals |
| **Service UUIDs** | 0x181A, E5700001-... | Advertised services |

### Characteristics
| Feature | Characteristic | Service |
|---------|---------------|---------|
| **Read** | All sensors, Config, DiagLog | Sensor, Control |
| **Write** | Config (with response, encrypted) | Control |
| **Write No Response** | Command Register | Control |
| **Notify** | Temp, Humidity, Battery, Response | Sensor, Control |
| **Indicate** | Alert | Control |
| **Read + Notify** | All sensors | Sensor |
| **Encrypted Write** | Config | Control |
| **Long Read (512 bytes)** | Diagnostic Log | Control |
| **16-bit SIG UUID** | 0x181A, 0x2A6E, etc. | Sensor |
| **128-bit vendor UUID** | E5700001-... | Control |
| **CCCD (0x2902)** | All Notify/Indicate chars (auto) | Sensor, Control |
| **Presentation Format (0x2904)** | Temp, Humidity, Battery | Sensor |
| **Aggregate Format (0x2905)** | Environment Summary | Sensor |
| **User Description (0x2901)** | All characteristics | All |
| **onSubscribe callback** | Response (notify) | Control |
| **Packed structs** | CommandPacket, AlertPacket, ConfigData | Control |

## Characteristic Details

### SensorService (0x181A)

| Characteristic | UUID | Properties | Description |
|---------------|------|------------|-------------|
| Temperature | 0x2A6E | Read, Notify | Signed int16, hundredths of °C |
| Humidity | 0x2A6F | Read, Notify | Unsigned int16, hundredths of % |
| Battery Level | 0x2A19 | Read, Notify | Unsigned int8, percentage |
| Environment Summary | E5710001-... | Read, Notify | Aggregated: temp (int16) + humidity (uint16) + battery (uint8) |

### ControlService (E5700001-...)

| Characteristic | UUID Suffix | Properties | Description |
|---------------|-------------|------------|-------------|
| Command Register | ...0002 | Write No Response | Fast command input |
| Command Response | ...0003 | Notify | Command acknowledgements |
| Alert | ...0004 | Indicate | Critical alerts (requires ACK) |
| Config | ...0005 | Read, Write (encrypted, with response) | Protected device settings |
| Diagnostic Log | ...0006 | Read | 512-byte log buffer (long read) |

## Command Protocol

### Command Register Format (Write No Response)

```
[0]: Command ID
[1-N]: Command parameters
```

### Commands

| ID | Command | Parameters | Description |
|----|---------|------------|-------------|
| 0x01 | Start Sampling | - | Enable sensor sampling |
| 0x02 | Stop Sampling | - | Disable sensor sampling |
| 0x03 | Set Interval | uint16 ms | Set sampling interval (100-60000ms) |
| 0x04 | Set Alert Thresholds | int16 high, int16 low | Temperature thresholds |
| 0x05 | Request Diag Dump | - | Prepare diagnostic log |
| 0x06 | Clear Diag Log | - | Clear diagnostic buffer |
| 0xFF | Reset | - | Reboot device |

### Response Format (Notify)

```
[0]: Command ID (echo)
[1]: Status (0=OK, 1=ERROR, 2=INVALID_PARAM, 3=INVALID_CMD)
[2-N]: Response data
```

### Alert Format (Indicate)

```
[0]: Alert type (1=TEMP_HIGH, 2=TEMP_LOW, 3=BATTERY_LOW, 4=ERROR)
[1]: Severity (0=info, 1=warning, 2=critical)
[2-3]: Value (int16)
[4-7]: Timestamp (uint32, uptime in ms)
```

## Hardware Requirements

- **Board**: Unexpected Maker FeatherS3 (ESP32-S3)
- **Framework**: Arduino
- **Platform**: PlatformIO

## Building

```bash
cd firmware
pio run
```

## Flashing

```bash
pio run -t upload
```

## Monitor Serial Output

```bash
pio device monitor
```

## Memory Usage

```
RAM:   13.2% (43368 / 327680 bytes)
Flash:  8.8% (575733 / 6553600 bytes)
```

## Dependencies

- [blex](https://github.com/srgg/blex) - Zero-cost BLE abstraction library
- [NimBLE-Arduino](https://github.com/h2zero/NimBLE-Arduino) - Lightweight BLE stack

## Usage Examples

### Scan for device
```bash
# Scan for device with MAC address e20e664a4716aba3abc6b9a0329b5b2e
blim scan --allow e20e664a4716aba3abc6b9a0329b5b2e
```

### Read single sensor values
```bash
# Read all sensor values
blim read e20e664a4716aba3abc6b9a0329b5b2e 2A6E,2A6F,2A19 --hex  # Temperature,Humidity,Battery  
```

### Read with polling (--watch)
```bash
blim read e20e664a4716aba3abc6b9a0329b5b2e 2A6E --hex --watch=5s
```
### Subscribe to notifications (Notify)
```bash
blim subscribe e20e664a4716aba3abc6b9a0329b5b2e 2A6E,2A6F,2A19 --hex
```

### Subscribe to indications (Indicate)
```bash
# Alert characteristic - requires acknowledgement from client
blim subscribe e20e664a4716aba3abc6b9a0329b5b2e E5700004-7BAC-429A-B4CE-57FF900F479D --hex

### Send command (start sampling)
```bash
blim write e20e664a4716aba3abc6b9a0329b5b2e E5700002-7BAC-429A-B4CE-57FF900F479D 01
```

### Read diagnostic log (long read)
```bash
blim read e20e664a4716aba3abc6b9a0329b5b2e E5700006-7BAC-429A-B4CE-57FF900F479D
```
