/**
 * @file main.cpp
 * @brief Environmental Sensor Hub - BLE Test Peripheral
 *
 * A realistic IoT device that demonstrates comprehensive BLE features:
 * - DeviceInfoService: Standard DIS with const characteristics
 * - SensorService: Temperature, humidity, battery with Read+Notify
 * - ControlService: Commands (WNR), responses (Notify), alerts (Indicate),
 *                   protected config (encrypted write), diagnostics (long read)
 *
 * Use this device to test BLE CLI tools against real-world patterns.
 */

#include "blex.hpp"
#include "services.hpp"

// ============================================================================
// Device Configuration
// ============================================================================

constexpr char deviceName[] = "Blim ESH";
constexpr char deviceNameLong[] = "Blim Sensor Hub";

// Forward declarations
static void onConnect(const blexDefault::ConnectionInfo& conn);
static void onDisconnect(const blexDefault::ConnectionInfo& conn, int reason);

// ============================================================================
// BLE Server Definition
// ============================================================================

using SensorHub = blexDefault::Server<
    deviceName,

    // Advertising: appear as a sensor device
    blexDefault::AdvertisementConfig<>
        ::WithLongName<deviceNameLong>
        ::WithManufacturerData<>
            ::WithManufacturerId<0xFFFF>    // Development/testing ID
            ::WithDeviceType<0x01>          // Sensor hub type
        ::WithTxPower<3>
        ::WithAppearance<blexDefault::BleAppearance::kSensor>
        ::WithIntervals<100, 200>,

    // Server callbacks
    blexDefault::ServerCallbacks<>
        ::WithOnConnect<onConnect>
        ::WithOnDisconnect<onDisconnect>,

    // Services
    blexDefault::PassiveAdvService<DeviceInfoService<blexDefault>>,
    blexDefault::ActiveAdvService<SensorService<blexDefault>>,
    blexDefault::ActiveAdvService<ControlService<blexDefault>>
>;

// Service aliases for convenience
using Sensor = SensorService<blexDefault>;
using Control = ControlService<blexDefault>;

// ============================================================================
// State
// ============================================================================

static bool connected = false;
static unsigned long lastSampleTime = 0;
static unsigned long lastAlertCheck = 0;
static uint8_t simulatedBattery = 100;

// ============================================================================
// Connection Callbacks
// ============================================================================

static void onConnect(const blexDefault::ConnectionInfo& conn) {
    connected = true;
    Serial.printf("Connected: %s\n", conn.getAddress().toString().c_str());

    // Log connection to diagnostics
    Control::appendDiagLog("[CONN] Client connected\n");
}

static void onDisconnect(const blexDefault::ConnectionInfo& conn, const int reason) {
    connected = false;
    Serial.printf("Disconnected: %s (reason: %d)\n", conn.getAddress().toString().c_str(), reason);

    Control::appendDiagLog("[CONN] Client disconnected\n");
    SensorHub::startAdvertising();
}

// ============================================================================
// Sensor Simulation
// ============================================================================

static void simulateSensors() {
    // Temperature: 20-25°C with small random variation
    int16_t temp = 2250 + random(-50, 51);  // 22.50°C ± 0.50°C
    Sensor::setTemperature(temp);

    // Humidity: 50-60% with variation
    uint16_t hum = 5500 + random(-500, 501);  // 55% ± 5%
    Sensor::setHumidity(hum);

    // Battery: simulate drain/charge cycles
    static unsigned long lastBatteryUpdate = 0;
    static bool charging = false;
    if (millis() - lastBatteryUpdate >= 2000) {  // Every 2 seconds
        lastBatteryUpdate = millis();
        if (charging) {
            simulatedBattery += 5;  // Charge faster
            if (simulatedBattery >= 100) {
                simulatedBattery = 100;
                charging = false;  // Start draining
            }
        } else {
            simulatedBattery--;  // Drain slowly
            if (simulatedBattery <= 10) {
                charging = true;  // Start charging
            }
        }
        Sensor::setBatteryLevel(simulatedBattery);
    }
}

// ============================================================================
// Alert Checking
// ============================================================================

static void checkAlerts() {
    if (!connected || !Control::isAlertSubscribed()) return;

    int16_t temp = Sensor::getTemperature();
    uint8_t battery = Sensor::getBatteryLevel();

    // Check temperature thresholds
    if (temp > Control::getTempAlertHigh()) {
        Control::sendTempHighAlert(temp);
        Serial.printf("ALERT: Temperature high (%d.%02d°C)\n", temp / 100, abs(temp) % 100);
    } else if (temp < Control::getTempAlertLow()) {
        Control::sendTempLowAlert(temp);
        Serial.printf("ALERT: Temperature low (%d.%02d°C)\n", temp / 100, abs(temp) % 100);
    }

    // Check battery threshold
    static bool batteryAlertSent = false;
    if (battery <= 20 && !batteryAlertSent) {
        Control::sendBatteryLowAlert(battery);
        Serial.printf("ALERT: Battery low (%d%%)\n", battery);
        batteryAlertSent = true;
    } else if (battery > 20) {
        batteryAlertSent = false;
    }
}

// ============================================================================
// Status Display
// ============================================================================

static void printStatus() {
    static unsigned long lastStatus = 0;
    if (millis() - lastStatus < 5000) return;
    lastStatus = millis();

    Serial.println("--- Sensor Status ---");
    Serial.printf("  Temp: %d.%02d°C\n",
                  Sensor::getTemperature() / 100,
                  abs(Sensor::getTemperature()) % 100);
    Serial.printf("  Humidity: %d.%02d%%\n",
                  Sensor::getHumidity() / 100,
                  Sensor::getHumidity() % 100);
    Serial.printf("  Battery: %d%%\n", Sensor::getBatteryLevel());
    Serial.printf("  Sampling: %s (interval: %dms)\n",
                  Control::isSamplingEnabled() ? "ON" : "OFF",
                  Control::getSampleInterval());
    Serial.printf("  Subscriptions: Temp=%s, Hum=%s, Batt=%s, Resp=%s, Alert=%s\n",
                  Sensor::isTempSubscribed() ? "Y" : "N",
                  Sensor::isHumiditySubscribed() ? "Y" : "N",
                  Sensor::isBatterySubscribed() ? "Y" : "N",
                  Control::isResponseSubscribed() ? "Y" : "N",
                  Control::isAlertSubscribed() ? "Y" : "N");
    Serial.println();
}

// ============================================================================
// Arduino Entry Points
// ============================================================================

void setup() {
    Serial.begin(115200);
    delay(1000);

    Serial.println();
    Serial.println("========================================");
    Serial.println("   Environmental Sensor Hub");
    Serial.println("   BLE Test Peripheral");
    Serial.println("========================================");
    Serial.printf("Manufacturer: %s\n", MANUFACTURER_NAME);
    Serial.printf("Model: %s\n", MODEL_NUMBER);
    Serial.printf("Firmware: %s\n", FIRMWARE_VERSION);
    Serial.println();

    // Initialize BLE
    if (!SensorHub::init()) {
        Serial.println("ERROR: BLE init failed!");
        return;
    }

    SensorHub::startAllServices();

    Serial.println("BLE Services:");
    Serial.println("  - Device Information (0x180A)");
    Serial.println("  - Sensor Service (0x181A)");
    Serial.println("    - Temperature (0x2A6E): Read+Notify");
    Serial.println("    - Humidity (0x2A6F): Read+Notify");
    Serial.println("    - Battery (0x2A19): Read+Notify");
    Serial.println("  - Control Service (E5700001-...)");
    Serial.println("    - Command: Write No Response");
    Serial.println("    - Response: Notify");
    Serial.println("    - Alert: Indicate");
    Serial.println("    - Config: Read + Encrypted Write");
    Serial.println("    - Diagnostic Log: Read (512 bytes)");
    Serial.println();
    Serial.printf("Device: %s\n", deviceName);
    Serial.printf("Address: %s\n", SensorHub::getAddress());
    Serial.println();
    Serial.println("Waiting for connection...");
    Serial.println();
}

void loop() {
    unsigned long now = millis();

    // Sample sensors at a configured interval (if sampling enabled)
    uint16_t interval = Control::isSamplingEnabled() ? Control::getSampleInterval() : 1000;
    if (now - lastSampleTime >= interval) {
        lastSampleTime = now;
        simulateSensors();
    }

    // Check alerts every 2 seconds
    if (now - lastAlertCheck >= 2000) {
        lastAlertCheck = now;
        checkAlerts();
    }

    // Print status periodically
    printStatus();

    delay(10);
}