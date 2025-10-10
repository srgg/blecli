package blecli

import _ "embed"

// DefaultInspectLuaScript contains the embedded inspect.lua script
//
//go:embed examples/inspect.lua
var DefaultInspectLuaScript string

// DefaultBridgeLuaScript contains the embedded bridge.lua script
//
//go:embed examples/bridge.lua
var DefaultBridgeLuaScript string

// BlimLuaScript contains the embedded blim.lua wrapper script
// This provides the CGO-like Lua wrapper for BLE API
//
//go:embed examples/lib/blim.lua
var BlimLuaScript string
