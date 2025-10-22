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

// Detect if NimBLE is available
#ifdef NIMBLE_CPP_DEVICE_H_
    #define BLEX_NIMBLE_AVAILABLE
#endif

#include <tuple>
#include <type_traits>
#include <string>
#include <cstring>
#include <cassert>
#include <atomic>
#include <mutex>

// ---------------------- Lock Policy Implementations ----------------------

// No-op lock (zero overhead for single-core)
template<typename /*Tag*/ = void>
struct NoLock {
    void lock() const {}
    void unlock() const {}
};

// ---------------------- Platform Detection ----------------------

// Detect FreeRTOS availability across platforms
#if defined(ESP_PLATFORM) || defined(ARDUINO_ARCH_RP2040) || defined(STM32H7xx) || \
    defined(IDF_VER) || (defined(__has_include) && __has_include(<FreeRTOS.h>))
    #define BLEX_HAS_FREERTOS
#endif

// Detect multi-core platforms
#if defined(CONFIG_FREERTOS_UNICORE)
    // ESP32 explicitly configured as single-core
    #define BLEX_SINGLE_CORE
#elif defined(ESP32) || defined(ESP32S3) || defined(ESP32C3) || defined(ESP_PLATFORM)
    // ESP32 family (assume dual-core unless UNICORE defined)
    #define BLEX_MULTI_CORE
#elif defined(ARDUINO_ARCH_RP2040) || defined(PICO_RP2040)
    // Raspberry Pi Pico (dual Cortex-M0+)
    #define BLEX_MULTI_CORE
#elif defined(STM32H745xx) || defined(STM32H747xx) || defined(STM32H755xx) || defined(STM32H757xx)
    // STM32H7 dual-core variants
    #define BLEX_MULTI_CORE
#endif

// FreeRTOS mutex implementation
#if defined(BLEX_HAS_FREERTOS) && !defined(BLEX_NO_FREERTOS)
    #if defined(ESP_PLATFORM)
        #include <freertos/FreeRTOS.h>
        #include <freertos/semphr.h>
    #else
        #include <FreeRTOS.h>
        #include <semphr.h>
    #endif

    /**
     * @brief FreeRTOS recursive mutex with ISR safety enforcement
     * @warning MUST NOT be used from an ISR context. Use only from FreeRTOS tasks.
     *          Violations will trigger assertion failure.
     */
    template<typename Tag = void>
    struct FreeRTOSLock {
        using tag = Tag;

        FreeRTOSLock() = default;
        // No destructor - never delete the static global mutex (shutdown ordering issues)

        void lock() const {
            auto m = get_mutex();
            assert(m != nullptr && "FreeRTOS mutex not initialized");
            xSemaphoreTakeRecursive(m, portMAX_DELAY);
        }

        void unlock() const {
            auto m = get_mutex();
            assert(m != nullptr && "FreeRTOS mutex not initialized");
            xSemaphoreGiveRecursive(m);
        }
    private:
        // Each Tag has its own static storage buffer and handle; initialized lazily in a thread-safe manner.
        static SemaphoreHandle_t& get_mutex() {
            #if defined(ESP_PLATFORM)
                configASSERT(!xPortInIsrContext() &&
                             "FreeRTOSLock: MUST NOT be called from ISR context!");
            #endif

            // local statics are initialized once in a thread-safe manner per C++11.
            static StaticSemaphore_t static_buf;
            static SemaphoreHandle_t mutex = []() -> SemaphoreHandle_t {
                // Create static recursive mutex using user-supplied buffer (no heap).
                auto h = xSemaphoreCreateRecursiveMutexStatic(&static_buf);
                assert(h && "FreeRTOS recursive mutex creation failed");
                return h;
            }();
            return mutex;
        }
    };
#endif

// ---------------------- Default Lock Policy Selection ----------------------

#if defined(BLEX_HAS_FREERTOS) && !defined(BLEX_NO_FREERTOS)
    // FreeRTOS available: use FreeRTOSLock for all platforms
    template<typename Tag = void>
    using DefaultLock = FreeRTOSLock<Tag>;

    #if defined(BLEX_MULTI_CORE)
        #pragma message("BLEX: Multi-core platform detected, using FreeRTOSLock for thread safety")
    #endif

#elif defined(BLEX_MULTI_CORE)
    // Multi-core platform WITHOUT FreeRTOS: ERROR
    #error "BLEX: Multi-core platform detected but FreeRTOS not available. " \
           "Either: (1) Enable FreeRTOS, or (2) Explicitly specify lock policy: blex<YourLockPolicy>"

#else
    // Single-core or unknown platform: use NoLock with a warning
    template<typename Tag = void>
    using DefaultLock = NoLock<Tag>;

    #pragma message("BLEX: No threading support detected, using NoLock (no thread safety). " \
                    "If your platform is multi-core, explicitly specify: blex<FreeRTOSLock>")
#endif

// ---------------------- BLEX Template Class ----------------------

template<
    template<typename> class LockPolicy = DefaultLock
>
struct blex {
    template<int8_t TxPower = 0, uint16_t IntervalMin = 100, uint16_t IntervalMax = 150>
    struct AdvertisingConfig;

    template<
        uint16_t MTU = 247,
        uint16_t ConnIntervalMin = 12,
        uint16_t ConnIntervalMax = 12,
        uint16_t ConnLatency = 0,
        uint16_t SupervisionTimeout = 400
    >
    struct ConnectionConfig;

private:
    // ---------------------- Private Implementation Details ----------------------
    struct detail {
        // Synchronization primitives
        struct sync {
            // RAII lock guard (used by SafeFuncPtr)
            template<typename Lock>
            class LockGuard {
                Lock& lock;
            public:
                explicit LockGuard(Lock& l) : lock(l) { lock.lock(); }
                ~LockGuard() { lock.unlock(); }
                LockGuard(const LockGuard&) = delete;
                LockGuard& operator=(const LockGuard&) = delete;
            };

            /**
             * @brief thread-aware pointer wrapper: Immutable = true: lock-free, pointer set once,
             *  Immutable = false: locked access using per-Tag mutex
             */
            template <
                typename T,
                typename Tag,
                bool Immutable = false
            >
            class SafePtr {
                std::atomic<T*> ptr_{nullptr};

            public:
                // Sets the pointer; allowed only once if Immutable = true.
                // For immutable (write-once) objects, atomics suffice.
                // for mutable, all access goes through ScopedLock<Tag>.
                void set(T* p) {
                    if constexpr (Immutable) {
                        T* expected = nullptr;
                        const bool ok = ptr_.compare_exchange_strong(
                            expected, p, std::memory_order_release, std::memory_order_relaxed);
                        configASSERT(ok && "SafePtr: immutable pointer set twice");
                    } else {
                        ScopedLock<Tag> lock;
                        ptr_.store(p, std::memory_order_release);
                    }
                }

                // ------------------------------------------------------------------------
                // call(): safe invocation under lock
                // Executes lambda only if pointer is valid, returns default-constructed value if null.
                // ------------------------------------------------------------------------
                template <typename F>
                auto call(F&& fn) -> std::invoke_result_t<F, T&> {
                    if constexpr (Immutable) {
                        T* p = ptr_.load(std::memory_order_acquire);
                        if (p) return fn(*p);
                        return {};  // Return default-constructed value if pointer is null
                    } else {
                        ScopedLock<Tag> lock;
                        T* p = ptr_.load(std::memory_order_acquire);
                        if (p) return fn(*p);
                        return {};  // Return default-constructed value if pointer is null
                    }
                }

                // Gives a raw pointer to the lambda under lock.
                // The Caller decides what to do with it.  Use for conditional access.
                template <typename F>
                auto with_lock(F&& fn) {
                    if constexpr (Immutable) {
                        return fn(ptr_.load(std::memory_order_acquire));
                    } else {
                        ScopedLock<Tag> lock;
                        return fn(ptr_.load(std::memory_order_acquire));
                    }
                }

                // ------------------------------------------------------------------------
                // fast read-only access (unsafe if mutable).
                // Intended only for Immutable=true pointers (static drivers, etc.)
                T* get() const noexcept {
                    if constexpr (Immutable)
                        return ptr_.load(std::memory_order_acquire);
                    else
                        return nullptr; // force compile-time awareness of unsafe call
                }

                // Convenience operator for immutable reads
                T* operator->() const noexcept { return get(); }
                explicit operator bool() const noexcept { return get() != nullptr; }
            };
            // Thread-safe function pointer wrapper using LockPolicy
            template<typename FuncPtr, typename Tag = void>
            class SafeFuncPtr {
                FuncPtr ptr = nullptr;
                mutable LockPolicy<Tag> lock_policy;  // Per-Tag locking for fine-grained concurrency

            public:
                SafeFuncPtr() = default;

                void set(FuncPtr p) {
                    LockGuard<LockPolicy<Tag>> guard(lock_policy);
                    ptr = p;
                }

                SafeFuncPtr& operator=(FuncPtr p) {
                    set(p);
                    return *this;
                }

                FuncPtr get() const {
                    LockGuard<LockPolicy<Tag>> guard(lock_policy);
                    return ptr;
                }

                template<typename... Args>
                auto call(Args&&... args) const {
                    using R = std::invoke_result_t<FuncPtr, Args...>;
                    LockGuard<LockPolicy<Tag>> guard(lock_policy);
                    if (!ptr) {
                        if constexpr (std::is_void_v<R>) {
                            return;
                        } else {
                            return std::optional<R>{}; // empty
                        }
                    }
                    if constexpr (std::is_void_v<R>) {
                        ptr(std::forward<Args>(args)...);
                    } else {
                        return std::optional<R>{ ptr(std::forward<Args>(args)...) };
                    }
                }

                // for convenience: operator() forwards to call()
                template<typename... Args>
                auto operator()(Args&&... args) const {
                    return call(std::forward<Args>(args)...);
                }

                explicit operator bool() const { return get() != nullptr; }

                template<typename Func>
                auto with_lock(Func&& f) const {
                    LockGuard<LockPolicy<Tag>> guard(lock_policy);
                    return f(ptr);
                }
            };

            // RAII lock wrapper; uses the same per-Tag static lock
            template<typename Tag>
            struct ScopedLock {
                using Lock = LockPolicy<Tag>;
                Lock& getLock() const {
                    static Lock lock;
                    return lock;
                }

                ScopedLock()  { getLock().lock(); }
                ~ScopedLock() { getLock().unlock(); }

                ScopedLock(const ScopedLock&) = delete;
                ScopedLock& operator=(const ScopedLock&) = delete;
            };
        }; // struct sync

        // UUID type validation
        template<typename T>
        static constexpr void check_uuid_type() {
            using U = std::remove_cv_t<std::remove_reference_t<T>>;
            using P = std::remove_cv_t<std::remove_pointer_t<U>>;
            using E = std::remove_cv_t<std::remove_extent_t<U>>;

            static_assert(
                std::is_integral_v<U> ||
                std::is_same_v<P, char> ||
                std::is_same_v<E, char>,
                "UUID must be an integer (e.g., uint16_t) or a C string (char*/char[])"
            );
        }

        // Constexpr string length helper
        static constexpr size_t const_strlen(const char* str) {
            return *str ? 1 + const_strlen(str + 1) : 0;
        }

        // Value storage size calculation
        template<typename T, T = T{}>
        struct value_storage_size {
            static constexpr size_t value = sizeof(T);
        };

        template<const char* Val>
        struct value_storage_size<const char*, Val> {
            static constexpr size_t value = const_strlen(Val) + 1;
        };

        // Service wrapper detection
        template<typename, typename = void>
        struct is_service_wrapper : std::false_type {};

        template<typename T>
        struct is_service_wrapper<T, std::void_t<typename T::service_type>> : std::true_type {};

        // Unwrap service type
        template<typename T, typename = void>
        struct unwrap_service_impl {
            using type = T;
        };

        template<typename T>
        struct unwrap_service_impl<T, std::enable_if_t<is_service_wrapper<T>::value>> {
            using type = typename T::service_type;
        };

        template<typename T>
        using unwrap_service = typename unwrap_service_impl<T>::type;

        // Pack wrappers to hold types as template parameter packs (not tuples)
        template<typename... /*Services*/>
        struct ServicesPack {};

        template<typename... /*Chars*/>
        struct CharsPack {};

        template<typename... /*Descs*/>
        struct DescriptorsPack {};

        // Service filtering (returns a tuple for compatibility with BlexAdvertising)
        template<template<typename> class Predicate, typename... Services>
        struct filter_services;

        template<template<typename> class Predicate>
        struct filter_services<Predicate> {
            using type = std::tuple<>;
        };

        template<template<typename> class Predicate, typename First, typename... Rest>
        struct filter_services<Predicate, First, Rest...> {
            using filtered_rest = typename filter_services<Predicate, Rest...>::type;
            using type = std::conditional_t<
                Predicate<First>::value,
                decltype(std::tuple_cat(std::tuple<First>{}, filtered_rest{})),
                filtered_rest
            >;
        };

        // Apply filter_services to ServicesPack
        template<template<typename> class Predicate, typename SvcPack>
        struct filter_services_pack;

        template<template<typename> class Predicate, typename... Services>
        struct filter_services_pack<Predicate, ServicesPack<Services...>> {
            using type = typename filter_services<Predicate, Services...>::type;
        };

        // Predicates for filtering
        template<typename, typename = void>
        struct is_passive_adv_pred : std::false_type {};

        template<typename T>
        struct is_passive_adv_pred<T, std::enable_if_t<is_service_wrapper<T>::value>>
            : std::integral_constant<bool, T::passive_adv> {};

        template<typename, typename = void>
        struct is_active_adv_pred : std::false_type {};

        template<typename T>
        struct is_active_adv_pred<T, std::enable_if_t<is_service_wrapper<T>::value>>
            : std::integral_constant<bool, T::active_adv> {};

        // Config type detection (forward-compatible)
        template<typename, typename = void>
        struct is_advertising_config : std::false_type {};

        // Specialize for any type that has is_blex_advertising_config_tag
        template<typename T>
        struct is_advertising_config<T, std::void_t<typename T::is_blex_advertising_config_tag>> : std::true_type {};

        template<typename, typename = void>
        struct is_connection_config : std::false_type {};

        // Specialize for any type that has is_blex_connection_config_tag
        template<typename T>
        struct is_connection_config<T, std::void_t<typename T::is_blex_connection_config_tag>> : std::true_type {};

        // Extract AdvertisingConfig from variadic args, or use default
        template<typename... /*Args*/>
        struct extract_adv_config {
            using type = AdvertisingConfig<-127, 0, 0>; // Default: use NimBLE defaults (sentinels)
};

        template<typename First, typename... Rest>
        struct extract_adv_config<First, Rest...> {
            using type = std::conditional_t<
                is_advertising_config<First>::value,
                First,
                typename extract_adv_config<Rest...>::type
            >;
        };

        // Extract ConnectionConfig from variadic args, or use default
        template<typename...>
        struct extract_conn_config {
            // Default: use NimBLE defaults (sentinels)
            using type = ConnectionConfig<0, 0, 0, 0, 0>;
        };

        template<typename First, typename... Rest>
        struct extract_conn_config<First, Rest...> {
            using type = std::conditional_t<
                is_connection_config<First>::value,
                First,
                typename extract_conn_config<Rest...>::type
            >;
        };

        // Helper to concatenate ServicesPack types
        template<typename Pack1, typename Pack2>
        struct concat_pack;

        template<typename... S1, typename... S2>
        struct concat_pack<ServicesPack<S1...>, ServicesPack<S2...>> {
            using type = ServicesPack<S1..., S2...>;
        };

        // Filter out AdvertisingConfig and ConnectionConfig to get only Services
        template<typename...>
        struct filter_non_config {
            using type = ServicesPack<>;  // Base case: empty pack
        };

        template<typename First, typename... Rest>
        struct filter_non_config<First, Rest...> {
            using filtered_rest = typename filter_non_config<Rest...>::type;
            using type = std::conditional_t<
                is_advertising_config<First>::value || is_connection_config<First>::value,
                filtered_rest,
                typename concat_pack<ServicesPack<First>, filtered_rest>::type
            >;
        };

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
            (advData.addServiceUUID(make_uuid<unwrap_service_impl<Services>::type::uuid>()), ...);
        }
#endif

        // Callback validation
        template<typename T, auto CallbackFunc>
        struct CallbackTraits {
            static constexpr bool is_valid_on_read = std::is_invocable_v<decltype(CallbackFunc), T&>;
            static constexpr bool is_valid_on_write = std::is_invocable_v<decltype(CallbackFunc), const T&>;
            static constexpr bool is_valid_on_status = std::is_invocable_v<decltype(CallbackFunc), int>;
            static constexpr bool is_valid_on_subscribe = std::is_invocable_v<decltype(CallbackFunc), uint16_t>;
        };

#ifdef BLEX_NIMBLE_AVAILABLE
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
        template<typename CharT>
        struct BLECharShim : NimBLECharacteristicCallbacks {
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
                    typename sync::template ScopedLock<CharT> lock;  // Per-characteristic locking
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
                        const int8_t prev = read_notify_optimization.subscriber_count.fetch_add(1, std::memory_order_acq_rel);
                        assert(prev >= 0 && prev < NIMBLE_MAX_CONNECTIONS && "BUG: subscriber_count exceeded NIMBLE_MAX_CONNECTIONS");
                    }
                }

                // SubscribeHandler is NOT thread-safe; the user-provided handler must be safe for concurrent execution.
                if constexpr (CharT::SubscribeHandler != nullptr) {
                    CharT::SubscribeHandler(subValue);
                }
            }
        };
#endif
    }; // struct detail

public:
    // ---------------------- Public API ----------------------

    // Helper variable template
    template<typename T, T Val = T{}>
    static constexpr size_t value_storage_size_v = detail::template value_storage_size<T, Val>::value;

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
    static constexpr bool is_passive_adv_v = detail::template is_passive_adv_pred<T>::value;

    template<typename T>
    static constexpr bool is_active_adv_v = detail::template is_active_adv_pred<T>::value;

    // Permission types
    struct Readable   { static constexpr bool canRead = true;  static constexpr bool canWrite = false; static constexpr bool canNotify = false; };
    struct Writable   { static constexpr bool canRead = false; static constexpr bool canWrite = true;  static constexpr bool canNotify = false; };
    struct Notifiable { static constexpr bool canRead = false; static constexpr bool canWrite = false; static constexpr bool canNotify = true; };

    template<typename... Perms>
    struct Permissions {
        static_assert(
            (... && (std::is_same_v<Perms, Readable> ||
                     std::is_same_v<Perms, Writable> ||
                     std::is_same_v<Perms, Notifiable>)),
            "Permissions only accepts Readable, Writable, or Notifiable permission types"
        );

        static constexpr bool canRead   = (... || Perms::canRead);
        static constexpr bool canWrite  = (... || Perms::canWrite);
        static constexpr bool canNotify = (... || Perms::canNotify);
    };

    // ConstDescriptor
    template<typename T, auto UUID, T Value, typename Perms = Permissions<Readable>, size_t MaxSize = value_storage_size_v<T, Value>>
    struct ConstDescriptor {
        static constexpr auto _ = (detail::template check_uuid_type<decltype(UUID)>(), 0);

        using value_type = T;
        static constexpr auto uuid = UUID;
        static constexpr T value = Value;
        using perms_type = Perms;

#ifdef BLEX_NIMBLE_AVAILABLE
        static NimBLEDescriptor* register_descriptor(NimBLECharacteristic* pChar) {
            NimBLEDescriptor* desc = pChar->createDescriptor(
                detail::template make_uuid<UUID>(),
                // ReSharper disable once CppRedundantQualifier
                (perms_type::canRead ? NIMBLE_PROPERTY::READ : 0) |
                // ReSharper disable once CppRedundantQualifier
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

    // Descriptor (dynamic values)
    template<typename T, const char* UUIDLiteral, typename Perms = Permissions<Readable>, size_t MaxSize = sizeof(T)>
    struct Descriptor {
        static constexpr auto _ = (detail::template check_uuid_type<decltype(UUIDLiteral)>(), 0);

        using value_type = T;
        static constexpr const char* uuid = UUIDLiteral;
        using perms_type = Perms;

#ifdef BLEX_NIMBLE_AVAILABLE
        static NimBLEDescriptor* register_descriptor(NimBLECharacteristic* pChar) {
            NimBLEDescriptor* desc = pChar->createDescriptor(
                detail::template make_uuid<UUIDLiteral>(),
                // ReSharper disable once CppRedundantQualifier
                (perms_type::canRead ? NIMBLE_PROPERTY::READ : 0) |
                // ReSharper disable once CppRedundantQualifier
                (perms_type::canWrite ? NIMBLE_PROPERTY::WRITE : 0),
                MaxSize);
            return desc;
        }
#endif
    };

    // ConstCharacteristic (read-only with compile-time value)
    template<typename T, auto UUID, T Value, typename... Descriptors>
    struct ConstCharacteristic {
        static constexpr auto _ = (detail::template check_uuid_type<decltype(UUID)>(), 0);

        using value_type = T;
        static constexpr auto uuid = UUID;
        static constexpr T value = Value;
        using perms_type = Permissions<Readable>;
        using descriptors_pack = typename detail::template DescriptorsPack<Descriptors...>;
        static constexpr bool is_const_characteristic = true;

        static constexpr void validate_all_descriptors() {}
    };

    // Characteristic (dynamic with callbacks)
    // IMPORTANT: User callbacks (OnRead, OnWrite, OnSubscribe, OnStatus) are NOT automatically locked.
    // If your callback accesses a shared state that may be modified from multiple threads,
    // YOU must provide appropriate synchronization.
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
        static constexpr auto _ = (detail::template check_uuid_type<decltype(UUID)>(), 0);

        static_assert( OnRead == nullptr || detail::template CallbackTraits<T, OnRead>::is_valid_on_read && Perms::canRead,
            "Invalid BLE OnRead callback: must be invocable with 'T&' and characteristic must allow Read");

        static_assert( OnWrite == nullptr || detail::template CallbackTraits<T, OnWrite>::is_valid_on_write && Perms::canWrite,
            "Invalid BLE OnWrite callback: must be invocable with 'const T&' and characteristic must allow Write");

        static_assert( OnSubscribe== nullptr || detail::template CallbackTraits<T, OnSubscribe>::is_valid_on_subscribe && Perms::canNotify,
            "Invalid BLE OnSubscribe callback: must be invocable with 'uint16_t' and characteristic must allow Notifications");

        using value_type = T;
        static constexpr auto uuid = UUID;
        using perms_type = Perms;
        using descriptors_pack = typename detail::template DescriptorsPack<Descriptors...>;
        static constexpr bool is_const_characteristic = false;
        static constexpr auto ReadHandler = OnRead;
        static constexpr auto WriteHandler = OnWrite;
        static constexpr auto StatusHandler = OnStatus;
        static constexpr auto SubscribeHandler = OnSubscribe;

    private:
#ifdef BLEX_NIMBLE_AVAILABLE
        inline static typename detail::sync::template SafePtr<NimBLECharacteristic, Characteristic, true> pChar;

        // Allow BLECharShim and register_characteristic to access private members
        template<typename>
        friend struct detail::BLECharShim;

        template<typename CharT>
        friend NimBLECharacteristic* blex::register_characteristic(NimBLEService* svc);
#endif

    public:
        static void setValue(const T& newValue) {
             return detail::template BLECharShim<Characteristic>::setValue(newValue);
        }

//         static void setValue(const T& newValue) {
//             return detail::BLECharShim<Characteristic>::setValue(newValue);
//             // Fine-grained lock: only for setValue + notify
//             {
//                 typename detail::sync::template ScopedLock<Characteristic> lock;  // Per-characteristic locking
// #ifdef BLEX_NIMBLE_AVAILABLE
//                 if (auto* p = pChar.get()) {
//                     if constexpr (std::is_same_v<T, std::string>) {
//                         p->setValue(newValue);
//                     } else {
//                         std::array<uint8_t, sizeof(T)> buf{};
//                         std::memcpy(buf.data(), &newValue, sizeof(T));
//                         p->setValue(buf.data(), sizeof(T));
//                     }
//
//                     if constexpr (perms_type::canNotify) {
//                         p->notify();
//                     }
//                 }
// #endif
//             }
//
//             if constexpr (perms_type::canNotify) {
//                 // Lock-free atomic update (runs in parallel with onRead/onSubscribe)
//                 notified_value_valid.store(true, std::memory_order_release);
//             }
//         }

        static constexpr void validate_all_descriptors() {}
    };

    // Service
    template<auto UUID, typename... Chars>
    struct Service {
        static constexpr auto _ = (detail::template check_uuid_type<decltype(UUID)>(), 0);

        static constexpr auto uuid = UUID;
        using chars_pack = typename detail::template CharsPack<Chars...>;

        static constexpr void validate() {
            (Chars::validate_all_descriptors(), ...);
        }

        // Per-service locking: each Service instantiation gets its own lock
        static inline typename detail::sync::template SafeFuncPtr<void(*)(), Service> onAdvertiseStart;
    };

    // AdvertisingConfig: Compile-time advertising configuration with runtime tuning
    // Hides NimBLE implementation details and provides type-safe advertising setup
    // Template parameters: TxPower (dBm), IntervalMin (ms), IntervalMax (ms)
    template<
        int8_t TxPower,
        uint16_t IntervalMin,
        uint16_t IntervalMax
    >
    struct AdvertisingConfig {
        // Marker for trait detection
        using is_blex_advertising_config_tag = void;

        // Compile-time configuration (user-specified via template parameters)
        static constexpr int8_t default_tx_power = TxPower;
        static constexpr uint16_t default_adv_interval_min = IntervalMin;
        static constexpr uint16_t default_adv_interval_max = IntervalMax;
        static constexpr uint8_t default_flags = 0x06;  // LE General Discoverable + BR/EDR Not Supported

        // ESP32-S3 TX power range (validation)
        static constexpr int8_t min_tx_power = -12;  // dBm
        static constexpr int8_t max_tx_power = 9;    // dBm

        // BLE spec advertising interval range (validation)
        static constexpr uint16_t min_adv_interval = 20;      // ms
        static constexpr uint16_t max_adv_interval = 10240;   // ms

        // Compile-time validation
        static_assert(TxPower >= min_tx_power && TxPower <= max_tx_power,
                     "TX power must be in range [-12, 9] dBm");
        static_assert(IntervalMin >= min_adv_interval && IntervalMin <= max_adv_interval,
                     "Advertising interval min must be in range [20, 10240] ms");
        static_assert(IntervalMax >= min_adv_interval && IntervalMax <= max_adv_interval,
                     "Advertising interval max must be in range [20, 10240] ms");
        static_assert(IntervalMin <= IntervalMax,
                     "Advertising interval min must be <= max");
    };

    // ConnectionConfig: Compile-time connection configuration
    // Hides NimBLE implementation details and provides type-safe connection setup
    // Template parameters: MTU (bytes), ConnIntervalMin/Max (1.25ms units), ConnLatency, SupervisionTimeout (10ms units)
    template<
        uint16_t MTU,
        uint16_t ConnIntervalMin,
        uint16_t ConnIntervalMax,
        uint16_t ConnLatency,
        uint16_t SupervisionTimeout
    >
    struct ConnectionConfig {
        // Marker for trait detection
        using is_blex_connection_config_tag = void;

        // Compile-time configuration
        static constexpr uint16_t mtu = MTU;
        static constexpr uint16_t conn_interval_min = ConnIntervalMin;  // in 1.25ms units
        static constexpr uint16_t conn_interval_max = ConnIntervalMax;  // in 1.25ms units
        static constexpr uint16_t conn_latency = ConnLatency;
        static constexpr uint16_t supervision_timeout = SupervisionTimeout;  // in 10ms units

        // BLE spec connection parameter ranges
        static constexpr uint16_t min_mtu = 23;      // BLE minimum
        static constexpr uint16_t max_mtu = 517;     // BLE maximum
        static constexpr uint16_t min_conn_interval = 6;    // 7.5ms
        static constexpr uint16_t max_conn_interval = 3200; // 4000ms
        static constexpr uint16_t max_conn_latency = 499;
        static constexpr uint16_t min_supervision_timeout = 10;   // 100ms
        static constexpr uint16_t max_supervision_timeout = 3200; // 32000ms

        // Compile-time validation
        static_assert(MTU >= min_mtu && MTU <= max_mtu,
                     "MTU must be in range [23, 517] bytes");
        static_assert(ConnIntervalMin >= min_conn_interval && ConnIntervalMin <= max_conn_interval,
                     "Connection interval min must be in range [6, 3200] (1.25ms units)");
        static_assert(ConnIntervalMax >= min_conn_interval && ConnIntervalMax <= max_conn_interval,
                     "Connection interval max must be in range [6, 3200] (1.25ms units)");
        static_assert(ConnIntervalMin <= ConnIntervalMax,
                     "Connection interval min must be <= max");
        static_assert(ConnLatency <= max_conn_latency,
                     "Connection latency must be <= 499");
        static_assert(SupervisionTimeout >= min_supervision_timeout && SupervisionTimeout <= max_supervision_timeout,
                     "Supervision timeout must be in range [10, 3200] (10ms units)");
    };

    // BlexAdvertising: Applies advertising configuration to NimBLE
    template<typename PassiveServicesTuple, typename ActiveServicesTuple, typename AdvConfig = AdvertisingConfig<>>
    struct BlexAdvertising {
        // Use configuration from AdvConfig template parameter
        static constexpr int8_t default_tx_power = AdvConfig::default_tx_power;
        static constexpr uint16_t default_adv_interval_min = AdvConfig::default_adv_interval_min;
        static constexpr uint16_t default_adv_interval_max = AdvConfig::default_adv_interval_max;
        static constexpr uint8_t default_flags = AdvConfig::default_flags;

        static constexpr int8_t min_tx_power = AdvConfig::min_tx_power;
        static constexpr int8_t max_tx_power = AdvConfig::max_tx_power;
        static constexpr uint16_t min_adv_interval = AdvConfig::min_adv_interval;
        static constexpr uint16_t max_adv_interval = AdvConfig::max_adv_interval;

#ifdef BLEX_NIMBLE_AVAILABLE
        // Configure advertising structure with compile-time service UUIDs and optional runtime overrides
        // advertising: NimBLE advertising object
        // device_name: Full device name for scan response
        // short_name: Short name for advertisement data
        // tx_power_override: Optional TX power in dBm (use default if < min_tx_power)
        // interval_min_override: Optional min advertising interval in ms (use default if 0)
        // interval_max_override: Optional max advertising interval in ms (use default if 0)
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
            detail::add_service_uuids_impl(adv_data, PassiveServicesTuple{});
            advertising->setAdvertisementData(adv_data);

            // Configure scan response data (active services + full name)
            NimBLEAdvertisementData scan_resp;
            scan_resp.setName(device_name, true);
            detail::add_service_uuids_impl(scan_resp, ActiveServicesTuple{});
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
        using AdvConfig = typename detail::template extract_adv_config<Args...>::type;
        using ConnConfig = typename detail::template extract_conn_config<Args...>::type;
        using ServicesTuple = typename detail::template filter_non_config<Args...>::type;

        static inline typename detail::sync::template SafePtr<NimBLEServer, Server, true> server;
        static inline typename detail::sync::template SafePtr<NimBLEAdvertising, Server, true> adv;

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

        /**
           * @brief Initialize BLE server
           * @pre MUST be called after FreeRTOS scheduler has started
           * @pre On Arduino: Call from setup() or later, NOT from global constructors
           * @pre On ESP-IDF: Call from task context, NOT from app_main() before scheduler
           * @thread_safety Thread-safe once the scheduler is running
           */
        static bool init() {
            static std::atomic_flag init_called = ATOMIC_FLAG_INIT;
            if (init_called.test_and_set(std::memory_order_acq_rel)) {
                Serial.println("[BLEX] init: already initialized, returning");
                return server.get() != nullptr;
            }

            Serial.println("🟢 Initializing BLE server...");
            Serial.println("[BLEX] init: calling NimBLEDevice::init");
            NimBLEDevice::init(DeviceName);
            // Only set MTU if not using sentinel value (0 = use NimBLE default)
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

        // Runtime tuning API for power management
        // Set TX power (-12 to +9 dBm on ESP32-S3)
        // Call updateAdvertising() to apply changes if advertising has already started
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

        // Set advertising interval (20-10240 ms)
        // Call updateAdvertising() to apply changes if advertising has already started
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

        // Apply runtime tuning changes (stops/reconfigures/restarts advertising)
        static void updateAdvertising() {
            auto* advertising = adv.get();
            if (!advertising) {
                Serial.println("❌ Advertising not initialized");
                return;
            }

            Serial.println("📡 Updating advertising configuration...");

            // Stop current advertising
            NimBLEDevice::stopAdvertising();

            // Reconfigure with runtime overrides
            using PassiveServices = typename detail::template filter_services_pack<detail::template is_passive_adv_pred, ServicesTuple>::type;
            using ActiveServices = typename detail::template filter_services_pack<detail::template is_active_adv_pred, ServicesTuple>::type;
            using Adv = BlexAdvertising<PassiveServices, ActiveServices, AdvConfig>;

            Adv::configure(adv.get(), DeviceName, ShortName,
                               runtime_tx_power_,
                               runtime_adv_interval_min_,
                               runtime_adv_interval_max_);

            // Restart advertising
            NimBLEDevice::startAdvertising();
            Serial.println("✓ Advertising updated and restarted");
        }

    private:
        // Type alias for ServicesPack
        template<typename... Services>
        using ServicesPack = typename detail::template ServicesPack<Services...>;

        template<typename ServiceOrWrapped>
        static void register_service() {
            using ActualService = typename detail::template unwrap_service_impl<ServiceOrWrapped>::type;
            blex::register_service<ActualService>(server.get(), adv.get());
        }

        template<typename ServiceOrWrapped>
        static void start_service() {
            using ActualService = typename detail::template unwrap_service_impl<ServiceOrWrapped>::type;
            if (auto* srv = server.get()) {
                if (auto* s = srv->getServiceByUUID(detail::template make_uuid<ActualService::uuid>())) {
                    s->start();
                }
            }
        }

        // Helper to register all services from ServicesPack
        template<typename... Services>
        static void register_all_services(ServicesPack<Services...>) {
            (register_service<Services>(), ...);
        }

        // Helper to start all services from ServicesPack
        template<typename... Services>
        static void start_all_services(ServicesPack<Services...>) {
            (start_service<Services>(), ...);
        }

        static void configureAdvertising() {
            // Use BlexAdvertising to configure advertising with compile-time service filtering
            using PassiveServices = typename detail::template filter_services_pack<detail::template is_passive_adv_pred, ServicesTuple>::type;
            using ActiveServices = typename detail::template filter_services_pack<detail::template is_active_adv_pred, ServicesTuple>::type;
            using Adv = BlexAdvertising<PassiveServices, ActiveServices, AdvConfig>;

            Adv::configure(adv.get(), DeviceName, ShortName);
        }
    };

    // NimBLE Registration
#ifdef BLEX_NIMBLE_AVAILABLE
    template<typename... Descriptors>
    static void register_all_descriptors(NimBLECharacteristic* pC, typename detail::template DescriptorsPack<Descriptors...>) {
        (Descriptors::register_descriptor(pC), ...);
    }

    template<typename CharT>
    static NimBLECharacteristic* register_characteristic(NimBLEService* svc) {
        NimBLECharacteristic* pC = svc->createCharacteristic(
            detail::template make_uuid<CharT::uuid>(),
            // ReSharper disable once CppRedundantQualifier
            (CharT::perms_type::canRead    ? NIMBLE_PROPERTY::READ  : 0) |
            // ReSharper disable once CppRedundantQualifier
            (CharT::perms_type::canWrite   ? NIMBLE_PROPERTY::WRITE : 0) |
            // ReSharper disable once CppRedundantQualifier
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
                static typename detail::template BLECharShim<CharT> shim;
                pC->setCallbacks(&shim);
            }
        }

        return pC;
    }

    template<typename... Chars>
    static void register_all_characteristics(NimBLEService* svc, typename detail::template CharsPack<Chars...>) {
        (register_characteristic<Chars>(svc), ...);
    }

    template<typename ServiceT>
    static NimBLEService* register_service(NimBLEServer* server, NimBLEAdvertising* adv) {
        ServiceT::validate();

        NimBLEService* svc = server->createService(detail::template make_uuid<ServiceT::uuid>());

        register_all_characteristics(svc, typename ServiceT::chars_pack{});

        if (ServiceT::onAdvertiseStart) {
            if (adv) ServiceT::onAdvertiseStart();
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

// Convenience alias for default policies (auto-detected based on a platform)
using blexDefault = blex<>;

#endif //BLEX_HPP_