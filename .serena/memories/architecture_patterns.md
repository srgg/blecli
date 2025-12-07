# Architecture and Design Patterns

## Core Architecture Principles

### Package Structure
- **cmd/blecli**: CLI entry point (Cobra commands)
- **pkg/**: Core business logic packages
- **pkg/ble**: BLE operations with internal utilities
- **pkg/device**: Device modeling and state management
- **pkg/config**: Configuration and logging setup
- **pkg/connection**: Connection management (future)

### Design Patterns Used

#### Factory Pattern
```go
type DeviceFactory func(Advertisement) *Device
```
- Used for device creation from BLE advertisements
- Allows for flexible device instantiation strategies

#### Observer Pattern
- Scanner handles BLE advertisements and notifies observers
- Callback-based architecture for real-time updates

#### Builder Pattern
- ScanOptions for configurable scanning behavior
- DefaultScanOptions() provides sensible defaults

#### Interface Segregation
- Small, focused interfaces for BLE operations
- Mock-friendly design for testing

### BLE Operations Flow
1. **Discovery**: Scanner.Scan() for advertising peripherals
2. **Filtering**: shouldIncludeDevice() applies user filters
3. **State Management**: Device.Update() handles advertisement changes
4. **Connection**: (Future) Establish GATT connections
5. **Operations**: (Future) Read/write/notify on characteristics
6. **PTY Bridge**: (Future) Serial device emulation

### Error Handling Strategy
- Return errors from all fallible operations
- Use error wrapping for context
- Structured logging for debugging
- No panics in normal operation

### Testing Architecture
- **Unit Tests**: Isolated component testing
- **Mock Objects**: testify/mock for external dependencies
- **Table-Driven Tests**: Multiple scenarios per test
- **Benchmarks**: Performance validation
- **Race Detection**: Concurrency safety

### Performance Considerations
- **LuaJIT Integration**: High-performance scripting
- **CGO Builds**: Native library integration
- **Memory Management**: Careful slice/map handling
- **Goroutine Safety**: Proper synchronization

### Platform Abstraction
- **go-ble Library**: Cross-platform BLE abstraction
- **Core Bluetooth**: macOS native integration
- **Future Support**: Linux (BlueZ), Windows (WinBLE)

### Configuration Management
- Structured configuration objects
- Environment-aware logging levels
- CLI flag integration with Cobra
- JSON serialization for data exchange

### Logging Strategy
- **Structured Logging**: logrus with fields
- **Levels**: Debug, Info, Warn, Error appropriately used
- **Context**: Include relevant operational context
- **Performance**: Avoid expensive logging in hot paths