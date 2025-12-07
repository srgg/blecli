/**
 * @file services.hpp
 * @brief Environmental Sensor Hub - BLE Services
 *
 * An IoT environmental mock device that naturally demonstrates
 * BLE features through meaningful use cases.
 *
 * ============================================================================
 * DEVICE CONCEPT: Environmental Sensor Hub
 * ============================================================================
 *
 * A battery-powered environmental monitoring device with:
 * - Temperature & humidity sensing
 * - Battery monitoring
 * - Configurable sampling and alert thresholds
 * - Command/control interface for device management
 * - Diagnostic logging
 *
 * ============================================================================
 * SERVICES & BLE FEATURE COVERAGE
 * ============================================================================
 *
 * 1. DeviceInfoService (0x180A) - Standard DIS [from blex library]
 *    - BLE Features: Read-only, const values, 16-bit SIG UUIDs
 *
 * 2. SensorService (0x181A) - Environmental + Battery readings
 *    - Temperature:  Read + Notify, signed int16, Presentation Format
 *    - Humidity:     Read + Notify, unsigned int16, Presentation Format
 *    - Battery:      Read + Notify, uint8 percentage
 *    - BLE Features: Multiple chars, descriptors (2901, 2904), 16-bit UUIDs
 *
 * 3. ControlService (vendor 128-bit UUID) - Device management
 *    - Command Register:   Write No Response (fast control commands)
 *    - Command Response:   Notify (command acknowledgements)
 *    - Alert:              Indicate (critical alerts requiring confirmation)
 *    - Config:             Read + Encrypted Write (protected settings)
 *    - Diagnostic Log:     Read (512-byte buffer for long read testing)
 *    - BLE Features: WNR, Notify vs Indicate, security levels, long read
 *
 * ============================================================================
 * COMMAND PROTOCOL (ControlService)
 * ============================================================================
 *
 * Command Register format (Write No Response):
 *   [0]: Command ID
 *   [1-N]: Command parameters
 *
 * Commands:
 *   0x01 - Start sampling
 *   0x02 - Stop sampling
 *   0x03 - Set sample interval (param: uint16 ms)
 *   0x04 - Set alert thresholds (param: int16 temp_high, int16 temp_low)
 *   0x05 - Request diagnostic dump
 *   0x06 - Clear diagnostic log
 *   0xFF - Reset device
 *
 * Response format (Notify):
 *   [0]: Command ID (echo)
 *   [1]: Status (0=OK, 1=ERROR, 2=INVALID_PARAM, 3=INVALID_CMD)
 *   [2-N]: Response data
 *
 * Alert format (Indicate):
 *   [0]: Alert type (1=TEMP_HIGH, 2=TEMP_LOW, 3=BATTERY_LOW, 4=SENSOR_ERROR)
 *   [1]: Severity (0=info, 1=warning, 2=critical)
 *   [2-3]: Value (int16, e.g., temperature in hundredths)
 *   [4-7]: Timestamp (uint32, uptime in ms)
 */

#ifndef SERVICES_HPP_
#define SERVICES_HPP_

#include "blex.hpp"
#include "blex/binary_command.hpp"

// ============================================================================
// SensorService (0x181A) - Environmental Sensing + Battery
// ============================================================================
// BLE Features tested:
// - Read + Notify characteristics
// - Multiple characteristics per service
// - Presentation Format Descriptor (0x2904) with units and exponents
// - User Description Descriptor (0x2901)
// - Signed (int16) and unsigned (uint16, uint8) data types
// - 16-bit SIG-assigned UUIDs
// ============================================================================

namespace detail {

template<typename Blex>
struct SensorServiceImpl {
    // Sensor values (updated by firmware, read/notified to clients)
    inline static int16_t temperature = 2200;   // 22.00°C (hundredths)
    inline static uint16_t humidity = 5500;     // 55.00% (hundredths)
    inline static uint8_t battery_level = 100;  // percentage

    // Subscription tracking
    inline static bool temp_subscribed = false;
    inline static bool humidity_subscribed = false;
    inline static bool battery_subscribed = false;

    // --- Temperature Characteristic (0x2A6E) ---
    // Presentation Format: sint16, exponent -2, unit Celsius
    using TempFormat = Blex::template PresentationFormatDescriptor<
        static_cast<uint8_t>(Blex::GattFormat::kSint16),
        static_cast<int8_t>(-2),
        static_cast<uint16_t>(Blex::GattUnit::kDegreeCelsius),
        0x01, 0x0000
    >;
    static constexpr char temp_desc[] = "Ambient Temperature";
    using TempUserDesc = blex_standard::descriptors::UserDescription<temp_desc>;

    static void onTempRead(int16_t& value) { value = temperature; }
    static void onTempSubscribe(const uint16_t value) {
        temp_subscribed = value != 0;
        Serial.printf("[Sensor] Temp notify: %s\n", temp_subscribed ? "ON" : "OFF");
    }

    using TemperatureChar = Blex::template Characteristic<
        int16_t,
        static_cast<uint16_t>(0x2A6E),
        typename Blex::template Permissions<>::AllowRead::AllowNotify,
        typename Blex::template CharacteristicCallbacks<>
            ::template WithOnRead<onTempRead>
            ::template WithOnSubscribe<onTempSubscribe>,
        TempFormat,
        TempUserDesc
    >;

    // --- Humidity Characteristic (0x2A6F) ---
    // Presentation Format: uint16, exponent -2, unit percentage
    using HumidityFormat = Blex::template PresentationFormatDescriptor<
        static_cast<uint8_t>(Blex::GattFormat::kUint16),
        static_cast<int8_t>(-2),
        static_cast<uint16_t>(Blex::GattUnit::kPercentage),
        0x01, 0x0000
    >;
    static constexpr char humidity_desc[] = "Relative Humidity";
    using HumidityUserDesc = blex_standard::descriptors::UserDescription<humidity_desc>;

    static void onHumidityRead(uint16_t& value) { value = humidity; }
    static void onHumiditySubscribe(const uint16_t value) {
        humidity_subscribed = (value != 0);
        Serial.printf("[Sensor] Humidity notify: %s\n", humidity_subscribed ? "ON" : "OFF");
    }

    using HumidityChar = Blex::template Characteristic<
        uint16_t,
        static_cast<uint16_t>(0x2A6F),
        typename Blex::template Permissions<>::AllowRead::AllowNotify,
        typename Blex::template CharacteristicCallbacks<>
            ::template WithOnRead<onHumidityRead>
            ::template WithOnSubscribe<onHumiditySubscribe>,
        HumidityFormat,
        HumidityUserDesc
    >;

    // --- Battery Level Characteristic (0x2A19) ---
    // Presentation Format: uint8, unit percentage
    using BatteryFormat = Blex::template PresentationFormatDescriptor<
        static_cast<uint8_t>(Blex::GattFormat::kUint8),
        0,
        static_cast<uint16_t>(Blex::GattUnit::kPercentage),
        0x01, 0x0000
    >;
    static constexpr char battery_desc[] = "Battery Level";
    using BatteryUserDesc = blex_standard::descriptors::UserDescription<battery_desc>;

    static void onBatteryRead(uint8_t& value) { value = battery_level; }
    static void onBatterySubscribe(const uint16_t value) {
        battery_subscribed = (value != 0);
        Serial.printf("[Sensor] Battery notify: %s\n", battery_subscribed ? "ON" : "OFF");
    }

    using BatteryLevelChar = Blex::template Characteristic<
        uint8_t,
        static_cast<uint16_t>(0x2A19),
        typename Blex::template Permissions<>::AllowRead::AllowNotify,
        typename Blex::template CharacteristicCallbacks<>
            ::template WithOnRead<onBatteryRead>
            ::template WithOnSubscribe<onBatterySubscribe>,
        BatteryFormat,
        BatteryUserDesc
    >;
};

} // namespace detail

/**
 * @brief Environmental Sensor Service (0x181A)
 *
 * Combines environmental sensing (temperature, humidity) with battery level
 * monitoring in a single service for efficient discovery.
 */
template<typename Blex, typename C = detail::SensorServiceImpl<Blex>>
struct SensorService : C, Blex::template Service<
    static_cast<uint16_t>(0x181A),
    typename C::TemperatureChar,
    typename C::HumidityChar,
    typename C::BatteryLevelChar
> {
    // --- Setters (called by firmware to update values and notify) ---
    static void setTemperature(int16_t hundredths) {
        C::temperature = hundredths;
        C::TemperatureChar::setValue(hundredths);
    }

    static void setHumidity(uint16_t hundredths) {
        C::humidity = hundredths;
        C::HumidityChar::setValue(hundredths);
    }

    static void setBatteryLevel(uint8_t percent) {
        C::battery_level = percent;
        C::BatteryLevelChar::setValue(percent);
    }

    // --- Getters ---
    static int16_t getTemperature() { return C::temperature; }
    static uint16_t getHumidity() { return C::humidity; }
    static uint8_t getBatteryLevel() { return C::battery_level; }

    // --- Subscription state ---
    static bool isTempSubscribed() { return C::temp_subscribed; }
    static bool isHumiditySubscribed() { return C::humidity_subscribed; }
    static bool isBatterySubscribed() { return C::battery_subscribed; }
};

// ============================================================================
// ControlService (vendor UUID) - Device Management & Control
// ============================================================================
// BLE Features tested:
// - Write No Response (fast command input)
// - Notify (command responses, non-critical updates)
// - Indicate (critical alerts requiring acknowledgement)
// - Encrypted Write (protected configuration)
// - Long Read (512-byte diagnostic buffer)
// - 128-bit vendor-specific UUIDs
// - onSubscribe callback (subscription tracking)
// - Various data types (structs, arrays)
// ============================================================================

namespace detail {

// Vendor-specific 128-bit UUIDs
constexpr char CONTROL_SERVICE_UUID[] = "E5700001-7BAC-429A-B4CE-57FF900F479D";
constexpr char CMD_REGISTER_UUID[]    = "E5700002-7BAC-429A-B4CE-57FF900F479D";
constexpr char CMD_RESPONSE_UUID[]    = "E5700003-7BAC-429A-B4CE-57FF900F479D";
constexpr char ALERT_UUID[]           = "E5700004-7BAC-429A-B4CE-57FF900F479D";
constexpr char CONFIG_UUID[]          = "E5700005-7BAC-429A-B4CE-57FF900F479D";
constexpr char DIAG_LOG_UUID[]        = "E5700006-7BAC-429A-B4CE-57FF900F479D";

// Command IDs
enum class Command : uint8_t {
    StartSampling    = 0x01,
    StopSampling     = 0x02,
    SetInterval      = 0x03,  // param: uint16_t interval_ms
    SetAlertThresh   = 0x04,  // param: int16_t temp_high, int16_t temp_low
    RequestDiagDump  = 0x05,
    ClearDiagLog     = 0x06,
    Reset            = 0xFF
};

// Response status codes
enum class Status : uint8_t {
    OK            = 0x00,
    Error         = 0x01,
    InvalidParam  = 0x02,
    InvalidCmd    = 0x03
};

// Alert types
enum class AlertType : uint8_t {
    TempHigh    = 0x01,
    TempLow     = 0x02,
    BatteryLow  = 0x03,
    SensorError = 0x04
};

#pragma pack(push, 1)

/// @brief Payload for SetInterval command (0x03)
struct SetIntervalPayload {
    uint16_t interval_ms;
};

/// @brief Payload for SetAlertThresh command (0x04)
struct SetAlertThreshPayload {
    int16_t temp_high;
    int16_t temp_low;
};

/// @brief Response packet sent via Notify characteristic
struct ResponsePacket {
    uint8_t cmd_id;
    uint8_t status;
    uint8_t data[14];
};

/// @brief Alert packet sent via Indicate characteristic
struct AlertPacket {
    uint8_t alert_type;
    uint8_t severity;    ///< 0=info, 1=warning, 2=critical
    int16_t value;       ///< Associated value (e.g., temperature in hundredths)
    uint32_t timestamp;  ///< Uptime in ms
};

/// @brief Device configuration (protected by encryption)
struct ConfigData {
    uint16_t sample_interval_ms;
    int16_t temp_alert_high;
    int16_t temp_alert_low;
    uint8_t battery_alert_level;
    uint8_t flags;
};

#pragma pack(pop)

template<typename Blex>
struct ControlServiceImpl {
    // State
    inline static bool sampling_enabled = false;
    inline static bool response_subscribed = false;
    inline static bool alert_subscribed = false;

    // Configuration (protected by encryption)
    inline static ConfigData config = {
        .sample_interval_ms = 1000,
        .temp_alert_high = 3500,    // 35.00°C
        .temp_alert_low = 500,      // 5.00°C
        .battery_alert_level = 20,  // 20%
        .flags = 0x01               // Alerts enabled
    };

    // Diagnostic log buffer (for long read testing)
    inline static uint8_t diag_log[512];
    inline static size_t diag_log_len = 0;
    inline static bool diag_initialized = false;

    // Response state - used by handlers to build response
    inline static uint8_t pending_cmd_id = 0;
    inline static Status pending_status = Status::OK;

    // =========================================================================
    // Command Handlers (typed, dispatched by blex binary command)
    // =========================================================================

    /// @brief Sends response via Notify characteristic
    static void sendResponse() {
        ResponsePacket resp = {
            .cmd_id = pending_cmd_id,
            .status = static_cast<uint8_t>(pending_status),
            .data = {}
        };
        CmdResponseChar::setValue(reinterpret_cast<const uint8_t*>(&resp), sizeof(resp));
    }

    /// @brief Handler for StartSampling (0x01) - no payload
    static void onStartSampling() {
        sampling_enabled = true;
        Serial.println("[Control] Sampling started");
        pending_status = Status::OK;
        sendResponse();
    }

    /// @brief Handler for StopSampling (0x02) - no payload
    static void onStopSampling() {
        sampling_enabled = false;
        Serial.println("[Control] Sampling stopped");
        pending_status = Status::OK;
        sendResponse();
    }

    /// @brief Handler for SetInterval (0x03) - typed payload
    static void onSetInterval(const SetIntervalPayload& payload) {
        if (payload.interval_ms >= 100 && payload.interval_ms <= 60000) {
            config.sample_interval_ms = payload.interval_ms;
            Serial.printf("[Control] Interval set to %dms\n", payload.interval_ms);
            pending_status = Status::OK;
        } else {
            pending_status = Status::InvalidParam;
        }
        sendResponse();
    }

    /// @brief Handler for SetAlertThresh (0x04) - typed payload
    static void onSetAlertThresh(const SetAlertThreshPayload& payload) {
        if (payload.temp_high > payload.temp_low) {
            config.temp_alert_high = payload.temp_high;
            config.temp_alert_low = payload.temp_low;
            Serial.printf("[Control] Thresholds: high=%d, low=%d\n",
                          payload.temp_high, payload.temp_low);
            pending_status = Status::OK;
        } else {
            pending_status = Status::InvalidParam;
        }
        sendResponse();
    }

    /// @brief Handler for RequestDiagDump (0x05) - no payload
    static void onRequestDiagDump() {
        initDiagLog();
        Serial.println("[Control] Diagnostic dump requested");
        pending_status = Status::OK;
        sendResponse();
    }

    /// @brief Handler for ClearDiagLog (0x06) - no payload
    static void onClearDiagLog() {
        diag_log_len = 0;
        memset(diag_log, 0, sizeof(diag_log));
        Serial.println("[Control] Diagnostic log cleared");
        pending_status = Status::OK;
        sendResponse();
    }

    /// @brief Handler for Reset (0xFF) - no payload
    static void onReset() {
        Serial.println("[Control] Reset requested (simulated)");
        pending_status = Status::OK;
        sendResponse();
    }

    /// @brief Fallback handler for unknown opcodes or payload errors
    static void onDispatchError(uint8_t opcode, blex_binary_command::DispatchError error) {
        using DispatchError = blex_binary_command::DispatchError;

        pending_cmd_id = opcode;
        switch (error) {
            case DispatchError::unknown_opcode:
                Serial.printf("[Control] Unknown command: 0x%02X\n", opcode);
                pending_status = Status::InvalidCmd;
                break;
            case DispatchError::payload_too_small:
            case DispatchError::payload_too_big:
            case DispatchError::invalid_payload:
                Serial.printf("[Control] Invalid payload for command 0x%02X\n", opcode);
                pending_status = Status::InvalidParam;
                break;
            case DispatchError::invalid_message:
                Serial.println("[Control] Invalid message received");
                pending_status = Status::Error;
                break;
            default:
                pending_status = Status::Error;
                break;
        }
        sendResponse();
    }

    // =========================================================================
    // Command Dispatcher (blex binary command)
    // =========================================================================

    using CmdStartSampling   = blex_binary_command::Command<
        static_cast<uint8_t>(Command::StartSampling), onStartSampling>;
    using CmdStopSampling    = blex_binary_command::Command<
        static_cast<uint8_t>(Command::StopSampling), onStopSampling>;
    using CmdSetInterval     = blex_binary_command::Command<
        static_cast<uint8_t>(Command::SetInterval), onSetInterval>;
    using CmdSetAlertThresh  = blex_binary_command::Command<
        static_cast<uint8_t>(Command::SetAlertThresh), onSetAlertThresh>;
    using CmdRequestDiagDump = blex_binary_command::Command<
        static_cast<uint8_t>(Command::RequestDiagDump), onRequestDiagDump>;
    using CmdClearDiagLog    = blex_binary_command::Command<
        static_cast<uint8_t>(Command::ClearDiagLog), onClearDiagLog>;
    using CmdReset           = blex_binary_command::Command<
        static_cast<uint8_t>(Command::Reset), onReset>;

    using CommandDispatcher = blex_binary_command::Dispatcher<
        CmdStartSampling,
        CmdStopSampling,
        CmdSetInterval,
        CmdSetAlertThresh,
        CmdRequestDiagDump,
        CmdClearDiagLog,
        CmdReset,
        blex_binary_command::Fallback<onDispatchError>
    >;

    // =========================================================================
    // BLE Characteristic callback (dispatches to typed handlers)
    // =========================================================================

    /// @brief Characteristic write callback - dispatches via blex binary command
    static void onCommandWrite(const uint8_t* data, size_t len) {
        if (len > 0) {
            pending_cmd_id = data[0];
            Serial.printf("[Control] Command received: 0x%02X\n", pending_cmd_id);
        }
        CommandDispatcher::dispatch(data, len);
    }

    static constexpr char cmd_desc[] = "Command Register";
    using CmdUserDesc = blex_standard::descriptors::UserDescription<cmd_desc>;

    using CmdRegisterChar = Blex::template Characteristic<
        uint8_t[CommandDispatcher::max_message_size],
        CMD_REGISTER_UUID,
        typename Blex::template Permissions<>::AllowWriteNoResponse,
        typename Blex::template CharacteristicCallbacks<>::template WithOnWrite<onCommandWrite>,
        CmdUserDesc
    >;

    // --- Command Response (Notify) ---
    // Non-critical responses - client may miss if not subscribed
    static void onResponseSubscribe(const uint16_t value) {
        response_subscribed = (value != 0);
        Serial.printf("[Control] Response notify: %s\n", response_subscribed ? "ON" : "OFF");
    }

    static constexpr char resp_desc[] = "Command Response";
    using RespUserDesc = blex_standard::descriptors::UserDescription<resp_desc>;

    using CmdResponseChar = Blex::template Characteristic<
        ResponsePacket,
        CMD_RESPONSE_UUID,
        typename Blex::template Permissions<>::AllowNotify,
        typename Blex::template CharacteristicCallbacks<>::template WithOnSubscribe<onResponseSubscribe>,
        RespUserDesc
    >;

    // --- Alert (Indicate) ---
    // Critical alerts - requires client acknowledgement
    static void onAlertSubscribe(const uint16_t value) {
        alert_subscribed = value != 0;
        Serial.printf("[Control] Alert indicate: %s\n", alert_subscribed ? "ON" : "OFF");
    }

    static constexpr char alert_desc[] = "Critical Alerts";
    using AlertUserDesc = blex_standard::descriptors::UserDescription<alert_desc>;

    using AlertChar = Blex::template Characteristic<
        AlertPacket,
        ALERT_UUID,
        typename Blex::template Permissions<>::AllowIndicate,
        typename Blex::template CharacteristicCallbacks<>::template WithOnSubscribe<onAlertSubscribe>,
        AlertUserDesc
    >;

    // --- Configuration (Read + Encrypted Write) ---
    // Protected settings - requires encrypted connection to modify
    static void onConfigRead(ConfigData& value) {
        value = config;
    }

    static void onConfigWrite(const ConfigData& value) {
        config = value;
        Serial.printf("[Control] Config updated: interval=%dms, thresh=%d/%d\n",
                      config.sample_interval_ms, config.temp_alert_high, config.temp_alert_low);
    }

    static constexpr char config_desc[] = "Device Configuration";
    using ConfigUserDesc = blex_standard::descriptors::UserDescription<config_desc>;

    using ConfigChar = Blex::template Characteristic<
        ConfigData,
        CONFIG_UUID,
        typename Blex::template Permissions<>::AllowRead::AllowEncryptedWrite,
        typename Blex::template CharacteristicCallbacks<>
            ::template WithOnRead<onConfigRead>
            ::template WithOnWrite<onConfigWrite>,
        ConfigUserDesc
    >;

    // --- Diagnostic Log (Long Read) ---
    // 512-byte buffer - tests ATT long read (multiple read blob requests)
    static void initDiagLog() {
        if (!diag_initialized || diag_log_len == 0) {
            // Fill with sample diagnostic data
            const auto header = "=== DIAGNOSTIC LOG ===\n";
            size_t pos = 0;

            memcpy(diag_log + pos, header, strlen(header));
            pos += strlen(header);

            // Add some realistic log entries
            const char* entries[] = {
                "00:00:00 [INFO] Device booted\n",
                "00:00:01 [INFO] BLE initialized\n",
                "00:00:02 [INFO] Sensors ready\n",
                "00:00:10 [DATA] Temp=22.50C Hum=55.0%\n",
                "00:00:20 [DATA] Temp=22.48C Hum=55.2%\n",
                "00:00:30 [DATA] Temp=22.52C Hum=54.8%\n",
                "00:01:00 [INFO] Connection established\n",
                "00:01:05 [INFO] Services discovered\n",
            };

            for (const char* entry : entries) {
                if (const size_t len = strlen(entry); pos + len < sizeof(diag_log)) {
                    memcpy(diag_log + pos, entry, len);
                    pos += len;
                }
            }

            // Fill remaining with pattern for long read testing
            while (pos < sizeof(diag_log) - 20) {
                const int written = snprintf(reinterpret_cast<char*>(diag_log + pos),
                                       sizeof(diag_log) - pos,
                                       "[PAD] offset=%03zu\n", pos);
                if (written <= 0) break;
                pos += written;
            }

            diag_log_len = pos;
            diag_initialized = true;
        }
    }

    static void onDiagLogRead(uint8_t (&value)[512]) {
        initDiagLog();
        memcpy(value, diag_log, sizeof(diag_log));
    }

    static constexpr char diag_desc[] = "Diagnostic Log";
    using DiagUserDesc = blex_standard::descriptors::UserDescription<diag_desc>;

    using DiagLogChar = Blex::template Characteristic<
        uint8_t[512],
        DIAG_LOG_UUID,
        typename Blex::template Permissions<>::AllowRead,
        typename Blex::template CharacteristicCallbacks<>::template WithOnRead<onDiagLogRead>,
        DiagUserDesc
    >;
};

} // namespace detail

/**
 * @brief Device Control Service (vendor-specific)
 *
 * Provides device management interface:
 * - Command/response pattern for device control
 * - Critical alerts via indications
 * - Protected configuration
 * - Diagnostic logging
 */
template<typename Blex, typename C = detail::ControlServiceImpl<Blex>>
struct ControlService : C, Blex::template Service<
    detail::CONTROL_SERVICE_UUID,
    typename C::CmdRegisterChar,
    typename C::CmdResponseChar,
    typename C::AlertChar,
    typename C::ConfigChar,
    typename C::DiagLogChar
> {
    // --- Alert API ---
    static void sendAlert(detail::AlertType type, uint8_t severity, const int16_t value) {
        const detail::AlertPacket alert = {
            .alert_type = static_cast<uint8_t>(type),
            .severity = severity,
            .value = value,
            .timestamp = millis()
        };
        C::AlertChar::setValue(reinterpret_cast<const uint8_t*>(&alert), sizeof(alert));
        Serial.printf("[Control] Alert sent: type=%d, severity=%d, value=%d\n",
                      static_cast<int>(type), severity, value);
    }

    static void sendTempHighAlert(const int16_t temp) {
        sendAlert(detail::AlertType::TempHigh, 2, temp);
    }

    static void sendTempLowAlert(const int16_t temp) {
        sendAlert(detail::AlertType::TempLow, 2, temp);
    }

    static void sendBatteryLowAlert(const uint8_t level) {
        sendAlert(detail::AlertType::BatteryLow, 1, level);
    }

    // --- State accessors ---
    static bool isSamplingEnabled() { return C::sampling_enabled; }
    static bool isResponseSubscribed() { return C::response_subscribed; }
    static bool isAlertSubscribed() { return C::alert_subscribed; }

    static uint16_t getSampleInterval() { return C::config.sample_interval_ms; }
    static int16_t getTempAlertHigh() { return C::config.temp_alert_high; }
    static int16_t getTempAlertLow() { return C::config.temp_alert_low; }

    // --- Diagnostic ---
    static void appendDiagLog(const char* entry) {
        size_t len = strlen(entry);
        if (C::diag_log_len + len < sizeof(C::diag_log)) {
            memcpy(C::diag_log + C::diag_log_len, entry, len);
            C::diag_log_len += len;
        }
    }
};

#endif // SERVICES_HPP_