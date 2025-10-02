# Blim

**Blim** is a lightweight, reusable Go library and CLI tool for working with Bluetooth Low Energy (BLE) devices.  
It provides high-level building blocks for scanning, bridging, and inspecting BLE devices — all in a clean, developer-friendly API.

## Key Features

- **Scan** for BLE devices quickly and reliably.
- **Bridge** BLE devices to serial, TCP, or other transport layers.
- **Inspect** BLE device data and characteristics programmatically.
- **CLI + Library**: Use `blim` from the command line, or import its packages into your Go projects.
- **Testable & Reusable**: Each core component (`scanner`, `bridge`, `inspector`) is a standalone package.

## Installation

```bash
go get github.com/srgg/blim
