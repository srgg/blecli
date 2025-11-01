/**
* BLEX — Compile-Time BLE Meta-Framework maximizes C++17 features usage for the sake of API simplicity
 *
 * Provides a fully declarative, zero-runtime BLE framework for embedded devices.
 *
 * Features:
 *   - Type-level Descriptors & Characteristics
 *       Define BLE descriptors and characteristics entirely at compile-time.
 *       Supports static default values, permissions, and compile-time validation.
 *
 *   - Type-level Services
 *       Combine characteristics into BLE services with guaranteed correctness.
 *
 *   - Static Callback Shims
 *       No dynamic memory or heap allocations required.
 *       Compile-time checked read/write/notify handlers.
 *
 *   - Automatic Integration with NimBLE
 *       Optional runtime registration to NimBLEServer, no boilerplate callbacks.
 *
 * Usage:
 *   1. Define characteristics and descriptors with `Characteristic` or `ConstCharacteristic`.
 *   2. Combine them into `Service`.
 *   3. Instantiate `Server` with device name and services.
 *
 * Goals:
 *   - Minimize runtime overhead and heap usage
 *   - Reduce boilerplate for BLE service definitions
 *   - Maintain strong compile-time guarantees
 *   - Policy-based design with zero-overhead abstractions
 *
 * Multi-Threading/Multi-Core Safety:
 *
 * Policy-Based Design:
 *   - Template class `blex` parameterized by lock policies
 *   - LockPolicy: For function pointer protection
 *   - Auto-detects platform (ESP32 → FreeRTOS, others → NoLock)
 *
 * Thread-Safety Guarantees:
 *   - BLE callbacks automatically protected with per-characteristic critical sections
 *   - Different characteristics can execute in parallel on multi-core
 *   - SafePtr: Lock-free atomic pointer with write-once enforcement
 *   - All policies configurable via template parameters
 *
 * Example:
 *   using MyBlex = blex<>;  // Default policies (auto-detected)
 *   using MyChar = MyBlex::Characteristic<uint8_t, 0x1234, MyBlex::Readable>;
 *
 * Requires: NimBLE-Arduino (for runtime registration)
 */

#ifndef BLEX_HPP_
#define BLEX_HPP_

#include "blex/platform.hpp"
#include "blex/core.hpp"
#include "blex/nimble.hpp"

// ---------------------- BLEX Template Class ----------------------

template<
    template<typename> class LockPolicy = DefaultLock
>
struct blex {
    // Re-export core types
    using BleAppearance = ::BleAppearance;
    using GattFormat = ::GattFormat;
    using GattUnit = ::GattUnit;
    using PresentationFormatValue = ::PresentationFormatValue;

    // Re-export permission types
    using Readable = ::Readable;
    using Writable = ::Writable;
    using Notifiable = ::Notifiable;
    template<typename... Perms>
    using Permissions = ::Permissions<Perms...>;

    // Re-export config templates
    template<int8_t TxPower = 0, uint16_t IntervalMin = 100, uint16_t IntervalMax = 150,
             uint16_t Appearance = static_cast<uint16_t>(BleAppearance::kUnknown)>
    using AdvertisingConfig = ::AdvertisingConfig<TxPower, IntervalMin, IntervalMax, Appearance>;

    template<
        uint16_t MTU = 247,
        uint16_t ConnIntervalMin = 12,
        uint16_t ConnIntervalMax = 12,
        uint16_t ConnLatency = 0,
        uint16_t SupervisionTimeout = 400
    >
    using ConnectionConfig = ::ConnectionConfig<MTU, ConnIntervalMin, ConnIntervalMax, ConnLatency, SupervisionTimeout>;

    // Helper variable template
    template<typename T, T Val = T{}>
    static constexpr size_t value_storage_size_v = blex_core::value_storage_size<T, Val>::value;

    // Advertising service wrappers
    template<typename Svc>
    struct PassiveAdvService {
        using service_type = Svc;
        static constexpr bool passive_adv = true;
        static constexpr bool active_adv = false;
    };

    template<typename Svc>
    struct ActiveAdvService {
        using service_type = Svc;
        static constexpr bool passive_adv = false;
        static constexpr bool active_adv = true;
    };

    template<typename Svc>
    struct BothAdvService {
        using service_type = Svc;
        static constexpr bool passive_adv = true;
        static constexpr bool active_adv = true;
    };

    // Service wrapper checks
    template<typename T>
    static constexpr bool is_passive_adv_v = blex_core::is_passive_adv_pred<T>::value;

    template<typename T>
    static constexpr bool is_active_adv_v = blex_core::is_active_adv_pred<T>::value;

    // Re-export descriptors and characteristics (they now need LockPolicy)
    template<typename T, auto UUID, T Value, typename Perms = Permissions<Readable>, size_t MaxSize = value_storage_size_v<T, Value>>
    using ConstDescriptor = ::ConstDescriptor<T, UUID, Value, Perms, MaxSize>;

    template<uint8_t Format, int8_t Exponent, uint16_t Unit, uint8_t Namespace, uint16_t Description>
    using PresentationFormatDescriptor = ::PresentationFormatDescriptor<Format, Exponent, Unit, Namespace, Description>;

    template<typename... PresentationFormatDescriptors>
    using AggregateFormatDescriptor = ::AggregateFormatDescriptor<PresentationFormatDescriptors...>;

    template<typename T, const char* UUIDLiteral, typename Perms = Permissions<Readable>, size_t MaxSize = sizeof(T)>
    using Descriptor = ::Descriptor<T, UUIDLiteral, Perms, MaxSize>;

    template<typename T, auto UUID, T Value, typename... Descriptors>
    using ConstCharacteristic = ::ConstCharacteristic<T, UUID, Value, Descriptors...>;

    template<
        typename T,
        auto UUID,
        typename Perms,
        auto OnRead = nullptr,
        auto OnWrite = nullptr,
        auto OnStatus = nullptr,
        auto OnSubscribe = nullptr,
        typename... Descriptors
    >
    using Characteristic = ::Characteristic<T, UUID, Perms, OnRead, OnWrite, OnStatus, OnSubscribe, Descriptors...>;

    template<auto UUID, typename... Chars>
    using Service = ::Service<UUID, Chars...>;

    // Extract config helpers
    template<typename... Args>
    using extract_adv_config = typename blex_core::extract_adv_config<Args...>::type;

    template<typename... Args>
    using extract_conn_config = typename blex_core::extract_conn_config<Args...>::type;

    // BlexAdvertising: Applies advertising configuration to NimBLE
    template<typename PassiveServicesTuple, typename ActiveServicesTuple, typename AdvConfig = AdvertisingConfig<>>
    struct BlexAdvertising {
        // Use configuration from AdvConfig template parameter
        static constexpr int8_t default_tx_power = AdvConfig::default_tx_power;
        static constexpr uint16_t default_adv_interval_min = AdvConfig::default_adv_interval_min;
        static constexpr uint16_t default_adv_interval_max = AdvConfig::default_adv_interval_max;
        static constexpr uint16_t default_appearance = AdvConfig::default_appearance;
        static constexpr uint8_t default_flags = AdvConfig::default_flags;

        static constexpr int8_t min_tx_power = AdvConfig::min_tx_power;
        static constexpr int8_t max_tx_power = AdvConfig::max_tx_power;
        static constexpr uint16_t min_adv_interval = AdvConfig::min_adv_interval;
        static constexpr uint16_t max_adv_interval = AdvConfig::max_adv_interval;

#ifdef BLEX_NIMBLE_AVAILABLE
        static void configure(NimBLEAdvertising* advertising,
                            const char* device_name,
                            const char* short_name,
                            const int8_t tx_power_override = default_tx_power,
                            const uint16_t interval_min_override = 0,
                            const uint16_t interval_max_override = 0) {
            if (!advertising) return;

            // Enable scan response for extended data
            advertising->enableScanResponse(true);

            // Configure advertisement data (passive services + short name)
            NimBLEAdvertisementData adv_data;
            adv_data.setFlags(default_flags);
            adv_data.setName(short_name, false);
            blex_nimble::add_service_uuids_impl(adv_data, PassiveServicesTuple{});
            advertising->setAdvertisementData(adv_data);

            // Configure scan response data (active services + full name)
            NimBLEAdvertisementData scan_resp;
            scan_resp.setName(device_name, true);
            blex_nimble::add_service_uuids_impl(scan_resp, ActiveServicesTuple{});
            advertising->setScanResponseData(scan_resp);

            // Apply TX power (use override if valid, otherwise use default)
            const int8_t tx_power = tx_power_override >= min_tx_power && tx_power_override <= max_tx_power
                                   ? tx_power_override
                                   : default_tx_power;
            // Only set TX power if not using sentinel value (-127 = use NimBLE default)
            if (tx_power != -127) {
                NimBLEDevice::setPower(tx_power);
            }

            // Apply advertising intervals (use overrides if valid, otherwise use defaults)
            const uint16_t interval_min = interval_min_override >= min_adv_interval &&
                                          interval_min_override <= max_adv_interval
                                         ? interval_min_override
                                         : default_adv_interval_min;
            const uint16_t interval_max = interval_max_override >= min_adv_interval &&
                                          interval_max_override <= max_adv_interval
                                         ? interval_max_override
                                         : default_adv_interval_max;

            // Only set intervals if not using sentinel values (0 = use NimBLE defaults)
            if (interval_min != 0 && interval_max != 0) {
                // NimBLE uses 0.625ms units, so convert milliseconds
                advertising->setMinInterval(interval_min * 16 / 10);
                advertising->setMaxInterval(interval_max * 16 / 10);
            }

            // Set BLE appearance in advertising packet
            if constexpr (default_appearance != 0x0000) {
                advertising->setAppearance(default_appearance);
            }
        }
#endif
    };

    // Server
    template<
        const char* DeviceName,
        const char* ShortName,
        typename... Args
    >
    struct Server {
        // Extract AdvConfig, ConnConfig, and Services from variadic Args
        using AdvConfig = typename blex_core::extract_adv_config<Args...>::type;
        using ConnConfig = typename blex_core::extract_conn_config<Args...>::type;
        using ServicesTuple = typename blex_core::filter_non_config<Args...>::type;

        static inline blex_sync::SafePtr<NimBLEServer, Server, LockPolicy, true> server;
        static inline blex_sync::SafePtr<NimBLEAdvertising, Server, LockPolicy, true> adv;

        // Runtime tuning state (static storage, no heap)
        static inline int8_t runtime_tx_power_ = -127;           // Sentinel: not set
        static inline uint16_t runtime_adv_interval_min_ = 0;    // 0 = not set, use default
        static inline uint16_t runtime_adv_interval_max_ = 0;    // 0 = not set, use default

        struct Callbacks final : NimBLEServerCallbacks {
            void onConnect(NimBLEServer* pServer, NimBLEConnInfo& conn) override {
                Serial.printf("🔗 Connected: %s\n", conn.getAddress().toString().c_str());
            }

            void onDisconnect(NimBLEServer*, NimBLEConnInfo& conn, const int reason) override {
                Serial.printf("❌ Disconnected (reason=%d)\n", reason);
                NimBLEDevice::startAdvertising();
                Serial.println("📡 Advertising restarted");
            }

            void onMTUChange(const uint16_t MTU, NimBLEConnInfo& conn) override {
                Serial.printf("📏 MTU updated: %u bytes for %s\n", MTU, conn.getAddress().toString().c_str());

                // Request connection parameters from compile-time configuration
                if (auto* pServer = server.get()) {
                    pServer->updateConnParams(
                        conn.getConnHandle(),
                        ConnConfig::conn_interval_min,
                        ConnConfig::conn_interval_max,
                        ConnConfig::conn_latency,
                        ConnConfig::supervision_timeout
                    );
                    Serial.printf("📊 Requested connection parameters: interval=%u-%u (%.1f-%.1fms), latency=%u, timeout=%u (%.1fs)\n",
                                ConnConfig::conn_interval_min, ConnConfig::conn_interval_max,
                                ConnConfig::conn_interval_min * 1.25f, ConnConfig::conn_interval_max * 1.25f,
                                ConnConfig::conn_latency,
                                ConnConfig::supervision_timeout, ConnConfig::supervision_timeout * 10.0f / 1000.0f);
                }
            }
        };

        static bool init() {
            static std::atomic_flag init_called = ATOMIC_FLAG_INIT;
            if (init_called.test_and_set(std::memory_order_acq_rel)) {
                Serial.println("[BLEX] init: already initialized, returning");
                return server.get() != nullptr;
            }

            Serial.println("🟢 Initializing BLE server...");
            Serial.println("[BLEX] init: calling NimBLEDevice::init");
            NimBLEDevice::init(DeviceName);

            // Set BLE appearance in GAP service
            if constexpr (AdvConfig::default_appearance != 0x0000) {
                Serial.printf("[BLEX] init: setting GAP appearance to 0x%04X\n", AdvConfig::default_appearance);
                ble_svc_gap_device_appearance_set(AdvConfig::default_appearance);
            }

            // Only set MTU if not using sentinel value
            if (ConnConfig::mtu != 0) {
                Serial.printf("[BLEX] init: calling setMTU(%u)\n", ConnConfig::mtu);
                NimBLEDevice::setMTU(ConnConfig::mtu);
            }

            Serial.println("[BLEX] init: creating server");
            server.set(NimBLEDevice::createServer());
            if (!server) {
                Serial.println("❌ Failed to create BLE server");
                return false;
            }

            Serial.println("[BLEX] init: setting callbacks");
            static Callbacks callbacks;
            server.with_lock([&](auto* s) { if (s) s->setCallbacks(&callbacks); });
            Serial.println("[BLEX] init: getting advertising");
            adv.set(NimBLEDevice::getAdvertising());

            Serial.println("[BLEX] init: registering services");
            register_all_services(ServicesTuple{});
            Serial.println("[BLEX] init: starting services");
            start_all_services(ServicesTuple{});

            Serial.println("[BLEX] init: configuring advertising");
            configureAdvertising();

            Serial.println("[BLEX] init: starting advertising");
            NimBLEDevice::startAdvertising();
            Serial.printf("✅ BLE ready (%s)\n", DeviceName);
            return true;
        }

        static bool setTxPower(int8_t dbm) {
            using advConfig = BlexAdvertising<std::tuple<>, std::tuple<>>;
            if (dbm < advConfig::min_tx_power || dbm > advConfig::max_tx_power) {
                Serial.printf("❌ TX power %d dBm out of range [%d, %d]\n",
                            dbm, advConfig::min_tx_power, advConfig::max_tx_power);
                return false;
            }
            runtime_tx_power_ = dbm;
            Serial.printf("✓ TX power set to %d dBm (call updateAdvertising to apply)\n", dbm);
            return true;
        }

        static bool setAdvInterval(uint16_t min_ms, uint16_t max_ms) {
            using advConfig = BlexAdvertising<std::tuple<>, std::tuple<>>;
            if (min_ms < advConfig::min_adv_interval || min_ms > advConfig::max_adv_interval ||
                max_ms < advConfig::min_adv_interval || max_ms > advConfig::max_adv_interval ||
                min_ms > max_ms) {
                Serial.printf("❌ Advertising interval out of range or min > max\n");
                return false;
            }
            runtime_adv_interval_min_ = min_ms;
            runtime_adv_interval_max_ = max_ms;
            Serial.printf("✓ Advertising interval set to [%u, %u] ms (call updateAdvertising to apply)\n",
                        min_ms, max_ms);
            return true;
        }

        static void updateAdvertising() {
            auto* advertising = adv.get();
            if (!advertising) {
                Serial.println("❌ Advertising not initialized");
                return;
            }

            Serial.println("📡 Updating advertising configuration...");
            NimBLEDevice::stopAdvertising();

            using PassiveServices = typename blex_core::filter_services_pack<blex_core::is_passive_adv_pred, ServicesTuple>::type;
            using ActiveServices = typename blex_core::filter_services_pack<blex_core::is_active_adv_pred, ServicesTuple>::type;
            using Adv = BlexAdvertising<PassiveServices, ActiveServices, AdvConfig>;

            Adv::configure(adv.get(), DeviceName, ShortName,
                               runtime_tx_power_,
                               runtime_adv_interval_min_,
                               runtime_adv_interval_max_);

            NimBLEDevice::startAdvertising();
            Serial.println("✓ Advertising updated and restarted");
        }

    private:
        template<typename... Services>
        using ServicesPack = typename blex_core::template ServicesPack<Services...>;

        template<typename ServiceOrWrapped>
        static void register_service() {
            using ActualService = typename blex_core::template unwrap_service_impl<ServiceOrWrapped>::type;
            blex::register_service<ActualService>(server.get(), adv.get());
        }

        template<typename ServiceOrWrapped>
        static void start_service() {
            using ActualService = typename blex_core::template unwrap_service_impl<ServiceOrWrapped>::type;
            if (auto* srv = server.get()) {
                if (auto* s = srv->getServiceByUUID(blex_nimble::template make_uuid<ActualService::uuid>())) {
                    s->start();
                }
            }
        }

        template<typename... Services>
        static void register_all_services(ServicesPack<Services...>) {
            (register_service<Services>(), ...);
        }

        template<typename... Services>
        static void start_all_services(ServicesPack<Services...>) {
            (start_service<Services>(), ...);
        }

        static void configureAdvertising() {
            using PassiveServices = typename blex_core::filter_services_pack<blex_core::is_passive_adv_pred, ServicesTuple>::type;
            using ActiveServices = typename blex_core::filter_services_pack<blex_core::is_active_adv_pred, ServicesTuple>::type;
            using Adv = BlexAdvertising<PassiveServices, ActiveServices, AdvConfig>;

            Adv::configure(adv.get(), DeviceName, ShortName);
        }
    };

    // NimBLE Registration
#ifdef BLEX_NIMBLE_AVAILABLE
    template<typename... Descriptors>
    static void register_all_descriptors(NimBLECharacteristic* pC, typename blex_core::template DescriptorsPack<Descriptors...>) {
        (Descriptors::register_descriptor(pC), ...);
    }

    template<typename CharT>
    static NimBLECharacteristic* register_characteristic(NimBLEService* svc) {
        NimBLECharacteristic* pC = svc->createCharacteristic(
            blex_nimble::template make_uuid<CharT::uuid>(),
            (CharT::perms_type::canRead    ? NIMBLE_PROPERTY::READ  : 0) |
            (CharT::perms_type::canWrite   ? NIMBLE_PROPERTY::WRITE : 0) |
            (CharT::perms_type::canNotify  ? NIMBLE_PROPERTY::NOTIFY: 0)
        );

        if constexpr (CharT::is_const_characteristic) {
            if constexpr (std::is_same_v<typename CharT::value_type, std::string> ||
                          std::is_same_v<typename CharT::value_type, const char*>)
                pC->setValue(CharT::value);
            else
                pC->setValue(reinterpret_cast<const uint8_t*>(&CharT::value), sizeof(typename CharT::value_type));
        }

        register_all_descriptors(pC, typename CharT::descriptors_pack{});

        if constexpr (!CharT::is_const_characteristic) {
            CharT::pChar.set(pC);

            if constexpr (
                CharT::ReadHandler != nullptr ||
                CharT::WriteHandler != nullptr ||
                CharT::StatusHandler != nullptr ||
                CharT::SubscribeHandler != nullptr
            ) {
                static typename blex_nimble::template BLECharShim<CharT, LockPolicy> shim;
                pC->setCallbacks(&shim);
            }
        }

        return pC;
    }

    template<typename... Chars>
    static void register_all_characteristics(NimBLEService* svc, typename blex_core::template CharsPack<Chars...>) {
        (register_characteristic<Chars>(svc), ...);
    }

    template<typename ServiceT>
    static NimBLEService* register_service(NimBLEServer* server, NimBLEAdvertising* adv) {
        ServiceT::validate();

        NimBLEService* svc = server->createService(blex_nimble::template make_uuid<ServiceT::uuid>());

        register_all_characteristics(svc, typename ServiceT::chars_pack{});

        auto onAdvertiseStartFunc = ServiceT::template onAdvertiseStart<LockPolicy>;
        if (onAdvertiseStartFunc) {
            if (adv) onAdvertiseStartFunc();
        }

        return svc;
    }
#else
    #error "blex: No BLE binding framework available. Include NimBLE headers before blex.hpp"
#endif

    // Helper structs (namespaces cannot be inside classes)
    struct descriptors {
        template<const char* DescText>
        using UserDescription = ConstDescriptor<const char*, 0x2901, DescText>;

        template<
            GattFormat Format,
            int8_t Exponent,
            GattUnit Unit,
            uint8_t Namespace = 0x01,
            uint16_t Description = 0x0000
        >
        using PresentationFormat = PresentationFormatDescriptor<
            static_cast<uint8_t>(Format),
            Exponent,
            static_cast<uint16_t>(Unit),
            Namespace,
            Description
        >;

        template<typename... PresentationFormatDescriptors>
        using AggregateFormat = AggregateFormatDescriptor<PresentationFormatDescriptors...>;
    };

    struct chars {
        template<const char* MdlNumber>
        using ModelNumber = ConstCharacteristic<const char*, static_cast<uint16_t>(0x2A24), MdlNumber>;

        template<const char* SerNumber>
        using SerialNumber = ConstCharacteristic<const char*, static_cast<uint16_t>(0x2A25), SerNumber>;

        template<const char* FrmRevision>
        using FirmwareRevision = ConstCharacteristic<const char*, static_cast<uint16_t>(0x2A26), FrmRevision>;

        template<const char* HwRevision>
        using HardwareRevision = ConstCharacteristic<const char*, static_cast<uint16_t>(0x2A27), HwRevision>;

        template<const char* SftRevision>
        using SoftwareRevision = ConstCharacteristic<const char*, static_cast<uint16_t>(0x2A28), SftRevision>;

        template<const char* MfgName>
        using ManufacturerName = ConstCharacteristic<const char*, static_cast<uint16_t>(0x2A29), MfgName>;
    };
}; // class blex

// Convenience alias for default policies (auto-detected based on platform)
using blexDefault = blex<>;

#endif //BLEX_HPP_