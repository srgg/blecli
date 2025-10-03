package blecli

import _ "embed"

// InspectLuaScript contains the embedded inspect.lua script
//
//go:embed examples/inspect.lua
var InspectLuaScript string
