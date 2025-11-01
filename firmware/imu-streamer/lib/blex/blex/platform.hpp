/**
 * BLEX Platform Layer - Platform detection, lock policies, and synchronization primitives
 */

#ifndef BLEX_PLATFORM_HPP_
#define BLEX_PLATFORM_HPP_

#include <atomic>
#include <mutex>
#include <cassert>

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

// ---------------------- Synchronization Primitives ----------------------

namespace blex_sync {

// RAII lock guard
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
    template<typename> class LockPolicy,
    bool Immutable = false
>
class SafePtr {
    std::atomic<T*> ptr_{nullptr};

    // RAII lock wrapper; uses the same per-Tag static lock
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
            ScopedLock lock;
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
            ScopedLock lock;
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
            ScopedLock lock;
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
template<typename FuncPtr, typename Tag, template<typename> class LockPolicy>
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
template<typename Tag, template<typename> class LockPolicy>
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

} // namespace blex_sync

#endif // BLEX_PLATFORM_HPP_