#include <Arduino.h>


// Uncomment for NoLock policy (single-core/pinned)
// #define BLE_ON_SINGLE_CORE

#define CONFIG_BT_NIMBLE_ROLE_OBSERVER_DISABLED
#define CONFIG_BT_NIMBLE_ROLE_CENTRAL_DISABLED
#define CONFIG_BT_NIMBLE_ROLE_BROADCASTER_DISABLE

#ifdef BLE_ON_SINGLE_CORE
    #define CONFIG_BT_NIMBLE_PINNED_TO_CORE   ARDUINO_RUNNING_CORE
#endif

#include <NimBLEDevice.h>


#include "device_info_service.hpp"
#include "device_settings_service.hpp"
#include "imu_servce.hpp"

// Example: https://github.com/h2zero/esp-nimble-cpp/blob/master/examples/NimBLE_Server/main/main.cpp

// Device name variables with external linkage for template parameters
inline constexpr char deviceName[] = DEVICE_NAME;
inline constexpr char deviceNameShort[] = DEVICE_NAME_SHORT;

// ---------------------- Lock Policy Selection ----------------------
// Choose lock policy based on deployment configuration
#ifdef BLE_ON_SINGLE_CORE
    using blim = blex<NoLock>;  // Zero overhead for single-core/pinned execution
#else
    using blim = blexDefault;   // Thread-safe for multi-core concurrent access
#endif

// ---------------------- Server Callbacks ----------------------

static void onConnect(NimBLEServer* pServer, NimBLEConnInfo& conn) {
    Serial.printf("🔗 Device connected: %s\n", conn.getAddress().toString().c_str());
    Serial.printf("   Connection ID: %u\n", conn.getConnHandle());
    Serial.printf("   MTU: %u bytes\n", conn.getMTU());
}

static void onDisconnect(NimBLEServer* pServer, NimBLEConnInfo& conn, int reason) {
    Serial.printf("❌ Device disconnected: %s (reason=%d)\n",
                  conn.getAddress().toString().c_str(), reason);

    // Auto-restart advertising
    NimBLEDevice::startAdvertising();
    Serial.println("📡 Advertising restarted");
}

// ---------------------- BLE Server Configuration ----------------------
using ImuDevice = blim::Server<
    deviceName,
    deviceNameShort,
    blim::AdvertisingConfig<
        9,                                                          // TX=9dBm
        120, 140,                                                   // Intervals=120-140ms
        static_cast<uint16_t>(blim::BleAppearance::kGenericSensor)  // Appearance=Generic Sensor (0x0540)
    >,
    blim::ConnectionConfig<247, 12, 12, 0, 400>,  // MTU=247, Interval=15ms, Latency=0, Timeout=4s
    blim::OnConnect<onConnect>,        // Custom connect handler
    blim::OnDisconnect<onDisconnect>,  // Custom disconnect handler
    blim::PassiveAdvService<DeviceSettingsService<blim>>,
    blim::ActiveAdvService<DeviceInfoService<blim>>,
    IMUService<blim>
>;

bool setup_ble() {
    // Verify NimBLE logging is configured
    #ifdef CONFIG_NIMBLE_CPP_LOG_LEVEL
        Serial.printf("[BLE] CONFIG_NIMBLE_CPP_LOG_LEVEL = %d\n", CONFIG_NIMBLE_CPP_LOG_LEVEL);
    #else
        Serial.println("[BLE] WARNING: CONFIG_NIMBLE_CPP_LOG_LEVEL not defined!")       ;
    #endif

    bool success = ImuDevice::init();
    if (!success) {
        BLIM_LOG_ERROR("BLE initialization failed\n");
        return false;
    }
    return true;
}

void update_imu(const float (&data)[9]) {
    IMUService<blim>::IMUChar::setValue(data);
}