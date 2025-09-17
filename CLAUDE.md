# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

# BLE CLI Tool (Go / macOS)

## Overview
A macOS CLI tool in Go that provides:
- BLE peripheral scanning and connection
- Characteristic read/write/notify operations
- PTY bridge for serial device emulation
- Homebrew distribution
- CI/CD via GitHub Actions

## Prerequisites
- Go >= 1.21
- macOS 12+
- Xcode Command Line Tools (required for CGO compilation of BLE bindings)
- Terminal app with Bluetooth permissions

## Key Dependencies
- `github.com/go-ble/ble` - Core BLE functionality
- `github.com/spf13/cobra` - CLI framework and command structure
- `github.com/sirupsen/logrus` - Structured logging
- `github.com/stretchr/testify` - Testing assertions and mocks

## Development Commands

Use the provided Makefile for all development tasks:

### Build
```bash
make build           # Build the CLI application
make clean          # Clean build artifacts
```

### Testing
```bash
make test           # Run all tests
make test-race      # Run tests with race detection
make test-coverage  # Generate coverage report (HTML + summary)
make coverage       # Show coverage summary
```

### Benchmarking
```bash
make bench          # Run all benchmarks
make bench-cpu      # Run benchmarks with CPU profiling
make bench-mem      # Run benchmarks with memory profiling
```

### Package-specific Testing
```bash
make test-device    # Test device package
make test-ble       # Test BLE scanner package
make test-config    # Test configuration package
make test-cli       # Test CLI commands
```

### Code Quality
```bash
make lint           # Run linter (golangci-lint or go vet)
make fmt            # Format code
make security       # Run security checks (gosec)
make check          # Full quality check (fmt + lint + test + security)
```

### Development Setup
```bash
make install-tools  # Install development tools
make tidy          # Tidy dependencies
make verify        # Verify dependencies
```

### Run
```bash
./blecli scan                        # Scan for BLE devices
./blecli connect <device-id>         # Connect to device
./blecli notify <device-id> <char>   # Subscribe to notifications
./blecli write <device-id> <char> <data>  # Write to characteristic
./blecli bridge <device-id> <char>   # Create PTY bridge
```

## Architecture

### Core Components
- **cmd/blecli/** - CLI entry point and command definitions
- **pkg/ble/** - BLE operations (scan, connect, read/write/notify)
- **pkg/device/** - Device management and state tracking
- **pkg/config/** - Configuration and settings management

### Current Implementation Status
- ✅ **BLE Scanning**: Fully implemented with filtering, watch mode, multiple output formats
- ✅ **Device Management**: Complete device modeling and state tracking
- ✅ **CLI Framework**: Cobra-based CLI with comprehensive flags and help
- ✅ **Configuration**: Structured logging and configuration management
- ✅ **Testing**: Comprehensive test suite with mocking and benchmarks
- ⏳ **Connection Management**: Not yet implemented
- ⏳ **Characteristic Operations**: Not yet implemented
- ⏳ **PTY Bridge**: Not yet implemented

### BLE Operations Flow
1. **Discovery**: Scan for advertising peripherals
2. **Connection**: Establish GATT connection to selected device
3. **Service Discovery**: Enumerate services and characteristics
4. **Operations**: Read/write/notify on characteristics
5. **PTY Bridge**: Optional serial device emulation via PTY

### PTY Bridge Architecture
The PTY bridge creates a pseudo-terminal that applications can connect to as if it were a serial device, while transparently forwarding data to/from BLE characteristics.

## Testing Infrastructure

### Test Coverage
- **pkg/device**: 100% coverage - Complete device modeling and BLE advertisement handling
- **pkg/config**: 100% coverage - Configuration and logger creation
- **pkg/ble**: 62% coverage - BLE scanning with comprehensive mocking
- **cmd/blecli**: 21.6% coverage - CLI commands (limited by BLE hardware requirements)
- **Overall**: 48.4% total coverage

### Test Features
- **Unit Tests**: Comprehensive testing with table-driven tests
- **Mock Objects**: Full BLE Advertisement and Address mocking
- **Concurrency Tests**: Thread-safety validation with goroutines
- **Integration Tests**: CLI command and flag parsing validation
- **Benchmarks**: Performance testing for critical operations
- **Coverage Reports**: HTML reports generated in `coverage/coverage.html`

### Test Commands
```bash
make test-coverage  # Generate coverage report
open coverage/coverage.html  # View coverage in browser
make bench         # Run performance benchmarks
```

## Platform Support
- **Primary**: macOS (Core Bluetooth framework)
- **Potential**: Linux (BlueZ), Windows (Windows BLE APIs)
- Platform-specific BLE implementations via go-ble library
- **Permissions**: Bluetooth access required for terminal/application

## Distribution
- macOS: Homebrew formula
- Cross-platform: GitHub releases with platform-specific binaries
- Linux: Potential package manager support (apt, yum)
- Windows: Potential package manager support (choco, winget)