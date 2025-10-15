/*
 * Device Configuration and Versioning Header
 *
 * All values are automatically injected by version.py at build time
 * as preprocessor build flags (-D defines). The fallback values below
 * are used only if version.py fails to execute.
 *
 * This header should be included by any source file that needs access
 * to device metadata or firmware versioning information.
 */

#ifndef VERSION_H
#define VERSION_H

// Device Name (full BLE advertised name)
#ifndef DEVICE_NAME
#define DEVICE_NAME "Unknown Device"
#endif

// Device Name (short BLE advertised name for passive scan)
#ifndef DEVICE_NAME_SHORT
#define DEVICE_NAME_SHORT "UNKNOWN"
#endif

// Manufacturer Name (BLE Device Information Service)
#ifndef MANUFACTURER_NAME
#define MANUFACTURER_NAME "Unknown"
#endif

// Serial Number (BLE Device Information Service)
#ifndef SERIAL_NUMBER
#define SERIAL_NUMBER "000000"
#endif

// Hardware Version (BLE Device Information Service)
#ifndef HARDWARE_VERSION
#define HARDWARE_VERSION "0.0"
#endif

// Model Number (BLE Device Information Service, typically board name)
#ifndef MODEL_NUMBER
#define MODEL_NUMBER "unknown"
#endif

// Firmware Version (semantic version from Git tags)
#ifndef FIRMWARE_VERSION
#define FIRMWARE_VERSION "0.0.0-dev"
#endif

// Software Revision (Git commit hash, potentially with -dirty suffix)
#ifndef SOFTWARE_REVISION
#define SOFTWARE_REVISION "unknown"
#endif


// ============================================================================
// BLE Device Information Service Helper
// ============================================================================
//
// This function is only available when NimBLE headers are included.
// Make sure to #include <NimBLEDevice.h> BEFORE including this header
// if you want to use createDeviceInfoService().
//
#if defined(CONFIG_BT_NIMBLE_ENABLED) || defined(NIMBLE_CPP_H_) || defined(_NIMBLEDEVICE_H_) || defined(NIMBLEDEVICE_H_)

/**
 * Create a standard BLE Device Information Service (0x180A)
 *
 * Automatically populates characteristics with build-time configuration values:
 * - Manufacturer Name (0x2A29)
 * - Model Number (0x2A24)
 * - Serial Number (0x2A25)
 * - Hardware Revision (0x2A27)
 * - Firmware Revision (0x2A26)
 * - Software Revision (0x2A28)
 *
 * @param pServer NimBLE server instance
 * @param manufacturer Manufacturer name (uses MANUFACTURER_NAME if NULL)
 * @param model Model number (uses MODEL_NUMBER if NULL)
 * @param serial Serial number (uses SERIAL_NUMBER if NULL)
 * @param hwRev Hardware revision (uses HARDWARE_VERSION if NULL)
 * @param fwRev Firmware version (uses FIRMWARE_VERSION if NULL)
 * @param swRev Software revision (uses SOFTWARE_REVISION if NULL)
 * @return Pointer to created NimBLEService (not started - call start() manually)
 */
inline NimBLEService* createDeviceInfoService(
    NimBLEServer* pServer,
    const char* manufacturer = MANUFACTURER_NAME,
    const char* model = MODEL_NUMBER,
    const char* serial = SERIAL_NUMBER,
    const char* hwRev = HARDWARE_VERSION,
    const char* fwRev = FIRMWARE_VERSION,
    const char* swRev = SOFTWARE_REVISION)
{
  // Device Information Service UUID
  constexpr uint16_t SERVICE_DEVICE_INFO_UUID = 0x180A;

  // Characteristic UUIDs
  constexpr uint16_t CHAR_MANUFACTURER_NAME_UUID = 0x2A29;
  constexpr uint16_t CHAR_MODEL_NUMBER_UUID      = 0x2A24;
  constexpr uint16_t CHAR_SERIAL_NUMBER_UUID     = 0x2A25;
  constexpr uint16_t CHAR_HARDWARE_REV_UUID      = 0x2A27;
  constexpr uint16_t CHAR_FIRMWARE_REV_UUID      = 0x2A26;
  constexpr uint16_t CHAR_SOFTWARE_REV_UUID      = 0x2A28;

  // Create the service
  NimBLEService* pService = pServer->createService(SERVICE_DEVICE_INFO_UUID);

  // Add characteristics based on non-NULL parameters
  if (manufacturer) {
    NimBLECharacteristic* c = pService->createCharacteristic(
        CHAR_MANUFACTURER_NAME_UUID,
        NIMBLE_PROPERTY::READ);
    c->setValue(manufacturer);
  }

  if (model) {
    NimBLECharacteristic* c = pService->createCharacteristic(
        CHAR_MODEL_NUMBER_UUID,
        NIMBLE_PROPERTY::READ);
    c->setValue(model);
  }

  if (serial) {
    NimBLECharacteristic* c = pService->createCharacteristic(
        CHAR_SERIAL_NUMBER_UUID,
        NIMBLE_PROPERTY::READ);
    c->setValue(serial);
  }

  if (hwRev) {
    NimBLECharacteristic* c = pService->createCharacteristic(
        CHAR_HARDWARE_REV_UUID,
        NIMBLE_PROPERTY::READ);
    c->setValue(hwRev);
  }

  if (fwRev) {
    NimBLECharacteristic* c = pService->createCharacteristic(
        CHAR_FIRMWARE_REV_UUID,
        NIMBLE_PROPERTY::READ);
    c->setValue(fwRev);
  }

  if (swRev) {
    NimBLECharacteristic* c = pService->createCharacteristic(
        CHAR_SOFTWARE_REV_UUID,
        NIMBLE_PROPERTY::READ);
    c->setValue(swRev);
  }

  return pService;
}

#endif // NimBLE available

#endif // VERSION_H