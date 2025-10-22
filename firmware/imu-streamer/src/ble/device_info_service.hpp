#ifndef DEVICE_INFO_SVC_HPP_
#define DEVICE_INFO_SVC_HPP_

/*
 * Device Info Service
 * Provides BLE interface to query device-related information
 *
 * BLE Device Info Service (0x180A):
 * - 0x2A24: Model Number (READ)
 * - 0x2A25: Model Number (READ)
 * - 0x2A26: Firmware Revision (READ)
 * - 0x2A27: Hardware Revision (READ)
 * - 0x2A28: Software Revision (READ)
 * - 0x2A29: Manufacturer Name (READ)
 *
 * All values are automatically injected by version.py at build time
 * as preprocessor build flags (-D defines). The fallback values below
 * are used only if version.py fails to execute.
 */

// Manufacturer Name (BLE Device Information Service)
#ifndef MANUFACTURER_NAME
    #define MANUFACTURER_NAME "unknown";
#endif

// Serial Number (BLE Device Information Service)
#ifndef SERIAL_NUMBER
    #define SERIAL_NUMBER "unknown"
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

template<typename>
struct DeviceInfoServiceImpl {
    static  constexpr char service_uuid[] = "180A";

    static constexpr char manufacturer_name[] = MANUFACTURER_NAME;
    static constexpr char serial_number[] = SERIAL_NUMBER;
    static constexpr char hardware_version[] = HARDWARE_VERSION;
    static constexpr char model_number[] = MODEL_NUMBER;
    static constexpr char firmware_revision[] = FIRMWARE_VERSION;
    static  constexpr char software_revision[] = SOFTWARE_REVISION;
};

// Device Information Service Template (policy-agnostic)
// Instantiate with your chosen Blex policy: DeviceInfoService_<blim>::Service
template<typename Blex, typename C = DeviceInfoServiceImpl<Blex> >
struct DeviceInfoService : C, Blex::template Service<
        C::service_uuid,
        typename Blex::chars::template ManufacturerName<C::manufacturer_name>,
        typename Blex::chars::template ModelNumber<C::model_number>,
        typename Blex::chars::template SerialNumber<C::serial_number>,
        typename Blex::chars::template HardwareRevision<C::hardware_version>,
        typename Blex::chars::template FirmwareRevision<C::firmware_revision>,
        typename Blex::chars::template SoftwareRevision<C::software_revision>
    >{
};

#endif //DEVICE_INFO_SVC_HPP_