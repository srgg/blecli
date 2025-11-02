/**
 * BLEX Core - Pure C++ type-level BLE API
 */

#ifndef BLEX_CORE_HPP_
#define BLEX_CORE_HPP_

#include <tuple>
#include <type_traits>
#include <cstring>
#include <cstdint>

// Forward declare blex template for friendship
template<template<typename> class LockPolicy>
struct blex;

namespace blex_core {

// ---------------------- Connection Abstraction (NimBLE-agnostic) ----------------------

// Connection information (implementation-agnostic)
struct ConnectionInfo {
    const char* address;      // MAC address as string (e.g., "aa:bb:cc:dd:ee:ff")
    uint16_t conn_handle;     // Connection handle
    uint16_t mtu;             // Current MTU size
};

// Disconnect reason codes (subset of BLE spec)
enum class DisconnectReason : uint8_t {
    Unknown = 0,
    UserTerminated = 0x13,
    RemoteTerminated = 0x13,
    ConnectionTimeout = 0x08,
    LocalHostTerminated = 0x16,
};

// ---------------------- C++20 Concepts ----------------------

// UUID type concept
template<typename T>
concept UuidType = requires {
    requires std::is_integral_v<std::remove_cv_t<std::remove_reference_t<T>>> ||
             std::is_same_v<std::remove_cv_t<std::remove_pointer_t<std::remove_cv_t<std::remove_reference_t<T>>>>, char> ||
             std::is_same_v<std::remove_cv_t<std::remove_extent_t<std::remove_cv_t<std::remove_reference_t<T>>>>, char>;
};

// Service wrapper concept
template<typename T>
concept ServiceWrapper = requires { typename T::service_type; };

// Advertising config concept
template<typename T>
concept AdvertisingConfigType = requires { typename T::is_blex_advertising_config_tag; };

// Connection config concept
template<typename T>
concept ConnectionConfigType = requires { typename T::is_blex_connection_config_tag; };

// Security config concept
template<typename T>
concept SecurityConfigType = requires { typename T::is_blex_security_config_tag; };

// Callback tag concept
template<typename T>
concept CharCallbackTag = requires { typename T::is_blex_char_callback_tag; };

// Server callback tag concept
template<typename T>
concept ServerCallbackTag = requires { typename T::is_blex_server_callback_tag; };

// Security callback tag concept
template<typename T>
concept SecurityCallbackTag = requires { typename T::is_blex_security_callback_tag; };

// Presentation format descriptor concept
template<typename T>
concept PresentationFormatDescriptor = requires { typename T::is_presentation_format_descriptor; };

// ---------------------- Type Traits and Metaprogramming ----------------------

// UUID type validation (compile-time evaluation)
template<typename T>
static constexpr void check_uuid_type() {
    static_assert(UuidType<T>, "UUID must be an integer (e.g., uint16_t) or a C string (char*/char[])");
}

// Compile-time string length helper
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

// Service wrapper detection (trait for backwards compatibility)
template<typename T>
struct is_service_wrapper : std::bool_constant<ServiceWrapper<T>> {};

// Unwrap service type (concept-based with SFINAE for member access)
template<typename T, typename = void>
struct unwrap_service_impl {
    using type = T;
};

template<typename T>
struct unwrap_service_impl<T, std::enable_if_t<ServiceWrapper<T>>> {
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

// Presentation Format descriptor detection (concept-based)
template<typename T>
static constexpr bool has_presentation_format_marker() {
    return PresentationFormatDescriptor<T>;
}

// Predicates for filtering (concept-based with SFINAE fallback for member access)
template<typename T, typename = void>
struct is_passive_adv_pred : std::false_type {};

template<typename T>
struct is_passive_adv_pred<T, std::enable_if_t<ServiceWrapper<T>>>
    : std::bool_constant<T::passive_adv> {};

template<typename T, typename = void>
struct is_active_adv_pred : std::false_type {};

template<typename T>
struct is_active_adv_pred<T, std::enable_if_t<ServiceWrapper<T>>>
    : std::bool_constant<T::active_adv> {};

// Config type detection (concept-based with trait wrappers for compatibility)
template<typename T>
struct is_advertising_config : std::bool_constant<AdvertisingConfigType<T>> {};

template<typename T>
struct is_connection_config : std::bool_constant<ConnectionConfigType<T>> {};

template<typename T>
struct is_security_config : std::bool_constant<SecurityConfigType<T>> {};

// Server callback tag type detection (concept-based with trait wrapper)
template<typename T>
struct is_server_callback_tag : std::bool_constant<ServerCallbackTag<T>> {};

// Security callback tag type detection (concept-based with trait wrapper)
template<typename T>
struct is_security_callback_tag : std::bool_constant<SecurityCallbackTag<T>> {};

// Helper to concatenate ServicesPack types
template<typename Pack1, typename Pack2>
struct concat_pack;

template<typename... S1, typename... S2>
struct concat_pack<ServicesPack<S1...>, ServicesPack<S2...>> {
    using type = ServicesPack<S1..., S2...>;
};

// Filter out config types, server callbacks, and security callbacks to get only Services
template<typename...>
struct filter_non_config {
    using type = ServicesPack<>;  // Base case: empty pack
};

template<typename First, typename... Rest>
struct filter_non_config<First, Rest...> {
    using filtered_rest = typename filter_non_config<Rest...>::type;
    using type = std::conditional_t<
        (is_advertising_config<First>::value ||
         is_connection_config<First>::value ||
         is_security_config<First>::value ||
         is_server_callback_tag<First>::value ||
         is_security_callback_tag<First>::value),
        filtered_rest,
        typename concat_pack<ServicesPack<First>, filtered_rest>::type
    >;
};

// Callback validation
template<typename T, auto CallbackFunc>
struct CallbackTraits {
    static constexpr bool is_valid_on_read = std::is_invocable_v<decltype(CallbackFunc), T&>;
    static constexpr bool is_valid_on_write = std::is_invocable_v<decltype(CallbackFunc), const T&>;
    static constexpr bool is_valid_on_status = std::is_invocable_v<decltype(CallbackFunc), int>;
    static constexpr bool is_valid_on_subscribe = std::is_invocable_v<decltype(CallbackFunc), uint16_t>;
};

// Characteristic callback tag type detection (concept-based with trait wrapper)
template<typename T>
struct is_char_callback_tag : std::bool_constant<CharCallbackTag<T>> {};

// Server callback extraction helper - finds server callback tag with matching type in variadic Args
template<int TargetType, typename...>
struct find_server_callback;

// Helper to safely check if a type is a server callback tag with matching type
template<typename T, int TargetType, typename = void>
struct matches_server_callback_type : std::false_type {};

template<typename T, int TargetType>
struct matches_server_callback_type<T, TargetType, std::enable_if_t<is_server_callback_tag<T>::value>>
    : std::bool_constant<T::callback_type == TargetType> {};

// Specialization helper for server callbacks
template<int TargetType, bool IsMatch, typename First, typename... Rest>
struct find_server_callback_impl {
    static constexpr auto value = find_server_callback<TargetType, Rest...>::value;
};

template<int TargetType, typename First, typename... Rest>
struct find_server_callback_impl<TargetType, true, First, Rest...> {
    static constexpr auto value = First::callback;
};

// Base case: no matching server callback found
template<int TargetType, typename...>
struct find_server_callback {
    static constexpr void(*value)() = nullptr;
};

// Recursive case: check if First matches, otherwise continue searching
template<int TargetType, typename First, typename... Rest>
struct find_server_callback<TargetType, First, Rest...> {
private:
    static constexpr bool is_match = matches_server_callback_type<First, TargetType>::value;
public:
    static constexpr auto value = find_server_callback_impl<TargetType, is_match, First, Rest...>::value;
};

// Security callback extraction helper - finds security callback tag with matching type in variadic Args
template<int TargetType, typename...>
struct find_security_callback;

// Helper to safely check if a type is a security callback tag with matching type
template<typename T, int TargetType, typename = void>
struct matches_security_callback_type : std::false_type {};

template<typename T, int TargetType>
struct matches_security_callback_type<T, TargetType, std::enable_if_t<is_security_callback_tag<T>::value>>
    : std::bool_constant<T::callback_type == TargetType> {};

// Specialization helper for security callbacks
template<int TargetType, bool IsMatch, typename First, typename... Rest>
struct find_security_callback_impl {
    static constexpr auto value = find_security_callback<TargetType, Rest...>::value;
};

template<int TargetType, typename First, typename... Rest>
struct find_security_callback_impl<TargetType, true, First, Rest...> {
    static constexpr auto value = First::callback;
};

// Base case: no matching security callback found
template<int TargetType, typename...>
struct find_security_callback {
    static constexpr void(*value)() = nullptr;
};

// Recursive case: check if First matches, otherwise continue searching
template<int TargetType, typename First, typename... Rest>
struct find_security_callback<TargetType, First, Rest...> {
private:
    static constexpr bool is_match = matches_security_callback_type<First, TargetType>::value;
public:
    static constexpr auto value = find_security_callback_impl<TargetType, is_match, First, Rest...>::value;
};

} // namespace blex_core

// ---------------------- Characteristic Callback Tag Types ----------------------

// Callback tag templates (user-facing API)
template<auto Func>
struct OnRead {
    using is_blex_char_callback_tag = void;
    static constexpr int callback_type = 0;  // 0 = Read
    static constexpr auto callback = Func;
};

template<auto Func>
struct OnWrite {
    using is_blex_char_callback_tag = void;
    static constexpr int callback_type = 1;  // 1 = Write
    static constexpr auto callback = Func;
};

template<auto Func>
struct OnStatus {
    using is_blex_char_callback_tag = void;
    static constexpr int callback_type = 2;  // 2 = Status
    static constexpr auto callback = Func;
};

template<auto Func>
struct OnSubscribe {
    using is_blex_char_callback_tag = void;
    static constexpr int callback_type = 3;  // 3 = Subscribe
    static constexpr auto callback = Func;
};

// ---------------------- Server Callback Tag Types ----------------------

// Server callback tag templates (user-facing API)
template<auto Func>
struct OnConnect {
    using is_blex_server_callback_tag = void;
    static constexpr int callback_type = 0;  // 0 = Connect
    static constexpr auto callback = Func;
};

template<auto Func>
struct OnDisconnect {
    using is_blex_server_callback_tag = void;
    static constexpr int callback_type = 1;  // 1 = Disconnect
    static constexpr auto callback = Func;
};

template<auto Func>
struct OnMTUChange {
    using is_blex_server_callback_tag = void;
    static constexpr int callback_type = 2;  // 2 = MTU Change
    static constexpr auto callback = Func;
};

// ---------------------- Security Callback Tag Types ----------------------

// Security callback tag templates (user-facing API)
template<auto Func>
struct OnPasskeyRequest {
    using is_blex_security_callback_tag = void;
    static constexpr int callback_type = 0;  // 0 = Passkey Request
    static constexpr auto callback = Func;
};

template<auto Func>
struct OnAuthComplete {
    using is_blex_security_callback_tag = void;
    static constexpr int callback_type = 1;  // 1 = Auth Complete
    static constexpr auto callback = Func;
};

template<auto Func>
struct OnConfirmPasskey {
    using is_blex_security_callback_tag = void;
    static constexpr int callback_type = 2;  // 2 = Confirm Passkey
    static constexpr auto callback = Func;
};

template<auto Func>
struct OnPasskeyDisplay {
    using is_blex_security_callback_tag = void;
    static constexpr int callback_type = 3;  // 3 = Passkey Display
    static constexpr auto callback = Func;
};

// Forward declarations for config templates (needed by extract helpers)
template<int8_t, uint16_t, uint16_t, auto>
struct AdvertisingConfig;

template<uint16_t, uint16_t, uint16_t, uint16_t, uint16_t>
struct ConnectionConfig;

template<uint8_t, bool, bool, bool>
struct SecurityConfig;

namespace blex_core {

// Callback extraction helper - finds callback tag with matching type in variadic Args
// Forward declaration
template<int TargetType, typename...>
struct find_char_callback;

// Helper to safely check if a type is a callback tag with matching type
template<typename T, int TargetType, typename = void>
struct matches_char_callback_type : std::false_type {};

template<typename T, int TargetType>
struct matches_char_callback_type<T, TargetType, std::enable_if_t<is_char_callback_tag<T>::value>>
    : std::bool_constant<T::callback_type == TargetType> {};

// Specialization helper: avoids accessing non-existent members by using template specialization
template<int TargetType, bool IsMatch, typename First, typename... Rest>
struct find_char_callback_impl {
    static constexpr auto value = find_char_callback<TargetType, Rest...>::value;
};

template<int TargetType, typename First, typename... Rest>
struct find_char_callback_impl<TargetType, true, First, Rest...> {
    static constexpr auto value = First::callback;
};

// Base case: no matching callback found - return nullptr with explicit type
template<int TargetType, typename...>
struct find_char_callback {
    static constexpr void(*value)() = nullptr;  // Explicit function pointer type for nullptr
};

// Recursive case: check if First matches, otherwise continue searching
template<int TargetType, typename First, typename... Rest>
struct find_char_callback<TargetType, First, Rest...> {
private:
    // Check if First is a callback tag with matching type (using SFINAE to avoid accessing non-existent members)
    static constexpr bool is_match = matches_char_callback_type<First, TargetType>::value;
public:
    static constexpr auto value = find_char_callback_impl<TargetType, is_match, First, Rest...>::value;
};

// Helper to concatenate DescriptorsPack
template<typename Pack1, typename Pack2>
struct concat_descriptors;

template<typename... D1, typename... D2>
struct concat_descriptors<DescriptorsPack<D1...>, DescriptorsPack<D2...>> {
    using type = DescriptorsPack<D1..., D2...>;
};

// ---------------------- Hybrid Concept + Static Assert Validation ----------------------

// Validate that Args are either callback tags or descriptor types
template<typename T>
struct is_valid_characteristic_arg : std::bool_constant<
    is_char_callback_tag<T>::value ||
    // Accept any type as a potential descriptor (will be validated by descriptor traits later)
    !std::is_same_v<T, std::nullptr_t>  // Reject nullptr_t
> {};

// Macros for cleaner error message formatting
#define BLEX_ERROR_HEADER \
"\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n" \
"❌  BLEX Characteristic Argument Error\n" \
"━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"

#define BLEX_ERROR_FOOTER \
"━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"

// Individual argument validator with detailed static_assert - shows UUID, type, and position
template<auto UUID, typename T, size_t Index>
struct validate_single_arg {
    static_assert(
        is_valid_characteristic_arg<T>::value,
        BLEX_ERROR_HEADER
        "\n"
        "  ▸ LOOK ABOVE for: validate_single_arg<UUID, BAD_TYPE, INDEX>\n"
        "\n"
        "    UUID  = Characteristic UUID (identifies which characteristic has the error)\n"
        "    INDEX = 0-indexed position in Args (add 1 for actual position after Permissions)\n"
        "\n"
        "  Expected arguments:\n"
        "    • OnRead<func>, OnWrite<func>, OnStatus<func>, OnSubscribe<func>\n"
        "    • UserDescription, PresentationFormat, AggregateFormat\n"
        "\n"
        "  ✓ Valid:   Characteristic<T, UUID, Permissions<...>, Descriptor>\n"
        "  ✗ Invalid: std::nullptr_t, raw function pointers\n"
        "\n"
        BLEX_ERROR_FOOTER
    );
    static constexpr bool value = true;
};

// Hybrid concept: checks validity AND forces diagnostic on failure
// The OR trick: valid ? true : (instantiate_validator, false)
template<auto UUID, typename T, size_t Index>
concept ValidCharArgWithDiagnostic =
    is_valid_characteristic_arg<T>::value ||  // ← Fast path: if valid, done
    (validate_single_arg<UUID, T, Index>::value, false);  // ← Force static_assert, then fail

// Helper to validate all args with indices using the diagnostic concept
template<auto UUID, typename IndexSeq, typename... Args>
struct validate_all_args_with_diagnostic_impl;

template<auto UUID, size_t... Indices, typename... Args>
struct validate_all_args_with_diagnostic_impl<UUID, std::index_sequence<Indices...>, Args...> {
    static constexpr bool value = (ValidCharArgWithDiagnostic<UUID, Args, Indices> && ...);
};

// Concept that validates all args with diagnostics
template<auto UUID, typename... Args>
concept AllValidCharArgs = validate_all_args_with_diagnostic_impl<
    UUID,
    std::make_index_sequence<sizeof...(Args)>,
    Args...
>::value;

// Filter out callback tags to get only Descriptors
template<typename...>
struct filter_descriptors_from_args {
    using type = DescriptorsPack<>;
};

template<typename First, typename... Rest>
struct filter_descriptors_from_args<First, Rest...> {
    using filtered_rest = typename filter_descriptors_from_args<Rest...>::type;
    using type = std::conditional_t<
        is_char_callback_tag<First>::value,
        filtered_rest,
        typename concat_descriptors<DescriptorsPack<First>, filtered_rest>::type
    >;
};


// Extract AdvertisingConfig from variadic args, or use default
template<typename... /*Args*/>
struct extract_adv_config {
    using type = AdvertisingConfig<-127, 0, 0, 0>;
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
    // Default
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

// Extract SecurityConfig from variadic args, or use default
template<typename...>
struct extract_security_config {
    // Default: NoInputNoOutput, no MITM, bonding enabled, secure connections enabled
    using type = SecurityConfig<0, false, true, true>;
};

template<typename First, typename... Rest>
struct extract_security_config<First, Rest...> {
    using type = std::conditional_t<
        is_security_config<First>::value,
        First,
        typename extract_security_config<Rest...>::type
    >;
};

} // namespace blex_core

// ---------------------- Public API Types ----------------------

// Permission types (basic, no security)
struct Readable   {
    static constexpr bool canRead = true;
    static constexpr bool canWrite = false;
    static constexpr bool canNotify = false;
    static constexpr bool requireEncryption = false;
    static constexpr bool requireAuthentication = false;
    static constexpr bool requireAuthorization = false;
};

struct Writable   {
    static constexpr bool canRead = false;
    static constexpr bool canWrite = true;
    static constexpr bool canNotify = false;
    static constexpr bool requireEncryption = false;
    static constexpr bool requireAuthentication = false;
    static constexpr bool requireAuthorization = false;
};

struct Notifiable {
    static constexpr bool canRead = false;
    static constexpr bool canWrite = false;
    static constexpr bool canNotify = true;
    static constexpr bool requireEncryption = false;
    static constexpr bool requireAuthentication = false;
    static constexpr bool requireAuthorization = false;
};

// Security-enhanced permission types
struct ReadEncrypted {
    static constexpr bool canRead = true;
    static constexpr bool canWrite = false;
    static constexpr bool canNotify = false;
    static constexpr bool requireEncryption = true;
    static constexpr bool requireAuthentication = false;
    static constexpr bool requireAuthorization = false;
};

struct WriteEncrypted {
    static constexpr bool canRead = false;
    static constexpr bool canWrite = true;
    static constexpr bool canNotify = false;
    static constexpr bool requireEncryption = true;
    static constexpr bool requireAuthentication = false;
    static constexpr bool requireAuthorization = false;
};

struct ReadAuthenticated {
    static constexpr bool canRead = true;
    static constexpr bool canWrite = false;
    static constexpr bool canNotify = false;
    static constexpr bool requireEncryption = true;
    static constexpr bool requireAuthentication = true;
    static constexpr bool requireAuthorization = false;
};

struct WriteAuthenticated {
    static constexpr bool canRead = false;
    static constexpr bool canWrite = true;
    static constexpr bool canNotify = false;
    static constexpr bool requireEncryption = true;
    static constexpr bool requireAuthentication = true;
    static constexpr bool requireAuthorization = false;
};

struct ReadAuthorized {
    static constexpr bool canRead = true;
    static constexpr bool canWrite = false;
    static constexpr bool canNotify = false;
    static constexpr bool requireEncryption = true;
    static constexpr bool requireAuthentication = true;
    static constexpr bool requireAuthorization = true;
};

struct WriteAuthorized {
    static constexpr bool canRead = false;
    static constexpr bool canWrite = true;
    static constexpr bool canNotify = false;
    static constexpr bool requireEncryption = true;
    static constexpr bool requireAuthentication = true;
    static constexpr bool requireAuthorization = true;
};

template<typename... Perms>
struct Permissions {
    static_assert(
        (... && (std::is_same_v<Perms, Readable> ||
                 std::is_same_v<Perms, Writable> ||
                 std::is_same_v<Perms, Notifiable> ||
                 std::is_same_v<Perms, ReadEncrypted> ||
                 std::is_same_v<Perms, WriteEncrypted> ||
                 std::is_same_v<Perms, ReadAuthenticated> ||
                 std::is_same_v<Perms, WriteAuthenticated> ||
                 std::is_same_v<Perms, ReadAuthorized> ||
                 std::is_same_v<Perms, WriteAuthorized>)),
        "Permissions only accept: Readable, Writable, Notifiable, "
        "ReadEncrypted, WriteEncrypted, ReadAuthenticated, WriteAuthenticated, "
        "ReadAuthorized, WriteAuthorized"
    );

    // Aggregate basic capabilities
    static constexpr bool canRead   = (... || Perms::canRead);
    static constexpr bool canWrite  = (... || Perms::canWrite);
    static constexpr bool canNotify = (... || Perms::canNotify);

    // Aggregate security requirements (most restrictive wins)
    static constexpr bool requireEncryption = (... || Perms::requireEncryption);
    static constexpr bool requireAuthentication = (... || Perms::requireAuthentication);
    static constexpr bool requireAuthorization = (... || Perms::requireAuthorization);
};

/**
 * @brief Bluetooth Low Energy Characteristic Presentation Format Field values.
 *
 * These values are used in the Characteristic Presentation Format descriptor (UUID 0x2904)
 * to indicate the data type of the characteristic value.
 *
 * Reference: Bluetooth Core Specification Supplement (CSS) Part B, Section 1.3
 * Assigned Numbers: https://www.bluetooth.com/specifications/assigned-numbers/
 */
enum class GattFormat : uint8_t {
    // Unsigned Integers
    kReserved       = 0x00, // Reserved for future use
    kBoolean        = 0x01, // 1-bit boolean
    k2Bit           = 0x02, // 2-bit unsigned integer
    k4Bit           = 0x03, // 4-bit unsigned integer
    kUint8          = 0x04, // 8-bit unsigned integer
    kUint12         = 0x05, // 12-bit unsigned integer
    kUint16         = 0x06, // 16-bit unsigned integer
    kUint24         = 0x07, // 24-bit unsigned integer
    kUint32         = 0x08, // 32-bit unsigned integer
    kUint48         = 0x09, // 48-bit unsigned integer
    kUint64         = 0x0A, // 64-bit unsigned integer
    kUint128        = 0x0B, // 128-bit unsigned integer

    // Signed Integers
    kSint8          = 0x0C, // 8-bit signed integer
    kSint16         = 0x0D, // 16-bit signed integer
    kSint24         = 0x0E, // 24-bit signed integer
    kSint32         = 0x0F, // 32-bit signed integer
    kSint48         = 0x10, // 48-bit signed integer
    kSint64         = 0x11, // 64-bit signed integer
    kSint128        = 0x12, // 128-bit signed integer

    // Floating Point Types
    kFloat32        = 0x13, // IEEE-754 32-bit floating point
    kFloat64        = 0x14, // IEEE-754 64-bit floating point
    kSFloat         = 0x15, // IEEE-11073 16-bit SFLOAT
    kFloat          = 0x16, // IEEE-11073 32-bit FLOAT

    // Other Types
    kDuplicatedUInt16 = 0x17, // IEEE-11073 16-bit Duplicated
    kUtf8String     = 0x18, // UTF-8 string
    kUtf16String    = 0x19, // UTF-16 string
    kStruct         = 0x1A  // Opaque structure
};

/**
 * @brief Bluetooth SIG Assigned Unit UUIDs for GATT Characteristic Presentation Format
 *
 * Used in the Characteristic Presentation Format descriptor (UUID 0x2904) unit field
 * to indicate the physical unit of the characteristic value.
 *
 * Reference: Bluetooth SIG Assigned Numbers - Units
 * https://www.bluetooth.com/specifications/assigned-numbers/units/
 *
 * Note: Unit codes are 16-bit UUID values. Combine with exponent field for scaling.
 * Example: Tesla (0x2774) with exponent -6 represents microtesla (µT)
 */
enum class GattUnit : uint16_t {
    // Dimensionless
    kUnitless                           = 0x2700, // unitless

    // Length
    kMetre                              = 0x2701, // metre (m)

    // Mass
    kKilogram                           = 0x2702, // kilogram (kg)

    // Time
    kSecond                             = 0x2703, // second (s)
    kMinute                             = 0x2760, // minute (min)
    kHour                               = 0x2761, // hour (h)
    kDay                                = 0x2762, // day (d)

    // Electric Current
    kAmpere                             = 0x2704, // ampere (A)

    // Thermodynamic Temperature
    kKelvin                             = 0x2705, // kelvin (K)
    kDegreeCelsius                      = 0x272F, // degree Celsius (°C)
    kDegreeFahrenheit                   = 0x27AC, // degree Fahrenheit (°F)

    // Amount of Substance
    kMole                               = 0x2706, // mole (mol)

    // Luminous Intensity
    kCandela                            = 0x2707, // candela (cd)

    // Area
    kSquareMetre                        = 0x2710, // square metre (m²)

    // Volume
    kCubicMetre                         = 0x2711, // cubic metre (m³)
    kLitre                              = 0x2767, // litre (L)

    // Velocity
    kMetrePerSecond                     = 0x2712, // metre per second (m/s)

    // Acceleration
    kMetrePerSecondSquared              = 0x2713, // metre per second squared (m/s²)

    // Wave Number
    kReciprocalMetre                    = 0x2714, // reciprocal metre (m⁻¹)

    // Density
    kKilogramPerCubicMetre              = 0x2715, // kilogram per cubic metre (kg/m³)

    // Surface Density
    kKilogramPerSquareMetre             = 0x2716, // kilogram per square metre (kg/m²)

    // Specific Volume
    kCubicMetrePerKilogram              = 0x2717, // cubic metre per kilogram (m³/kg)

    // Current Density
    kAmperePerSquareMetre               = 0x2718, // ampere per square metre (A/m²)

    // Magnetic Field Strength
    kAmperePerMetre                     = 0x2719, // ampere per metre (A/m)

    // Concentration
    kMolePerCubicMetre                  = 0x271A, // mole per cubic metre (mol/m³)

    // Mass Concentration
    kKilogramPerCubicMetreConc          = 0x271B, // kilogram per cubic metre (kg/m³)

    // Luminance
    kCandelaPerSquareMetre              = 0x271C, // candela per square metre (cd/m²)

    // Refractive Index
    kRefractiveIndex                    = 0x271D, // refractive index

    // Relative Permeability
    kRelativePermeability               = 0x271E, // relative permeability

    // Plane Angle
    kRadian                             = 0x2720, // radian (rad)
    kDegree                             = 0x2763, // degree (°)

    // Solid Angle
    kSteradian                          = 0x2721, // steradian (sr)

    // Frequency
    kHertz                              = 0x2722, // hertz (Hz)

    // Force
    kNewton                             = 0x2723, // newton (N)

    // Pressure, Stress
    kPascal                             = 0x2724, // pascal (Pa)
    kBar                                = 0x2780, // bar
    kMillimetreOfMercury                = 0x2781, //millimeter of mercury (mmHg)

    // Energy, Work, Heat
    kJoule                              = 0x2725, // joule (J)
    kKilowattHour                       = 0x2726, // kilowatt hour (kWh)

    // Power, Radiant Flux
    kWatt                               = 0x2727, // watt (W)

    // Electric Charge
    kCoulomb                            = 0x2728, // coulomb (C)

    // Electric Potential Difference
    kVolt                               = 0x2729, // volt (V)

    // Capacitance
    kFarad                              = 0x272A, // farad (F)

    // Electric Resistance
    kOhm                                = 0x272B, // ohm (Ω)

    // Electric Conductance
    kSiemens                            = 0x272C, // siemens (S)

    // Magnetic Flux
    kWeber                              = 0x272D, // weber (Wb)

    // Magnetic Flux Density
    kTesla                              = 0x272E, // tesla (T)

    // Inductance
    kHenry                              = 0x2730, // henry (H)

    // Luminous Flux
    kLumen                              = 0x2731, // lumen (lm)

    // Illuminance
    kLux                                = 0x2732, // lux (lx)

    // Activity (Radionuclide)
    kBecquerel                          = 0x2733, // becquerel (Bq)

    // Absorbed Dose
    kGray                               = 0x2734, // gray (Gy)

    // Dose Equivalent
    kSievert                            = 0x2735, // sievert (Sv)

    // Catalytic Activity
    kKatal                              = 0x2736, // katal (kat)

    // Dynamic Viscosity
    kPascalSecond                       = 0x2740, // pascal second (Pa·s)

    // Moment of Force
    kNewtonMetre                        = 0x2741, // newton metre (N·m)

    // Surface Tension
    kNewtonPerMetre                     = 0x2742, // newton per metre (N/m)

    // Angular Velocity
    kRadianPerSecond                    = 0x2743, // radian per second (rad/s)

    // Angular Acceleration
    kRadianPerSecondSquared             = 0x2744, // radian per second squared (rad/s²)

    // Heat Flux Density
    kWattPerSquareMetre                 = 0x2745, // watt per square metre (W/m²)

    // Heat Capacity, Entropy
    kJoulePerKelvin                     = 0x2746, // joule per kelvin (J/K)

    // Specific Heat Capacity
    kJoulePerKilogramKelvin             = 0x2747, // joule per kilogram kelvin (J/(kg·K))

    // Specific Energy
    kJoulePerKilogram                   = 0x2748, // joule per kilogram (J/kg)

    // Thermal Conductivity
    kWattPerMetreKelvin                 = 0x2749, // watt per metre kelvin (W/(m·K))

    // Energy Density
    kJoulePerCubicMetre                 = 0x274A, // joule per cubic metre (J/m³)

    // Electric Field Strength
    kVoltPerMetre                       = 0x274B, // volt per metre (V/m)

    // Electric Charge Density
    kCoulombPerCubicMetre               = 0x274C, // coulomb per cubic metre (C/m³)

    // Surface Charge Density
    kCoulombPerSquareMetre              = 0x274D, // coulomb per square metre (C/m²)

    // Electric Flux Density
    kCoulombPerSquareMetreFlux          = 0x274E, // coulomb per square metre (C/m²)

    // Permittivity
    kFaradPerMetre                      = 0x274F, // farad per metre (F/m)

    // Permeability
    kHenryPerMetre                      = 0x2750, // henry per metre (H/m)

    // Molar Energy
    kJoulePerMole                       = 0x2751, // joule per mole (J/mol)

    // Molar Entropy, Molar Heat Capacity
    kJoulePerMoleKelvin                 = 0x2752, // joule per mole kelvin (J/(mol·K))

    // Exposure (X-rays, γ-rays)
    kCoulombPerKilogram                 = 0x2753, // coulomb per kilogram (C/kg)

    // Absorbed Dose Rate
    kGrayPerSecond                      = 0x2754, // gray per second (Gy/s)

    // Radiant Intensity
    kWattPerSteradian                   = 0x2755, // watt per steradian (W/sr)

    // Radiance
    kWattPerSquareMetreSteradian        = 0x2756, // watt per square metre steradian (W/(m²·sr))

    // Catalytic Activity Concentration
    kKatalPerCubicMetre                 = 0x2757, // katal per cubic metre (kat/m³)

    // Percentage
    kPercentage                         = 0x27AD, // percentage (%)

    // Parts Per Million
    kPartsPerMillion                    = 0x27AE, // parts per million (ppm)

    // Parts Per Billion
    kPartsPerBillion                    = 0x27AF, // parts per billion (ppb)

    // Mass Density (non-SI)
    kGramPerCubicCentimetre             = 0x27A0, // gram per cubic centimetre (g/cm³)

    // Concentration (non-SI)
    kMilligramPerDecilitre              = 0x27A1, // milligram per decilitre (mg/dL)
    kMillimolePerLitre                  = 0x27A2, // millimole per litre (mmol/L)

    // Beats Per Minute
    kBeatsPerMinute                     = 0x27A7, // beats per minute (bpm)

    // Revolutions Per Minute
    kRevolutionsPerMinute               = 0x27A8, // revolutions per minute (rpm)

    // Count
    kCount                              = 0x27B1, // count

    // Steps
    kSteps                              = 0x27B2  // steps
};

/**
 * @brief Bluetooth SIG Assigned Appearance Values
 *
 * Used to indicate the external appearance of the device to the user.
 * Appearance values are organized into categories and subcategories.
 *
 * Reference: Bluetooth SIG Assigned Numbers - Appearance Values
 * https://www.bluetooth.com/specifications/assigned-numbers/
 *
 * Format: Category (bits 15-6) | Subcategory (bits 5-0)
 */
enum class BleAppearance : uint16_t {
    // Unknown
    kUnknown                            = 0x0000, // Unknown

    // Generic category
    kGenericPhone                       = 0x0040, // Generic Phone
    kGenericComputer                    = 0x0080, // Generic Computer
    kGenericWatch                       = 0x00C0, // Generic Watch
    kGenericClock                       = 0x0100, // Generic Clock
    kGenericDisplay                     = 0x0140, // Generic Display
    kGenericRemoteControl               = 0x0180, // Generic Remote Control
    kGenericEyeGlasses                  = 0x01C0, // Generic Eye-glasses
    kGenericTag                         = 0x0200, // Generic Tag
    kGenericKeyring                     = 0x0240, // Generic Keyring
    kGenericMediaPlayer                 = 0x0280, // Generic Media Player
    kGenericBarcodeScanner              = 0x02C0, // Generic Barcode Scanner
    kGenericThermometer                 = 0x0300, // Generic Thermometer
    kGenericHeartRateSensor             = 0x0340, // Generic Heart rate Sensor
    kGenericBloodPressure               = 0x0380, // Generic Blood Pressure
    kGenericHumanInterfaceDevice        = 0x03C0, // Generic Human Interface Device (HID)
    kGenericGlucoseMeter                = 0x0400, // Generic Glucose Meter
    kGenericRunningWalkingSensor        = 0x0440, // Generic Running Walking Sensor
    kGenericCycling                     = 0x0480, // Generic Cycling
    kGenericPulseOximeter               = 0x0C40, // Generic Pulse Oximeter
    kGenericWeightScale                 = 0x0C80, // Generic Weight Scale
    kGenericOutdoorSportsActivity       = 0x1440, // Generic Outdoor Sports Activity

    // Sensor category (0x0540)
    kGenericSensor                      = 0x0540, // Generic Sensor
    kMotionSensor                       = 0x0541, // Motion Sensor
    kAirQualitySensor                   = 0x0542, // Air Quality Sensor
    kTemperatureSensor                  = 0x0543, // Temperature Sensor
    kHumiditySensor                     = 0x0544, // Humidity Sensor
    kLeakSensor                         = 0x0545, // Leak Sensor
    kSmokeSensor                        = 0x0546, // Smoke Sensor
    kOccupancySensor                    = 0x0547, // Occupancy Sensor
    kProximitySensor                    = 0x0548, // Proximity Sensor
    kMultiSensor                        = 0x0549, // Multi-Sensor
    kEnergyMeter                        = 0x054A, // Energy Meter
    kFlameSensor                        = 0x054B, // Flame Sensor
    kVehicleTirePressureSensor          = 0x054C, // Vehicle Tire Pressure Sensor
};

// Presentation Format value type for 0x2904 descriptor (7 bytes packed)
struct PresentationFormatValue {
    uint8_t format;
    int8_t exponent;
    uint16_t unit;
    uint8_t name_space;
    uint16_t description;
} __attribute__((packed));

// ---------------------- Configuration Templates ----------------------

// AdvertisingConfig: Compile-time advertising configuration with runtime tuning
template<int8_t TxPower = 0, uint16_t IntervalMin = 100, uint16_t IntervalMax = 150,
         auto Appearance = BleAppearance::kUnknown>
struct AdvertisingConfig {
    // Validate Appearance type and range FIRST (before using it)
    static_assert(std::is_same_v<decltype(Appearance), BleAppearance> ||
                  (std::is_integral_v<decltype(Appearance)> &&
                   static_cast<uint64_t>(Appearance) <= 0xFFFF),
                  "Appearance must be BleAppearance enum or uint16_t value [0x0000-0xFFFF]. "
                  "Example: BleAppearance::kGenericSensor or 0x0540");

    // Marker for trait detection
    using is_blex_advertising_config_tag = void;

    // Compile-time configuration (user-specified via template parameters)
    static constexpr int8_t default_tx_power = TxPower;
    static constexpr uint16_t default_adv_interval_min = IntervalMin;
    static constexpr uint16_t default_adv_interval_max = IntervalMax;
    static constexpr uint16_t default_appearance = static_cast<uint16_t>(Appearance);
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
template<
    uint16_t MTU = 247,
    uint16_t ConnIntervalMin = 12,
    uint16_t ConnIntervalMax = 12,
    uint16_t ConnLatency = 0,
    uint16_t SupervisionTimeout = 400
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

// SecurityConfig: Compile-time BLE security and pairing configuration
template<
    uint8_t IOCapabilities = 0,
    bool MITMProtection = false,
    bool Bonding = true,
    bool SecureConnections = true
>
struct SecurityConfig {
    // Marker for trait detection
    using is_blex_security_config_tag = void;

    // Compile-time configuration
    static constexpr uint8_t io_capabilities = IOCapabilities;
    static constexpr bool mitm_protection = MITMProtection;
    static constexpr bool bonding = Bonding;
    static constexpr bool secure_connections = SecureConnections;

    // IO Capability values (BLE Core Spec Vol 3, Part H, Section 2.3.5.1)
    static constexpr uint8_t IO_CAP_DISPLAY_ONLY = 0;
    static constexpr uint8_t IO_CAP_DISPLAY_YES_NO = 1;
    static constexpr uint8_t IO_CAP_KEYBOARD_ONLY = 2;
    static constexpr uint8_t IO_CAP_NO_INPUT_NO_OUTPUT = 3;
    static constexpr uint8_t IO_CAP_KEYBOARD_DISPLAY = 4;

    // Compile-time validation
    static_assert(IOCapabilities <= 4,
                 "IO Capabilities must be 0-4 (DisplayOnly, DisplayYesNo, KeyboardOnly, NoInputNoOutput, KeyboardDisplay)");
};

#endif // BLEX_CORE_HPP_