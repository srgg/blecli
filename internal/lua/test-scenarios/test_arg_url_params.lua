-- Test Script: URL Parameters via arg[] Table
--
-- PURPOSE: This script is used by lua-api-suite-test-test-scenarios.yaml to verify
--          that URL query parameters are correctly parsed and passed to Lua scripts
--          via the arg[] table.
--
-- USAGE: Called from YAML test with URL like:
--        script: "file://internal/lua/test-scenarios/test_arg_url_params.lua?format=json&verbose=true"
--
-- EXPECTED: The runTestCase function should:
--           1. Parse the URL query parameters (format=json, verbose=true)
--           2. Populate the arg[] table with these key-value pairs
--           3. Execute this script with arg["format"] = "json" and arg["verbose"] = "true"
--
-- OUTPUT: Prints each arg table entry in sorted order for consistent test validation

-- Collect all keys for sorting
local keys = {}
for k in pairs(arg) do
    table.insert(keys, k)
end
table.sort(keys)

-- Print each arg in sorted order
for _, key in ipairs(keys) do
    print(string.format("arg[%q] = %q", key, arg[key]))
end