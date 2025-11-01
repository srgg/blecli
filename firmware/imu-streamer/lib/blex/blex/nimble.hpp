/**
 * BLEX NimBLE Integration - Runtime BLE server and NimBLE binding
 */

#ifndef BLEX_NIMBLE_HPP_
#define BLEX_NIMBLE_HPP_

#include "platform.hpp"
#include "core.hpp"

// Detect if NimBLE is available
#ifdef NIMBLE_CPP_DEVICE_H_
    #define BLEX_NIMBLE_AVAILABLE
    #include <NimBLE2904.h>
    #include <NimBLE2905.h>
    // For ble_svc_gap_device_appearance_set()
    #if defined(CONFIG_NIMBLE_CPP_IDF)
        #include "host/ble_svc_gap.h"
    #else
        #include "nimble/nimble/host/services/gap/include/services/gap/ble_svc_gap.h"
    #endif
#endif

// Forward declaration
template<template<typename> class LockPolicy>
struct blex;

namespace blex_nimble {

#ifdef BLEX_NIMBLE_AVAILABLE

// Helper to create NimBLEUUID
template<auto uuid>
static NimBLEUUID make_uuid() {
    using UUIDType = decltype(uuid);
    using U = std::remove_cv_t<std::remove_reference_t<UUIDType>>;
    if constexpr (std::is_integral_v<U>) {
        static_assert(sizeof(U) <= sizeof(uint16_t) ||
                     (std::is_signed_v<U> ? uuid >= 0 && uuid <= 0xFFFF : uuid <= 0xFFFF),
                     "UUID integer value must fit in uint16_t (0-65535)");
        return NimBLEUUID(static_cast<uint16_t>(uuid));
    } else {
        return NimBLEUUID(uuid);
    }
}

// Add service UUIDs to advertising
template<typename... Services>
static void add_service_uuids_impl(NimBLEAdvertisementData& advData, std::tuple<Services...>) {
    (advData.addServiceUUID(make_uuid<blex_core::unwrap_service_impl<Services>::type::uuid>()), ...);
}

// NimBLE callback adapter with READ+NOTIFY optimization
//
// Threading Model:
//   NimBLE callbacks typically execute from a single BLE task.
//   Locking is unnecessary unless user code accesses shared state from other tasks.
//   Internal state (notified_value_valid, subscriber_count, pChar) uses lock-free atomics.
//
// READ+NOTIFY Optimization:
//   For characteristics with both READ and NOTIFY permissions, onRead() returns
//   the last notified value instead of calling ReadHandler, avoiding redundant sampling.
//
//   Requirements for correctness:
//     1. notify() must be called continuously at high rate (e.g., 100Hz sensor streaming)
//     2. notify() is the ONLY data update path (ReadHandler updates are bypassed)
//     3. Data staleness within a notification period is acceptable
//
//   Consistency guarantee:
//     "The last notified value IS the latest" - reads return the most recent notify() data.
//     This maintains consistency: all clients see the same value at any given time.
//
//   Performance benefit:
//     Eliminates expensive ReadHandler calls (e.g., I²C sensor reads @ 50-200µs) when
//     characteristic value is already current from the recent notification.
//
//   Freshness tracking:
//     notified_value_valid flag tracks whether the characteristic has been updated via notify().
//     Flag is cleared only when the LAST subscriber unsubscribes (subscriber_count == 0).
//     This prevents cache invalidation when multiple clients are subscribed.
//
template<typename CharT, template<typename> class LockPolicy>
struct BLECharShim final : NimBLECharacteristicCallbacks {
    // Determine if we should use read+notify optimization
    static constexpr bool use_read_notify_optimization =
        CharT::perms_type::canNotify && CharT::perms_type::canRead;

    // Empty container when optimization is disabled
    struct NoReadNotifyOptimization {};

    // Container for when optimization is enabled
    struct WithReadNotifyOptimization {
        // Lock-free subscription tracking: atomically incremented/decremented on subscribe/unsubscribe
        // Independent of NimBLE's internal state to avoid race conditions between notify() and onSubscribe()
        // Signed type (int8_t sufficient for NIMBLE_MAX_CONNECTIONS=9) simplifies corner case detection
        std::atomic<int8_t> subscriber_count{0};

        // Validity flag: prevents READ from returning stale data between subscribe and first notify()
        std::atomic<bool> notified_value_valid{false};
    };

     inline static std::conditional_t<
        use_read_notify_optimization,
        WithReadNotifyOptimization,
        NoReadNotifyOptimization
    > read_notify_optimization;

    static void setValue(const typename CharT::value_type& newValue) {
        using T = typename CharT::value_type;
        {// Fine-grained lock: only for setValue + notify
            typename blex_sync::template ScopedLock<CharT, LockPolicy> lock;  // Per-characteristic locking
            if (auto* p = CharT::pChar.get()) {
                if constexpr (std::is_same_v<T, std::string>) {
                    p->setValue(newValue);
                } else {
                    std::array<uint8_t, sizeof(T)> buf{};
                    std::memcpy(buf.data(), &newValue, sizeof(T));
                    p->setValue(buf.data(), sizeof(T));
                }

                if constexpr (CharT::perms_type::canNotify) {
                    p->notify();
                }
            }
        }

        if constexpr (use_read_notify_optimization) {
            // Lock-free atomic update (runs in parallel with onRead/onSubscribe)
            read_notify_optimization.notified_value_valid.store(true, std::memory_order_release);
        }
    }

    void onRead(NimBLECharacteristic* pChar, NimBLEConnInfo& connInfo) override {
        if constexpr (CharT::ReadHandler != nullptr) {
            // READ+NOTIFY optimization: lock-free fast path
            if constexpr (use_read_notify_optimization) {
                if (read_notify_optimization.notified_value_valid.load(std::memory_order_acquire)) {
                    // Characteristic value is fresh from a recent notification
                    // Return cached value without invoking OnRead callback
                    return;
                }
            }

            // ReadHandler is NOT thread-safe; the user-provided handler must be safe for concurrent execution.
            typename CharT::value_type tmp;
            CharT::ReadHandler(tmp);
            setValue(tmp);
        }
    }

    void onWrite(NimBLECharacteristic* pChar, NimBLEConnInfo& connInfo) override {
        // WriteHandler is NOT thread-safe; the user-provided handler must be safe for concurrent execution.
        if constexpr (CharT::WriteHandler != nullptr) {
            if constexpr (std::is_same_v<typename CharT::value_type, std::string>) {
                CharT::WriteHandler(pChar->getValue());
            } else {
                const auto& data = pChar->getValue();
                assert(data.size() >= sizeof(typename CharT::value_type) &&
                       "BLE write data size mismatch");
                // Using memcpy to avoid undefined behavior from pointer aliasing/misalignment
                typename CharT::value_type val;
                std::memcpy(&val, data.data(), sizeof(typename CharT::value_type));
                CharT::WriteHandler(val);
            }
        }
    }

    void onStatus(NimBLECharacteristic* pChar, int code) override {
        // StatusHandler is NOT thread-safe; the user-provided handler must be safe for concurrent execution.
        if constexpr (CharT::StatusHandler != nullptr) {
            CharT::StatusHandler(code);
        }
    }

    void onSubscribe(NimBLECharacteristic* pChar, NimBLEConnInfo& connInfo, uint16_t subValue) override {
        // Track the subscription count and update READ+NOTIFY optimization flags
        if constexpr (use_read_notify_optimization) {
            if (subValue == 0) {
                // Client unsubscribed - decrement counter
                const int8_t prev = read_notify_optimization.subscriber_count.fetch_sub(1, std::memory_order_acq_rel);
                assert(prev > 0 && "BUG: subscriber_count went negative (unsubscribe without subscribe)");

                // Only clear the freshness flag when the last subscriber leaves
                // This prevents cache invalidation while other clients remain subscribed
                if (prev == 1) {  // prev is a value BEFORE decrement, so 1 means now 0
                    read_notify_optimization.notified_value_valid.store(false, std::memory_order_release);
                }
            } else {
                // Client subscribed (notifications or indications enabled)
                read_notify_optimization.subscriber_count.fetch_add(1, std::memory_order_acq_rel);
            }
        }

        // SubscribeHandler is NOT thread-safe; the user-provided handler must be safe for concurrent execution.
        if constexpr (CharT::SubscribeHandler != nullptr) {
            CharT::SubscribeHandler(subValue);
        }
    }
};

#endif // BLEX_NIMBLE_AVAILABLE

} // namespace blex_nimble

// ---------------------- Descriptor Templates (with NimBLE registration) ----------------------

// Helper variable template
template<typename T, T Val = T{}>
static constexpr size_t value_storage_size_v = blex_core::value_storage_size<T, Val>::value;

// ConstDescriptor
template<typename T, auto UUID, T Value, typename Perms = Permissions<Readable>, size_t MaxSize = value_storage_size_v<T, Value>>
struct ConstDescriptor {
    static constexpr auto _ = (blex_core::check_uuid_type<decltype(UUID)>(), 0);

    using value_type = T;
    static constexpr auto uuid = UUID;
    static constexpr T value = Value;
    using perms_type = Perms;

#ifdef BLEX_NIMBLE_AVAILABLE
    static NimBLEDescriptor* register_descriptor(NimBLECharacteristic* pChar) {
        NimBLEDescriptor* desc = pChar->createDescriptor(
            blex_nimble::template make_uuid<UUID>(),
            (perms_type::canRead ? NIMBLE_PROPERTY::READ : 0) |
            (perms_type::canWrite ? NIMBLE_PROPERTY::WRITE : 0),
            MaxSize);
        if constexpr (perms_type::canRead) {
            if constexpr (std::is_same_v<T, const char*> || std::is_same_v<T, std::string>)
                desc->setValue(value);
            else
                desc->setValue(reinterpret_cast<const uint8_t*>(&value), sizeof(T));
        }
        return desc;
    }
#endif
};

// Descriptor for PresentationFormatValue (0x2904)
template<uint8_t Format, int8_t Exponent, uint16_t Unit, uint8_t Namespace, uint16_t Description>
struct PresentationFormatDescriptor {
    using value_type = PresentationFormatValue;
    static constexpr auto uuid = static_cast<uint16_t>(0x2904);
    static constexpr PresentationFormatValue value{Format, Exponent, Unit, Namespace, Description};
    using perms_type = Permissions<Readable>;
    using is_presentation_format_descriptor = void;  // Marker for trait detection

#ifdef BLEX_NIMBLE_AVAILABLE
    static NimBLEDescriptor* register_descriptor(NimBLECharacteristic* pChar) {
        auto* desc = pChar->create2904();
        if (desc) {
            desc->setFormat(Format);
            desc->setExponent(Exponent);
            desc->setUnit(Unit);
            desc->setNamespace(Namespace);
            desc->setDescription(Description);
        }
        return desc;
    }
#endif
};

// Aggregate Format Descriptor (0x2905)
template<typename... PresentationFormatDescriptors>
struct AggregateFormatDescriptor {
    static constexpr auto uuid = static_cast<uint16_t>(0x2905);
    using perms_type = Permissions<Readable>;
    static constexpr size_t num_descriptors = sizeof...(PresentationFormatDescriptors);

    static_assert(num_descriptors > 0, "AggregateFormat requires at least one PresentationFormat descriptor");
    static_assert((... && blex_core::has_presentation_format_marker<PresentationFormatDescriptors>()),
                  "AggregateFormat only accepts PresentationFormatDescriptor types");

#ifdef BLEX_NIMBLE_AVAILABLE
    static NimBLEDescriptor* register_descriptor(NimBLECharacteristic* pChar) {
        NimBLE2905* p2905 = pChar->create2905();

        (add_presentation_descriptor<PresentationFormatDescriptors>(pChar, p2905), ...);

        return p2905;
    }

private:
    template<typename PresentationDesc>
    static void add_presentation_descriptor(NimBLECharacteristic* pChar, NimBLE2905* p2905) {
        auto* desc = PresentationDesc::register_descriptor(pChar);
        if (desc) {
            p2905->add2904Descriptor(static_cast<NimBLE2904*>(desc));
        }
    }
#endif
};

// Descriptor (dynamic values)
template<typename T, const char* UUIDLiteral, typename Perms = Permissions<Readable>, size_t MaxSize = sizeof(T)>
struct Descriptor {
    static constexpr auto _ = (blex_core::check_uuid_type<decltype(UUIDLiteral)>(), 0);

    using value_type = T;
    static constexpr const char* uuid = UUIDLiteral;
    using perms_type = Perms;

#ifdef BLEX_NIMBLE_AVAILABLE
    static NimBLEDescriptor* register_descriptor(NimBLECharacteristic* pChar) {
        NimBLEDescriptor* desc = pChar->createDescriptor(
            blex_nimble::template make_uuid<UUIDLiteral>(),
            (perms_type::canRead ? NIMBLE_PROPERTY::READ : 0) |
            (perms_type::canWrite ? NIMBLE_PROPERTY::WRITE : 0),
            MaxSize);
        return desc;
    }
#endif
};

// ---------------------- Characteristic Templates (with NimBLE registration) ----------------------

// ConstCharacteristic (read-only with compile-time value)
template<typename T, auto UUID, T Value, typename... Descriptors>
struct ConstCharacteristic {
    static constexpr auto _ = (blex_core::check_uuid_type<decltype(UUID)>(), 0);

    using value_type = T;
    static constexpr auto uuid = UUID;
    static constexpr T value = Value;
    using perms_type = Permissions<Readable>;
    using descriptors_pack = blex_core::DescriptorsPack<Descriptors...>;
    static constexpr bool is_const_characteristic = true;

    static constexpr void validate_all_descriptors() {}
};

// Characteristic (dynamic with callbacks)
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
struct Characteristic {
    static constexpr auto _ = (blex_core::check_uuid_type<decltype(UUID)>(), 0);

    static_assert( OnRead == nullptr || blex_core::CallbackTraits<T, OnRead>::is_valid_on_read && Perms::canRead,
        "Invalid BLE OnRead callback: must be invocable with 'T&' and characteristic must allow Read");

    static_assert( OnWrite == nullptr || blex_core::CallbackTraits<T, OnWrite>::is_valid_on_write && Perms::canWrite,
        "Invalid BLE OnWrite callback: must be invocable with 'const T&' and characteristic must allow Write");

    static_assert( OnSubscribe== nullptr || blex_core::CallbackTraits<T, OnSubscribe>::is_valid_on_subscribe && Perms::canNotify,
        "Invalid BLE OnSubscribe callback: must be invocable with 'uint16_t' and characteristic must allow Notifications");

    using value_type = T;
    static constexpr auto uuid = UUID;
    using perms_type = Perms;
    using descriptors_pack = blex_core::DescriptorsPack<Descriptors...>;
    static constexpr bool is_const_characteristic = false;
    static constexpr auto ReadHandler = OnRead;
    static constexpr auto WriteHandler = OnWrite;
    static constexpr auto StatusHandler = OnStatus;
    static constexpr auto SubscribeHandler = OnSubscribe;

#ifdef BLEX_NIMBLE_AVAILABLE
    // Make pChar accessible via template parameter LockPolicy
    template<template<typename> class LockPolicy>
    struct WithLockPolicy {
        inline static typename blex_sync::template SafePtr<NimBLECharacteristic, Characteristic, LockPolicy, true> pChar;
    };

    // Storage for pChar pointer (using DefaultLock by default)
    inline static typename blex_sync::template SafePtr<NimBLECharacteristic, Characteristic, DefaultLock, true> pChar;

    // Allow BLECharShim to access private members
    template<typename, template<typename> class>
    friend struct blex_nimble::BLECharShim;

    // Allow register_characteristic to access private members
    template<template<typename> class LockPolicy>
    friend struct blex;
#endif

    template<template<typename> class LockPolicy = DefaultLock>
    static void setValue(const T& newValue) {
         return blex_nimble::BLECharShim<Characteristic, LockPolicy>::setValue(newValue);
    }

    static constexpr void validate_all_descriptors() {}
};

// ---------------------- Service Template ----------------------

template<auto UUID, typename... Chars>
struct Service {
    static constexpr auto _ = (blex_core::check_uuid_type<decltype(UUID)>(), 0);

    static constexpr auto uuid = UUID;
    using chars_pack = blex_core::CharsPack<Chars...>;

    static constexpr void validate() {
        (Chars::validate_all_descriptors(), ...);
    }

    // Per-service locking: each Service instantiation gets its own lock
    template<template<typename> class LockPolicy = DefaultLock>
    static inline blex_sync::SafeFuncPtr<void(*)(), Service, LockPolicy> onAdvertiseStart;
};

#endif // BLEX_NIMBLE_HPP_