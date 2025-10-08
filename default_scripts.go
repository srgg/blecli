package blecli

import _ "embed"

// DefaultInspectLuaScript contains the embedded inspect.lua script
//
//go:embed examples/inspect.lua
var DefaultInspectLuaScript string

// BridgeHeaderLuaScript contains the embedded bridge-header.lua script
//
//go:embed examples/bridge-header.lua
var BridgeHeaderLuaScript string

// DefaultBridgeLuaScript contains the embedded bridge.lua script
//
//go:embed examples/bridge.lua
var DefaultBridgeLuaScript string
