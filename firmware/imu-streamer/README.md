# IMU Streamer

The **ESP32-S3 BLE IMU streaming firmware** provides real-time streaming of 9-axis motion data—including accelerometer, gyroscope, and magnetometer readings—over Bluetooth Low Energy at 50 Hz, making it ideal for motion capture, sensor calibration (MotionCal), and orientation tracking applications.

It integrates the **LSM6DSOX 6-axis IMU** (accelerometer + gyroscope) and the **LIS3MDL 3-axis magnetometer**, streaming motion data through a **[custom BLE IMU Service (0xFF10)](#ble-imu-service-uuid-0xff10)**.  
To complement the live sensor stream, the firmware also exposes comprehensive device metadata — including firmware version and source commit — via the **[BLE Device Information Service](#ble-device-information-service-uuid-0x180a)**.

### Bill of Materials (BOM)

| Item | Component | Description                                                                                                                                                        | Notes / Links |
|------|------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------------|
| 1 | **Main Board** | [Unexpected Maker FeatherS3](https://www.adafruit.com/product/5399?srsltid=AfmBOoo-aelaqileYJxyt2K2CcK8VVmg5PJ5fkiZwHbSfYKA0XC3LQGH)                               | ESP32-S3-based development board with native USB and integrated LiPo charger |
| 2 | **9-DOF Sensor Module** | [Adafruit LSM6DSOX + LIS3MDL Precision 9-DOF IMU Breakout](https://www.adafruit.com/product/4438?srsltid=AfmBOoquINBeIyNSaaiiawB9ImDCyLVHLenjeEX96Q5F4SeESJs0Rd4E) | Combines LSM6DSOX (6-axis accelerometer + gyroscope) and LIS3MDL (3-axis magnetometer) |
| 3 | **I²C Connection Cable** | [JST-SH 4-pin cable](https://www.adafruit.com/product/4399?gad_source=1&gad_campaignid=21079227318&gbraid=0AAAAADx9JvSBs8V2LypUZaqi4mE9YQl7p&gclid=Cj0KCQjw6bfHBhDNARIsAIGsqLgpJrA1a9DCmPXy7pZsAp7GksNP1LzPPEBQBwWid22Hp4dbLPqv18AaAtaiEALw_wcB) (Feather to sensor module)                                                                                                                  | Connects FeatherS3 I²C bus to IMU breakout (SDA=GPIO8, SCL=GPIO9, 3V3, GND) |
| 4 | **Power Source** | LiPo battery or USB-C                                                                                                                                              | Optional — FeatherS3 supports both USB and LiPo operation |


### Building and Uploading

```bash
# Build
pio run -e um_feathers3

# Upload
pio run -t upload -e um_feathers3

# Monitor serial output
pio device monitor -e um_feathers3
```

## BLE Services

### BLE Advertising

The firmware uses **optimized dual-mode BLE advertising** to ensure fast and efficient device discovery while minimizing broadcast overhead.  
By distributing key metadata between **passive advertisements** (`ADV_IND`) and **active scan responses** (`SCAN_RSP`), the system enables quick identification of available devices during scanning, while exposing detailed information only when requested.

#### Passive Advertising (ADV_IND)
Contains the minimal data required for efficient discovery:
- **Short Name:** `BLIM-IMU`
- **Service UUID:** `0xFF10` (IMU Service)

#### Active Scan Response (SCAN_RSP)
Provides extended information when a scanning client actively requests it:
- **Complete Name:** `BLIM IMU Stream`
- **Service UUID:** `0x180A` (Device Information Service)

> This dual-mode approach optimizes both **discovery latency** and **advertising bandwidth**, allowing BLE scanners to filter devices based on services or names without unnecessary data transmission.


### BLE Device Information Service (UUID: 0x180A)

The **BLE Device Information Service** is a standard GATT service that provides static metadata about the device to connected clients.  
It enables applications to easily identify the manufacturer, model, firmware version, and hardware revision — without requiring any custom protocol commands.  
Because BLE tools and libraries universally recognize this service, it allows for automatic device identification, version tracking, and compatibility validation across different platforms.

All characteristic values are automatically populated by `version.py` and passed as preprocessor build flags (see [Versioning and Device Configuration](#versioning-and-device-configuration)).  
This ensures that every build consistently exposes accurate device information over BLE.

| Attribute             | Example Value  | Build Flag          | Characteristic UUID |
|-----------------------|----------------|----------------------|---------------------|
| Manufacturer Name     | BLIMCo         | `MANUFACTURER_NAME`  | 0x2A29 |
| Model Number          | um_feathers3   | `MODEL_NUMBER`       | 0x2A24 |
| Serial Number         | FS3-001        | `SERIAL_NUMBER`      | 0x2A25 |
| Hardware Revision     | 1.0            | `HARDWARE_VERSION`   | 0x2A27 |
| Firmware Version      | 1.0.0          | `FIRMWARE_VERSION`   | 0x2A26 |
| Software Revision     | b96b420        | `SOFTWARE_REVISION`  | 0x2A28 |

> Each characteristic ensures that connected clients receive **standardized and verifiable device information**, allowing firmware and hardware versions to be reliably identified over Bluetooth Low Energy.


### BLE IMU Service (UUID: 0xFF10)

The **BLE IMU Service** is a custom, vendor-specific GATT service that streams real-time 9-axis motion data over Bluetooth Low Energy.  
Using the 16-bit UUID `0xFF10` (vendor-specific range), it provides a single characteristic optimized for **efficient, high-frequency data transmission** at **50 Hz**.

#### IMU Characteristic (UUID: 0xFF11)

**Properties:** `READ`, `NOTIFY`  
**Update Rate:** 50 Hz (20 ms interval)  
**Payload Size:** 36 bytes (9 × IEEE-754 float32)

This characteristic transmits accelerometer, gyroscope, and magnetometer readings in a compact binary format designed for minimal BLE overhead and maximum throughput.

| Offset | Sensor        | Axis | Type    | Unit | Range               |
|--------|---------------|------|---------|------|---------------------|
| 0–3    | Accelerometer | X    | float32 | m/s² | ±4 g (±39.2 m/s²)   |
| 4–7    | Accelerometer | Y    | float32 | m/s² | ±4 g (±39.2 m/s²)   |
| 8–11   | Accelerometer | Z    | float32 | m/s² | ±4 g (±39.2 m/s²)   |
| 12–15  | Gyroscope     | X    | float32 | dps  | ±2000 dps           |
| 16–19  | Gyroscope     | Y    | float32 | dps  | ±2000 dps           |
| 20–23  | Gyroscope     | Z    | float32 | dps  | ±2000 dps           |
| 24–27  | Magnetometer  | X    | float32 | µT   | ±400 µT (±4 Gauss)  |
| 28–31  | Magnetometer  | Y    | float32 | µT   | ±400 µT (±4 Gauss)  |
| 32–35  | Magnetometer  | Z    | float32 | µT   | ±400 µT (±4 Gauss)  |

#### BLE Descriptors

To support standardized interpretation by BLE clients, the characteristic includes a complete set of **GATT descriptors**:

- **Presentation Format Descriptors (0x2904)** — define data types and measurement units:
  - **Accelerometer:** `float32`, unit `0x2713` (m/s²)
  - **Gyroscope:** `float32`, unit `0x2700` (unitless – degrees/second)
  - **Magnetometer:** `float32`, unit `0x2774` (tesla), exponent `–6` (µT)
- **Aggregate Format Descriptor (0x2905)** — indicates that the characteristic contains composite (multi-sensor) data.
- **User Description Descriptor (0x2901)** — provides a human-readable label:  
  `"IMU: Accel (m/s²) | Gyro (dps) | Mag (µT)"`

> Together, these descriptors ensure that BLE clients and visualization tools can automatically interpret IMU data with correct scaling, units, and structure — without requiring manual parsing or proprietary metadata.

## Versioning and Device Configuration

The **firmware** uses `version.py`, a PlatformIO pre-build hook, to automatically manage both firmware versioning and device-specific configuration before compilation. This ensures that each build is fully traceable, consistent, and tailored to the target board.

### Firmware Versioning

The script extracts the firmware version from Git tags, enforcing **[Semantic Versioning 2.0.0](https://semver.org)** (see [Semantic Versioning](#semantic-versioning) section) using the MAJOR.MINOR.PATCH format. It also retrieves the short (7-character) Git commit hash and checks for uncommitted changes, appending a `-dirty` suffix if needed. These values are injected as **preprocessor build flags** so the firmware can report its exact version at runtime:

- `FIRMWARE_VERSION` — semantic version from Git tags
- `SOFTWARE_REVISION` — short commit hash, including the `-dirty` suffix if the working tree has uncommitted changes

#### Semantic Versioning
```
1.2.3
│ │ │
│ │ └─ PATCH: Bug fixes
│ └─── MINOR: New features (backward-compatible)
└───── MAJOR: Breaking changes
```

**Guidelines for deciding version bumps:**

- **PATCH** – Bug fixes or small tweaks that do not affect the API or functionality
- **MINOR** – New features or performance improvements that remain backward-compatible
- **MAJOR** – Breaking changes or major new functionality requiring updates elsewhere

> Always bump the version based on the impact on users or dependent systems.

### Device-Specific Configuration

Device-specific settings—such as manufacturer, hardware version, serial numbers, and Bluetooth device names (advertised BLE names visible to other devices)—are stored in a **board-indexed dictionary**. The script automatically injects these values as **preprocessor build flags**:

```python
DEVICE_CONFIG = {
  "manufacturer": "BLIMCo",
  "hardware_version": "1.0",

  "boards": {
    "um_feathers3": {
      "serial_number": "FS3-001",
      "device_name": "BLIM IMU Stream",
      "device_name_short": "BLIM-IMU"
    }
  }
}
```

Making it easy to add new boards:
1. Add a new entry under boards in DEVICE_CONFIG 
2. Create a corresponding [env:...] section in platformio.ini 
3. Rebuild the firmware

Injected preprocessor build flags include:
- MANUFACTURER_NAME — e.g., "BLIMCo"
- HARDWARE_VERSION — e.g., "1.0"
- SERIAL_NUMBER — e.g., "FS3-001"
- DEVICE_NAME — e.g., "BLIM IMU Stream"
- DEVICE_NAME_SHORT — e.g., "BLIM-IMU"

### Build-Time Summary
During compilation, all configuration parameters are displayed in a clear, formatted summary, allowing developers to verify device-specific settings, firmware version, and build flags:

```yaml
============================================================
Device Configuration:
  Board:             um_feathers3
  Manufacturer:      BLIMCo
  Serial Number:     FS3-001
  Hardware Version:  1.0
  Firmware Version:  0.0.0-dev
  Software Revision: b96b420-dirty
  Device Name:       BLIM IMU Stream
  Short Name:        BLIM-IMU
============================================================
```

### Creating a release
```bash
git commit -m "Add IMU calibration feature"
git tag v1.1.0
pio run -e um_feathers3
# Firmware Version: 1.1.0
# Software Revision: b96b420
```