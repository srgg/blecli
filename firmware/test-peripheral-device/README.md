# ESP32-S3 BLE Test Peripheral Device

A comprehensive BLE peripheral firmware for ESP32-S3 that simulates multiple services and characteristics for testing and debugging BLE applications.

## Hardware Requirements

- **Board**: Unexpected Maker FeatherS3
- **Framework**: Arduino
- **Platform**: PlatformIO

## Features

This firmware implements a full-featured BLE peripheral with the following services:

### 1. Device Information Service (UUID: 180A)
Standard BLE service providing device metadata:
- Manufacturer Name: "Unnamed Maker"
- Model Number: "ESP32-S3-DevKit-1"
- Serial Number: "TEST-001"
- Firmware Version: "1.0.0"
- Hardware Version: "1.0"

### 2. Battery Service (UUID: 180F)
Simulates battery level monitoring:
- Battery Level characteristic (read/notify)
- Automatically decrements from 100% to 0%, then resets
- Notifications every 5 seconds

### 3. Heart Rate Service (UUID: 180D)
Simulates a heart rate monitor:
- Heart Rate Measurement characteristic (read/notify)
- Varies between 60-100 bpm
- Notifications every 1 second

### 4. Environmental Sensing Service (UUID: 181A)
Simulates environmental sensors:
- Temperature characteristic (read/notify) - varies 20-25°C
- Humidity characteristic (read/notify) - varies 45-65%
- Temperature notifications every 2 seconds
- Humidity notifications every 3 seconds

### 5. Nordic UART Service (UUID: 6E400001-...)
Serial-like communication service:
- TX characteristic (notify) - sends data to client
- RX characteristic (write) - receives data from client
- Echo functionality: received data is echoed back with "Echo: " prefix

### 6. Custom Test Service (UUID: 12345678-...)
Comprehensive test service with various characteristic types:
- Read-Only characteristic - returns "ReadOnlyValue"
- Write-Only characteristic - logs received data
- Notify characteristic - sends counter updates every second
- Read/Write characteristic - supports both operations

## Building and Flashing

### Prerequisites
1. Install [PlatformIO](https://platformio.org/)
2. Connect ESP32-S3 board via USB

### Build and Upload
```bash
cd firmware/test-peripheral-device
pio run --target upload
```

### Monitor Serial Output
```bash
pio device monitor
```

Or use the combined upload and monitor:
```bash
pio run --target upload && pio device monitor
```

## Usage

### Initial Setup
1. Flash the firmware to your ESP32-S3
2. Open serial monitor at 115200 baud
3. The device will start advertising as "ESP32-S3-TestPeripheral"

### Connecting
Use any BLE scanner or the blecli tool to connect:

```bash
# Scan for the device
blecli scan -n ESP32-S3-TestPeripheral

# Connect and explore services
blecli connect <device-address>
```

### Testing Characteristics

**Reading Device Information:**
```bash
blecli read <device> 180A 2A29  # Manufacturer Name
blecli read <device> 180A 2A24  # Model Number
```

**Subscribing to Notifications:**
```bash
blecli notify <device> 180F 2A19  # Battery Level
blecli notify <device> 180D 2A37  # Heart Rate
blecli notify <device> 181A 2A6E  # Temperature
```

**UART Communication:**
```bash
blecli write <device> 6E400001-B5A3-F393-E0A9-E50E24DCCA9E 6E400002-B5A3-F393-E0A9-E50E24DCCA9E "Hello"
blecli notify <device> 6E400001-B5A3-F393-E0A9-E50E24DCCA9E 6E400003-B5A3-F393-E0A9-E50E24DCCA9E
```

## Serial Monitor Output

When running, the device logs:
- Connection/disconnection events
- Service initialization
- Notification values (battery, heart rate, temperature, humidity, counter)
- Received UART/Write data

Example output:
```
=== ESP32-S3 BLE Test Peripheral ===
Manufacturer: Unnamed Maker
Model: ESP32-S3-DevKit-1
Firmware: 1.0.0

Device Information Service started
Battery Service started
Heart Rate Service started
Environmental Sensing Service started
UART Service started
Custom Test Service started
BLE advertising started
Device name: ESP32-S3-TestPeripheral
Ready for connections!

Client connected
Battery: 100%
Heart Rate: 75 bpm
Temperature: 22.45°C
Humidity: 56.32%
Test Notify: Counter: 42
```

## Troubleshooting

### Device not advertising
- Check serial output for initialization errors
- Verify USB connection and power
- Reset the board (EN button)

### Cannot connect
- Ensure Bluetooth is enabled on the client device
- Check if device is already connected to another client
- Move closer to reduce interference

### Notifications not received
- Verify you've enabled notifications on the characteristic
- Check client-side notification subscription
- Monitor serial output for notification attempts

## License

Same as parent project.