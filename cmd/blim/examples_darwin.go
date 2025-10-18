//go:build darwin

package main

const (
	exampleDeviceAddress = "01234567-89AB-CDEF-0123-456789ABCDEF"
	deviceAddressNote    = "Device address format: 128-bit UUID, with or without dashes\n  Examples: 01234567-89AB-CDEF-0123-456789ABCDEF or 0123456789ABCDEF0123456789ABCDEF\n  Use 'blim scan' to discover devices"
)
