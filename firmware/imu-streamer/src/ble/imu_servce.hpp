#ifndef IMU_SVC_HPP_
#define IMU_SVC_HPP_

/*
 * IMU Streaming Service
 *
 * Provides BLE interface for real-time 9-axis IMU data streaming
 *
 * BLE IMU Service (0xFF10):
 * - 0xFF11: IMU Data (READ/NOTIFY) - 9x float32 (Accel, Gyro, Mag)
 *
 * Data format: [accelX, accelY, accelZ, gyroX, gyroY, gyroZ, magX, magY, magZ]
 * - Accelerometer: m/s² (unit 0x2713)
 * - Gyroscope: degrees/second (unit 0x2700 unitless)
 * - Magnetometer: µT (unit 0x2774 tesla, exponent -6)
 */

#include "blex.hpp"

template<typename Blex>
struct IMUServiceImpl {
    // User Description text
    static constexpr char IMU_DESC_TEXT[] = "IMU: Accel(m/s^2) | Gyro(dps) | Mag(uT)";

    // Presentation Format Descriptor values (0x2904)
    // Format: [format(1), exponent(1), unit(2), namespace(1), description(2)]

    // Accelerometer: IEEE-754 32-bit float, m/s² (unit 0x2713)
    static constexpr uint8_t ACCEL_FORMAT[7] = {
        0x06,       // Format: IEEE-754 32-bit float
        0x00,       // Exponent: 10^0
        0x13, 0x27, // Unit: 0x2713 (metres per second squared)
        0x01,       // Namespace: Bluetooth SIG Assigned Numbers
        0x00, 0x00  // Description: none
    };

    // Gyroscope: IEEE-754 32-bit float, degrees/second (unit 0x2700 unitless)
    static constexpr uint8_t GYRO_FORMAT[7] = {
        0x06,       // Format: IEEE-754 32-bit float
        0x00,       // Exponent: 10^0
        0x00, 0x27, // Unit: 0x2700 (unitless - no BLE standard unit for angular velocity)
        0x01,       // Namespace: Bluetooth SIG
        0x00, 0x00  // Description: none
    };

    // Magnetometer: IEEE-754 32-bit float, µT (unit 0x2774 tesla, exponent -6)
    static constexpr uint8_t MAG_FORMAT[7] = {
        0x06,       // Format: IEEE-754 32-bit float
        0xFA,       // Exponent: -6 (10^-6 for micro)
        0x74, 0x27, // Unit: 0x2774 (tesla)
        0x01,       // Namespace: Bluetooth SIG
        0x00, 0x00  // Description: none
    };

    // User Description Descriptor
    using IMUUserDesc = typename Blex::descriptors::template UserDescription<IMU_DESC_TEXT>;

    // IMU Characteristic - 9 floats (36 bytes): accel(3) + gyro(3) + mag(3)
    // Note: We use a custom characteristic with READ and NOTIFY
    // The presentation format descriptors will be added manually since Blex doesn't support
    // multiple presentation format descriptors with aggregate format descriptor yet
    using IMUChar = typename Blex::template Characteristic<
        float[9],
        static_cast<uint16_t>(0XFF11),
        typename Blex::template Permissions<typename Blex::Readable, typename Blex::Notifiable>
    >;
};

// IMU Service Service Template (policy-agnostic)
// Instantiate with your chosen Blex policy: IMUService<blim>::Service
template<typename Blex, typename C = IMUServiceImpl<Blex> >
struct IMUService : C, Blex::template Service<
    0xFF10,
    typename C::IMUChar
> {
};
#endif //IMU_SVC_HPP_