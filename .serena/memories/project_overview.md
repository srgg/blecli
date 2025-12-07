# BLE CLI Tool Project Overview

## Purpose
A macOS CLI tool written in Go that provides comprehensive Bluetooth Low Energy (BLE) functionality including:
- BLE peripheral scanning and connection
- Characteristic read/write/notify operations  
- PTY bridge for serial device emulation
- Homebrew distribution with CI/CD via GitHub Actions

## Tech Stack
- **Language**: Go 1.25.1
- **Core BLE Library**: `github.com/go-ble/ble` v0.0.0-20240122180141-8c5522f54333
- **CLI Framework**: `github.com/spf13/cobra` v1.10.1
- **Logging**: `github.com/sirupsen/logrus` v1.9.3
- **Testing**: `github.com/stretchr/testify` v1.11.1
- **PTY Support**: `github.com/creack/pty` v1.1.24
- **Lua Integration**: `github.com/aarzilli/golua` v0.0.0-20250217091409-248753f411c4 (for scripting)
- **Platform**: macOS 12+ (Core Bluetooth framework)

## Project Structure
```
blecli/
├── cmd/               # CLI entry point and command definitions (empty currently)
├── pkg/               # Core packages
│   ├── ble/          # BLE operations (scanner, bridge, inspector)
│   │   └── internal/ # Internal utilities (buffer, BLE API, Lua engine)
│   ├── connection/   # Connection management
│   ├── config/       # Configuration and settings
│   └── device/       # Device modeling and state tracking
├── go.mod/go.sum     # Go module dependencies
├── Makefile          # Build and development tasks
├── CLAUDE.md         # Project documentation and guidelines
└── LICENSE           # MIT License
```

## Implementation Status
- ✅ **BLE Scanning**: Fully implemented with filtering, watch mode, multiple output formats
- ✅ **Device Management**: Complete device modeling and state tracking  
- ✅ **CLI Framework**: Cobra-based CLI with comprehensive flags and help
- ✅ **Configuration**: Structured logging and configuration management
- ✅ **Testing**: Comprehensive test suite with mocking and benchmarks (48.4% overall coverage)
- ⏳ **Connection Management**: Not yet implemented
- ⏳ **Characteristic Operations**: Not yet implemented  
- ⏳ **PTY Bridge**: Not yet implemented

## Key Features
- Uses LuaJIT for maximum performance (CGO builds)
- Comprehensive Makefile with build, test, lint, format, and benchmark targets
- Supports filtering by device name, service UUID, RSSI
- Mock-based testing infrastructure for BLE operations
- Structured JSON output for programmatic usage
- Cross-platform potential (Linux/Windows) via go-ble library