package blecli

import _ "embed"

// DefaultInspectLuaScript contains the embedded inspect.lua script
//
//go:embed examples/inspect.lua
var DefaultInspectLuaScript string

// DefaultBridgeLuaScript contains the embedded inspect.lua script
//
//go:embed examples/bridge.lua
var DefaultBridgeLuaScript string
