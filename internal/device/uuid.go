package device

import "strings"

// NormalizeUUID converts a UUID string to the internal BLE library format (lowercase, no dashes)
// Handles both standard UUID format (with dashes) and already normalized format (without dashes)
func NormalizeUUID(uuid string) string {
	return strings.ToLower(strings.ReplaceAll(uuid, "-", ""))
}

// NormalizeUUIDs normalizes a slice of UUID strings to internal format
func NormalizeUUIDs(uuids []string) []string {
	normalized := make([]string, len(uuids))
	for i, uuid := range uuids {
		normalized[i] = NormalizeUUID(uuid)
	}
	return normalized
}
