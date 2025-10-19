package main

import "errors"

// Command-level errors
var (
	// ErrConnectionLost indicates the BLE connection was unexpectedly lost during operation.
	// This is distinct from device.ErrNotConnected, which indicates an attempt to use
	// a device that was never connected or was already disconnected.
	ErrConnectionLost = errors.New("connection lost")
)
