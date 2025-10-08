// Package device provides Bluetooth Low Energy (BLE) connection management and
// GATT service abstractions for macOS.
//
// This package implements a complete BLE client stack with support for:
//   - Connection lifecycle management (connect, disconnect, reconnect)
//   - GATT service and characteristic discovery
//   - Characteristic read/write/notify operations
//   - Real-time notification streaming with multiple patterns
//   - Thread-safe concurrent operations with mutex protection
//   - Object pooling for high-performance notification handling
package device
