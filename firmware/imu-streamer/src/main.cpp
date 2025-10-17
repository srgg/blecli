/*
 * ESP32-S3 MotionCal BLE IMU Sensor Device
 *
 * This firmware streams real-time 9-axis IMU data (accelerometer, gyroscope, magnetometer)
 * over BLE for motion capture and sensor calibration applications.
 *
 * Features:
 * - LSM6DSOX 6-axis IMU (accelerometer + gyroscope)
 * - LIS3MDL 3-axis magnetometer
 * - Custom BLE IMU Service with single 9-float characteristic
 * - Device Information Service with hardware/firmware details
 * - 50Hz continuous streaming (36 bytes/sample)
 * - Optimized advertising for passive/active scan discovery
 */

#include <Arduino.h>
#include <Wire.h>
#include <Adafruit_LSM6DSOX.h>
#include <Adafruit_LIS3MDL.h>
#include <Adafruit_Sensor.h>
#include <NimBLEDevice.h>   // NimBLE + NimBLE2902 class included here
#include "version.h"        // Device configuration and versioning
#include "ble_device_settings_service.h"  // Device settings management


// Sensor objects
Adafruit_LSM6DSOX lsm6dsox;
Adafruit_LIS3MDL lis3mdl;

// BLE objects
NimBLEServer* pServer;
NimBLEService* imuService;
NimBLECharacteristic* imuChar;

// Custom BLE UUIDs for IMU Service
// Using 16-bit vendor-specific UUIDs (0xFF00-0xFFFF range)
// These are compact (2 bytes) and clearly non-Bluetooth SIG assigned
#define SERVICE_IMU_UUID 0xFF10  // Vendor-specific IMU service
#define CHAR_IMU_UUID    0xFF11  // Vendor-specific IMU characteristic

// Pre-allocated array for BLE notification (9 floats)
float imuData[9]; // accelX,Y,Z; gyroX,Y,Z; magX,Y,Z

void setupSensors() {
  // FeatherS3 I2C pins: SDA=GPIO8, SCL=GPIO9
  Wire.begin(8, 9);

  // I2C Scanner - diagnose what's on the bus
  Serial.println("Scanning I2C bus...");
  uint8_t devices_found = 0;
  for (uint8_t addr = 1; addr < 127; addr++) {
    Wire.beginTransmission(addr);
    uint8_t error = Wire.endTransmission();
    if (error == 0) {
      Serial.printf("  Found device at 0x%02X\n", addr);
      devices_found++;
    }
  }
  Serial.printf("Scan complete. Found %d device(s)\n", devices_found);

  if (!lsm6dsox.begin_I2C()) {
    Serial.println("❌ Could not find LSM6DSOX!");
    Serial.println("   Expected I2C address: 0x6A or 0x6B");
    while (1) delay(10);
  }
  lsm6dsox.setAccelRange(LSM6DS_ACCEL_RANGE_4_G);
  lsm6dsox.setGyroRange(LSM6DS_GYRO_RANGE_2000_DPS);

  if (!lis3mdl.begin_I2C()) {
    Serial.println("❌ Could not find LIS3MDL!");
    Serial.println("   Expected I2C address: 0x1C or 0x1E");
    while (1) delay(10);
  }
  lis3mdl.setRange(LIS3MDL_RANGE_4_GAUSS);

  Serial.println("✅ Sensors initialized");
}

// BLE Server Callbacks - handle connection/disconnection events
class ServerCallbacks : public NimBLEServerCallbacks {
  void onConnect(NimBLEServer* pServer, NimBLEConnInfo& connInfo) {
    Serial.println("🔗 Client connected");
    Serial.printf("   Address: %s\n", connInfo.getAddress().toString().c_str());
  }

  void onDisconnect(NimBLEServer* pServer, NimBLEConnInfo& connInfo, int reason) {
    Serial.println("❌ Client disconnected");
    Serial.printf("   Reason: %d\n", reason);
    // Restart advertising so other clients can connect
    NimBLEDevice::startAdvertising();
    Serial.println("📡 Advertising restarted");
  }
};

void setupBLE() {
  // Initialize NimBLE with device name
  NimBLEDevice::init(DEVICE_NAME);
  NimBLEDevice::setMTU(247); // Max MTU to send all 9 floats

  // Create BLE server and set callbacks
  pServer = NimBLEDevice::createServer();
  pServer->setCallbacks(new ServerCallbacks());

  // Create Device Information Service (0x180A)
  NimBLEService* deviceInfoService = createDeviceInfoService(
      pServer,
      MANUFACTURER_NAME,
      MODEL_NUMBER,
      SERIAL_NUMBER,
      HARDWARE_VERSION,
      FIRMWARE_VERSION,
      SOFTWARE_REVISION  // Git commit hash
  );
  deviceInfoService->start();

  // Create IMU service with custom UUID
  imuService = pServer->createService(NimBLEUUID((uint16_t)SERVICE_IMU_UUID));

  // Create IMU characteristic with READ and NOTIFY
  imuChar = imuService->createCharacteristic(
      NimBLEUUID((uint16_t)CHAR_IMU_UUID),
      NIMBLE_PROPERTY::READ | NIMBLE_PROPERTY::NOTIFY
  );

  // Add User Description Descriptor (0x2901)
  NimBLEDescriptor* userDesc = new NimBLEDescriptor(
      NimBLEUUID((uint16_t)0x2901),
      NIMBLE_PROPERTY::READ,
      50,  // maxLen for description string
      imuChar
  );
  userDesc->setValue("IMU: Accel(m/s^2) | Gyro(dps) | Mag(uT)");
  imuChar->addDescriptor(userDesc);

  // Presentation Format Descriptors (0x2904) for each sensor type
  // Format: [format(1), exponent(1), unit(2), namespace(1), description(2)]

  // Accelerometer: 3x float32, m/s² (unit 0x2713)
  uint8_t accelFormat[7] = {
    0x06,       // Format: IEEE-754 32-bit float
    0x00,       // Exponent: 10^0
    0x13, 0x27, // Unit: 0x2713 (metres per second squared)
    0x01,       // Namespace: Bluetooth SIG Assigned Numbers
    0x00, 0x00  // Description: none
  };
  NimBLEDescriptor* accelPresFormat = new NimBLEDescriptor(
      NimBLEUUID((uint16_t)0x2904),
      NIMBLE_PROPERTY::READ,
      7,
      imuChar
  );
  accelPresFormat->setValue(accelFormat, 7);
  imuChar->addDescriptor(accelPresFormat);

  // Gyroscope: 3x float32, degrees/second (unit 0x2700 unitless, use exponent for scaling)
  uint8_t gyroFormat[7] = {
    0x06,       // Format: IEEE-754 32-bit float
    0x00,       // Exponent: 10^0
    0x00, 0x27, // Unit: 0x2700 (unitless - no BLE standard unit for angular velocity)
    0x01,       // Namespace: Bluetooth SIG
    0x00, 0x00  // Description: none
  };
  NimBLEDescriptor* gyroPresFormat = new NimBLEDescriptor(
      NimBLEUUID((uint16_t)0x2904),
      NIMBLE_PROPERTY::READ,
      7,
      imuChar
  );
  gyroPresFormat->setValue(gyroFormat, 7);
  imuChar->addDescriptor(gyroPresFormat);

  // Magnetometer: 3x float32, µT (unit 0x2774 tesla, exponent -6 for micro)
  uint8_t magFormat[7] = {
    0x06,       // Format: IEEE-754 32-bit float
    0xFA,       // Exponent: -6 (10^-6 for micro)
    0x74, 0x27, // Unit: 0x2774 (tesla)
    0x01,       // Namespace: Bluetooth SIG
    0x00, 0x00  // Description: none
  };
  NimBLEDescriptor* magPresFormat = new NimBLEDescriptor(
      NimBLEUUID((uint16_t)0x2904),
      NIMBLE_PROPERTY::READ,
      7,
      imuChar
  );
  magPresFormat->setValue(magFormat, 7);
  imuChar->addDescriptor(magPresFormat);

  // Aggregate Format Descriptor (0x2905) - combines the 3 format descriptors
  // Lists the handles of the 3 Presentation Format Descriptors
  // Note: This indicates the characteristic contains aggregated data
  NimBLEDescriptor* aggregateFormat = new NimBLEDescriptor(
      NimBLEUUID((uint16_t)0x2905),
      NIMBLE_PROPERTY::READ,
      6,  // 3 handles x 2 bytes each
      imuChar
  );
  // Get handles - these are assigned when descriptors are added
  uint16_t handles[3] = {
    accelPresFormat->getHandle(),
    gyroPresFormat->getHandle(),
    magPresFormat->getHandle()
  };
  aggregateFormat->setValue((uint8_t*)handles, 6);
  imuChar->addDescriptor(aggregateFormat);

  // Start the IMU service
  imuService->start();

  // Create and start Device Settings Service (separate from IMU service)
  NimBLEService* settingsService = create_device_settings_service(pServer);
  if (settingsService) {
    settingsService->start();
  }

  // Configure advertising for passive and active scanning
  NimBLEAdvertising* pAdv = NimBLEDevice::getAdvertising();

  // Enable scan response for active scanning
  pAdv->enableScanResponse(true);

  // PASSIVE SCAN (Advertising Data - 31 bytes max):
  // - Flags: General Discoverable + BR/EDR Not Supported (3 bytes)
  // - Shortened device name (~13 bytes)
  // - IMU Service UUID 16-bit (~4 bytes)
  // - Total: ~20 bytes - Fits!
  NimBLEAdvertisementData advData;
  advData.setFlags(0x06);  // 0x02 (General Discoverable) | 0x04 (BR/EDR Not Supported)
  advData.setName(DEVICE_NAME_SHORT, false);  // Shortened name, isComplete=false
  advData.addServiceUUID(NimBLEUUID((uint16_t)SERVICE_IMU_UUID));  // 16-bit vendor UUID
  pAdv->setAdvertisementData(advData);

  // ACTIVE SCAN (Scan Response Data - 31 bytes max):
  // - Complete name "BLIM IMU Stream" (~17 bytes)
  // - Device Info Service UUID 0x180A (~4 bytes)
  // - Total: ~21 bytes
  // Note: IMU UUID already in an advertising packet, no need to repeat
  NimBLEAdvertisementData scanResponse;
  scanResponse.setName(DEVICE_NAME, true);  // Complete name, isComplete=true
  scanResponse.addServiceUUID(NimBLEUUID((uint16_t)0x180A));  // Device Info Service
  pAdv->setScanResponseData(scanResponse);

  NimBLEDevice::startAdvertising();

  Serial.println("📡 BLE Services started:");
  Serial.printf("   Device: %s\n", DEVICE_NAME);
  Serial.printf("   Short Name (passive): %s\n", DEVICE_NAME_SHORT);
  Serial.printf("   IMU Service UUID: 0x%04X (vendor-specific)\n", SERVICE_IMU_UUID);
  Serial.printf("   Manufacturer: %s\n", MANUFACTURER_NAME);
  Serial.printf("   Model: %s\n", MODEL_NUMBER);
  Serial.println("   📱 Passive scan: Short name + IMU UUID (0xFF10)");
  Serial.println("   🔍 Active scan: Complete name + Device Info UUID (0x180A)");
}

void setup() {
  Serial.begin(115200);
  delay(1000);
  setupSensors();
  setupBLE();
}

void loop() {
  sensors_event_t accel_event, gyro_event, mag_event;

  // Read sensors via Unified Sensor API
  lsm6dsox.getAccelerometerSensor()->getEvent(&accel_event);
  lsm6dsox.getGyroSensor()->getEvent(&gyro_event);
  lis3mdl.getEvent(&mag_event);

  // Pack data into imuData[9]
  imuData[0] = accel_event.acceleration.x;
  imuData[1] = accel_event.acceleration.y;
  imuData[2] = accel_event.acceleration.z;
  imuData[3] = gyro_event.gyro.x;
  imuData[4] = gyro_event.gyro.y;
  imuData[5] = gyro_event.gyro.z;
  imuData[6] = mag_event.magnetic.x;
  imuData[7] = mag_event.magnetic.y;
  imuData[8] = mag_event.magnetic.z;

  // Send BLE notification
  imuChar->setValue((uint8_t*)imuData, sizeof(imuData));
  imuChar->notify();

  // Print for debug: Accel (m/s²), Gyro (dps), Mag (µT)
  Serial.printf("Accel: %.2f,%.2f,%.2f | Gyro: %.2f,%.2f,%.2f | Mag: %.2f,%.2f,%.2f\n",
                imuData[0], imuData[1], imuData[2],
                imuData[3], imuData[4], imuData[5],
                imuData[6], imuData[7], imuData[8]);

  delay(20); // 50 Hz
}
