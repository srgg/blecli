/*
 * Device Settings BLE Service
 *
 * Provides BLE interface for device configuration with persistent storage.
 * Supports JSON format and partial updates.
 *
 * BLE Device Settings Service (0xFF20):
 * - 0xFF21: Configuration Data (READ/WRITE) - JSON format, auto-saves to NVS
 * - 0xFF22: Settings State (READ/NOTIFY) - Status flags (calibration enabled, etc.)
 * - 0xFF23: Settings Control Point (WRITE) - Commands (factory reset, reboot)
 */

#ifndef DEVICE_SETTINGS_SVC_H
#define DEVICE_SETTINGS_SVC_H

#include "device_settings.h"

// NimBLE must be included before this header
#if !defined(CONFIG_BT_NIMBLE_ENABLED) && !defined(NIMBLE_CPP_H_) && !defined(_NIMBLEDEVICE_H_) && !defined(NIMBLEDEVICE_H_)
#error "NimBLE headers must be included before device_settings_svc.h"
#endif

// ============================================================================
// BLE Service Constants
// ============================================================================

// BLE Service and Characteristic UUIDs
constexpr uint16_t SERVICE_DEVICE_SETTINGS_UUID  = 0xFF20;  // Device Settings Service
constexpr uint16_t CHAR_CONFIG_DATA_UUID         = 0xFF21;  // Configuration data (JSON)
constexpr uint16_t CHAR_SETTINGS_STATE_UUID      = 0xFF22;  // Settings state flags
constexpr uint16_t CHAR_CONTROL_POINT_UUID       = 0xFF23;  // Control point (commands)

// BLE state bit masks
constexpr uint8_t STATE_APPLY_CALIBRATION = 0x01;  // Bit 0: Apply calibration to IMU stream

// Settings state bit field (packed for BLE transmission)
struct __attribute__((packed)) SettingsState {
  uint8_t apply_calibration : 1;  // Bit 0: Apply calibration to IMU stream
  uint8_t reserved : 7;            // Bits 1-7: Reserved for future use

  SettingsState() : apply_calibration(0), reserved(0) {}
  explicit SettingsState(bool apply_cal) : apply_calibration(apply_cal ? 1 : 0), reserved(0) {}
};

static_assert(sizeof(SettingsState) == 1, "SettingsState must be 1 byte");

// Control Point Commands (0xFF23)
constexpr uint8_t CMD_FACTORY_RESET = 0x01;  // Reset all config to factory defaults
constexpr uint8_t CMD_REBOOT        = 0x02;  // Reboot device

// Control Point Response Codes (for INDICATE feedback)
constexpr uint8_t RESP_SUCCESS          = 0x00;  // Command executed successfully
constexpr uint8_t RESP_INVALID_COMMAND  = 0x01;  // Unknown command code
constexpr uint8_t RESP_ERROR            = 0x02;  // Execution failed

// ============================================================================
// BLE Service State
// ============================================================================

namespace DeviceSettingsSvc {
  // BLE characteristic pointers
  static NimBLECharacteristic* g_config_data_char = nullptr;
  static NimBLECharacteristic* g_settings_state_char = nullptr;
  static NimBLECharacteristic* g_control_point_char = nullptr;
}

// ============================================================================
// BLE Helper Functions
// ============================================================================

/**
 * Set whether to apply calibration to IMU stream (with BLE notification)
 * Uses builder API and notifies BLE clients
 *
 * @param apply true to apply calibration coefficients, false for raw data
 * @param save If true, persist changes to storage (default: true)
 */
inline void ble_set_apply_calibration(bool apply, bool save = true) {
  // Update via builder (this logs and saves if requested)
  if (!DeviceSettings::modify().set_apply_calibration(apply).commit(save)) {
    Serial.println("❌ Failed to apply calibration setting");
  }

  // Notify BLE clients of state change
  if (DeviceSettingsSvc::g_settings_state_char) {
    SettingsState state(DeviceSettings::get().is_calibration_enabled());
    DeviceSettingsSvc::g_settings_state_char->setValue(reinterpret_cast<uint8_t*>(&state), 1);
    DeviceSettingsSvc::g_settings_state_char->notify();
  }
}

// ============================================================================
// BLE Characteristic Callbacks
// ============================================================================

/**
 * Configuration Data Characteristic (0xFF21) callbacks
 * READ: Returns current configuration as JSON
 * WRITE: Deep merges incoming JSON, auto-saves to NVS
 */
class ConfigDataCallbacks : public NimBLECharacteristicCallbacks {
  char json_buffer[512];  // Local buffer for JSON serialization

  void onRead(NimBLECharacteristic* pChar, NimBLEConnInfo& connInfo) override {
    size_t len = DeviceSettings::get().to_json(json_buffer, sizeof(json_buffer));
    if (len > 0) {
      pChar->setValue(reinterpret_cast<const uint8_t*>(json_buffer), len);
      Serial.printf("📤 BLE READ: Configuration data (%u bytes)\n", len);
    } else {
      Serial.println("❌ Failed to serialize configuration");
    }
  }

  void onWrite(NimBLECharacteristic* pChar, NimBLEConnInfo& connInfo) override {
    std::string value = pChar->getValue();
    Serial.printf("📥 BLE WRITE: Configuration data (%u bytes)\n", value.length());

    // Validate size (must fit in our static buffer)
    if (value.length() > sizeof(json_buffer)) {
      Serial.printf("❌ JSON too large: %u bytes (max %u)\n",
                    value.length(), sizeof(json_buffer));
      return;
    }

    // Merge JSON and auto-save using builder
    if (!DeviceSettings::modify().merge_json(value.c_str()).commit(true)) {
      Serial.println("❌ Failed to apply config");
      return;
    }

    // Notify state characteristic of any changes
    if (DeviceSettingsSvc::g_settings_state_char) {
      SettingsState state(DeviceSettings::get().is_calibration_enabled());
      DeviceSettingsSvc::g_settings_state_char->setValue(reinterpret_cast<uint8_t*>(&state), 1);
      DeviceSettingsSvc::g_settings_state_char->notify();
    }

    // Log current stream mode
    Serial.printf("IMU stream: %s\n", DeviceSettings::get().is_calibration_enabled() ? "CALIBRATED" : "RAW");
  }
};

/**
 * Settings State Characteristic (0xFF22) callbacks
 * READ: Returns current state flags
 * WRITE: Updates state flags
 */
class SettingsStateCallbacks : public NimBLECharacteristicCallbacks {
  void onRead(NimBLECharacteristic* pChar, NimBLEConnInfo& connInfo) override {
    SettingsState state(DeviceSettings::get().is_calibration_enabled());
    pChar->setValue(reinterpret_cast<uint8_t*>(&state), 1);
    Serial.printf("📤 BLE READ: Settings state 0x%02X\n", *reinterpret_cast<uint8_t*>(&state));
  }

  void onWrite(NimBLECharacteristic* pChar, NimBLEConnInfo& connInfo) override {
    std::string value_str = pChar->getValue();
    if (value_str.length() < 1) {
      Serial.println("❌ Invalid state write: no data");
      return;
    }

    uint8_t new_state = value_str[0];
    Serial.printf("📥 BLE WRITE: Settings state 0x%02X\n", new_state);

    // Validate reserved bits (should be 0)
    if ((new_state & ~STATE_APPLY_CALIBRATION) != 0) {
      Serial.printf("⚠️  Warning: Reserved bits set in state: 0x%02X\n", new_state);
    }

    // Check if apply_calibration bit changed
    bool old_apply = DeviceSettings::get().is_calibration_enabled();
    bool new_apply = (new_state & STATE_APPLY_CALIBRATION) != 0;

    if (old_apply != new_apply) {
      ble_set_apply_calibration(new_apply, true);  // This handles save and notify
    } else {
      // No state change, just notify
      pChar->notify();
    }
  }
};

/**
 * Control Point Characteristic (0xFF23) callbacks
 * WRITE: Execute control commands
 */
class ControlPointCallbacks : public NimBLECharacteristicCallbacks {
  void onWrite(NimBLECharacteristic* pChar, NimBLEConnInfo& connInfo) override {
    std::string value_str = pChar->getValue();

    // Validate data length
    if (value_str.length() < 1) {
      Serial.println("❌ Invalid control command: no data");
      return;
    }

    uint8_t cmd = value_str[0];
    Serial.printf("📥 BLE CONTROL: Command 0x%02X\n", cmd);

    switch (cmd) {
      case CMD_FACTORY_RESET:
        // Reset to factory and save
        if (!DeviceSettings::modify().reset(true).commit(true)) {
          Serial.println("❌ Factory reset failed");
        }

        // Notify BLE clients
        if (DeviceSettingsSvc::g_settings_state_char) {
          SettingsState state(DeviceSettings::get().is_calibration_enabled());
          DeviceSettingsSvc::g_settings_state_char->setValue(reinterpret_cast<uint8_t*>(&state), 1);
          DeviceSettingsSvc::g_settings_state_char->notify();
        }
        break;

      case CMD_REBOOT:
        Serial.println("🔄 Rebooting...");
        delay(100);
        ESP.restart();
        break;

      default:
        Serial.printf("⚠️  Unknown command: 0x%02X\n", cmd);
        break;
    }
  }
};

// ============================================================================
// Setup Function
// ============================================================================

/**
 * Create Device Settings Service (0xFF20) with all characteristics
 *
 * MUST be called during BLE initialization, after server creation
 *
 * @param pServer Pointer to NimBLE server
 * @return Pointer to created service (ready to start)
 */
inline NimBLEService* create_device_settings_service(NimBLEServer* pServer) {
  if (!pServer) {
    Serial.println("❌ Cannot create settings service: server is null");
    return nullptr;
  }

  // Create Device Settings Service (0xFF20)
  NimBLEService* settings_service = pServer->createService(NimBLEUUID(SERVICE_DEVICE_SETTINGS_UUID));

  // Configuration Data characteristic (0xFF21)
  DeviceSettingsSvc::g_config_data_char = settings_service->createCharacteristic(
    NimBLEUUID(CHAR_CONFIG_DATA_UUID),
    NIMBLE_PROPERTY::READ | NIMBLE_PROPERTY::WRITE
  );
  DeviceSettingsSvc::g_config_data_char->setCallbacks(new ConfigDataCallbacks());

  NimBLEDescriptor* data_desc = new NimBLEDescriptor(
    NimBLEUUID((uint16_t)0x2901),
    NIMBLE_PROPERTY::READ,
    100,
    DeviceSettingsSvc::g_config_data_char
  );
  data_desc->setValue("Configuration data (JSON, supports partial updates, auto-saves)");
  DeviceSettingsSvc::g_config_data_char->addDescriptor(data_desc);

  // Settings State characteristic (0xFF22)
  DeviceSettingsSvc::g_settings_state_char = settings_service->createCharacteristic(
    NimBLEUUID(CHAR_SETTINGS_STATE_UUID),
    NIMBLE_PROPERTY::READ | NIMBLE_PROPERTY::WRITE | NIMBLE_PROPERTY::NOTIFY
  );
  DeviceSettingsSvc::g_settings_state_char->setCallbacks(new SettingsStateCallbacks());

  // Set initial value from current settings
  SettingsState state(DeviceSettings::get().is_calibration_enabled());
  DeviceSettingsSvc::g_settings_state_char->setValue(reinterpret_cast<uint8_t*>(&state), 1);

  NimBLEDescriptor* state_desc = new NimBLEDescriptor(
    NimBLEUUID((uint16_t)0x2901),
    NIMBLE_PROPERTY::READ,
    100,
    DeviceSettingsSvc::g_settings_state_char
  );
  state_desc->setValue("Settings state (Bit 0: apply calibration to stream)");
  DeviceSettingsSvc::g_settings_state_char->addDescriptor(state_desc);

  // Control Point characteristic (0xFF23)
  DeviceSettingsSvc::g_control_point_char = settings_service->createCharacteristic(
    NimBLEUUID(CHAR_CONTROL_POINT_UUID),
    NIMBLE_PROPERTY::WRITE
  );
  DeviceSettingsSvc::g_control_point_char->setCallbacks(new ControlPointCallbacks());

  NimBLEDescriptor* control_desc = new NimBLEDescriptor(
    NimBLEUUID((uint16_t)0x2901),
    NIMBLE_PROPERTY::READ,
    100,
    DeviceSettingsSvc::g_control_point_char
  );
  control_desc->setValue("Control point (0x01=factory reset, 0x02=reboot)");
  DeviceSettingsSvc::g_control_point_char->addDescriptor(control_desc);

  Serial.println("✅ Device Settings Service (0xFF20) created:");
  Serial.println("   - 0xFF21: Configuration Data (READ/WRITE, JSON)");
  Serial.println("   - 0xFF22: Settings State (READ/WRITE/NOTIFY)");
  Serial.println("   - 0xFF23: Control Point (WRITE)");

  return settings_service;
}

#endif // DEVICE_SETTINGS_SVC_H