package testutils

import (
	"testing"
)

func TestJSONAsserter_DefaultOptions(t *testing.T) {
	ja := NewJSONAsserter(t)
	opts := ja.GetOptions()

	if !opts.IgnoreExtraKeys {
		t.Error("IgnoreExtraKeys should default to true")
	}
	if !opts.NilToEmptyArray {
		t.Error("NilToEmptyArray should default to true")
	}
	if !opts.AllowPresencePlaceholder {
		t.Error("AllowPresencePlaceholder should default to true")
	}
	if opts.CompareOnlyExpectedKeys {
		t.Error("CompareOnlyExpectedKeys should default to false")
	}
	if len(opts.IgnoredFields) != 0 {
		t.Error("IgnoredFields should default to empty slice")
	}
}

func TestJSONAsserter_FunctionalOptions(t *testing.T) {
	t.Run("WithAllowPresencePlaceholder false", func(t *testing.T) {
		ja := NewJSONAsserter(t).WithOptions(
			WithAllowPresencePlaceholder(false),
		)
		opts := ja.GetOptions()

		if opts.AllowPresencePlaceholder {
			t.Error("AllowPresencePlaceholder should be false when explicitly set")
		}
		// Other options should remain default
		if !opts.IgnoreExtraKeys {
			t.Error("IgnoreExtraKeys should remain true from defaults")
		}
		if !opts.NilToEmptyArray {
			t.Error("NilToEmptyArray should remain true from defaults")
		}
	})

	t.Run("WithIgnoreExtraKeys false", func(t *testing.T) {
		ja := NewJSONAsserter(t).WithOptions(
			WithIgnoreExtraKeys(false),
		)
		opts := ja.GetOptions()

		if opts.IgnoreExtraKeys {
			t.Error("IgnoreExtraKeys should be false when explicitly set")
		}
		// Other options should remain default
		if !opts.AllowPresencePlaceholder {
			t.Error("AllowPresencePlaceholder should remain true from defaults")
		}
		if !opts.NilToEmptyArray {
			t.Error("NilToEmptyArray should remain true from defaults")
		}
	})

	t.Run("WithNilToEmptyArray false", func(t *testing.T) {
		ja := NewJSONAsserter(t).WithOptions(
			WithNilToEmptyArray(false),
		)
		opts := ja.GetOptions()

		if opts.NilToEmptyArray {
			t.Error("NilToEmptyArray should be false when explicitly set")
		}
		// Other options should remain default
		if !opts.IgnoreExtraKeys {
			t.Error("IgnoreExtraKeys should remain true from defaults")
		}
		if !opts.AllowPresencePlaceholder {
			t.Error("AllowPresencePlaceholder should remain true from defaults")
		}
	})

	t.Run("WithCompareOnlyExpectedKeys true", func(t *testing.T) {
		ja := NewJSONAsserter(t).WithOptions(
			WithCompareOnlyExpectedKeys(true),
		)
		opts := ja.GetOptions()

		if !opts.CompareOnlyExpectedKeys {
			t.Error("CompareOnlyExpectedKeys should be true when explicitly set")
		}
		// Other options should remain default
		if !opts.IgnoreExtraKeys {
			t.Error("IgnoreExtraKeys should remain true from defaults")
		}
		if !opts.AllowPresencePlaceholder {
			t.Error("AllowPresencePlaceholder should remain true from defaults")
		}
		if !opts.NilToEmptyArray {
			t.Error("NilToEmptyArray should remain true from defaults")
		}
	})

	t.Run("Multiple options", func(t *testing.T) {
		ja := NewJSONAsserter(t).WithOptions(
			WithAllowPresencePlaceholder(false),
			WithIgnoreExtraKeys(false),
			WithNilToEmptyArray(true), // explicitly set to true
			WithCompareOnlyExpectedKeys(true),
		)
		opts := ja.GetOptions()

		if opts.AllowPresencePlaceholder {
			t.Error("AllowPresencePlaceholder should be false")
		}
		if opts.IgnoreExtraKeys {
			t.Error("IgnoreExtraKeys should be false")
		}
		if !opts.NilToEmptyArray {
			t.Error("NilToEmptyArray should be true")
		}
		if !opts.CompareOnlyExpectedKeys {
			t.Error("CompareOnlyExpectedKeys should be true")
		}
	})
}

func TestJSONAsserter_LegacyStructOptions(t *testing.T) {
	ja := NewJSONAsserter(t).WithOptionsStruct(JSONAssertOptions{
		AllowPresencePlaceholder: true,
		NilToEmptyArray:          true,
		IgnoreExtraKeys:          false, // override default
	})
	opts := ja.GetOptions()

	if !opts.AllowPresencePlaceholder {
		t.Error("AllowPresencePlaceholder should be true")
	}
	if !opts.NilToEmptyArray {
		t.Error("NilToEmptyArray should be true")
	}
	if opts.IgnoreExtraKeys {
		t.Error("IgnoreExtraKeys should be false when explicitly set")
	}
}

func TestJSONAsserter_PresencePlaceholder(t *testing.T) {
	t.Run("allows presence placeholder when enabled", func(t *testing.T) {
		// Create a test that won't fail
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithAllowPresencePlaceholder(true),
		)

		actualJSON := `{"id": "123", "timestamp": 1758348286}`
		expectedJSON := `{"id": "123", "timestamp": "<<PRESENCE>>"}`

		// This should not produce a diff since placeholder is allowed
		diff := ja.diff(actualJSON, expectedJSON)
		if diff != "" {
			t.Errorf("Expected no diff with presence placeholder enabled, got: %s", diff)
		}
	})

	t.Run("rejects presence placeholder when disabled", func(t *testing.T) {
		// Create a test that should fail
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithAllowPresencePlaceholder(false),
		)

		actualJSON := `{"id": "123", "timestamp": 1758348286}`
		expectedJSON := `{"id": "123", "timestamp": "<<PRESENCE>>"}`

		// This should produce a diff since placeholder is not allowed
		diff := ja.diff(actualJSON, expectedJSON)
		if diff == "" {
			t.Error("Expected diff with presence placeholder disabled, got no diff")
		}
		if !containsString(diff, "<<PRESENCE>>") {
			t.Errorf("Expected diff to contain <<PRESENCE>>, got: %s", diff)
		}
	})
}

func TestJSONAsserter_IgnoreExtraKeys(t *testing.T) {
	t.Run("ignores extra keys when enabled", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithIgnoreExtraKeys(true),
		)

		actualJSON := `{"id": "123", "name": "test", "extra": "value"}`
		expectedJSON := `{"id": "123", "name": "test"}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff != "" {
			t.Errorf("Expected no diff with IgnoreExtraKeys enabled, got: %s", diff)
		}
	})

	t.Run("detects extra keys when disabled", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithIgnoreExtraKeys(false),
		)

		actualJSON := `{"id": "123", "name": "test", "extra": "value"}`
		expectedJSON := `{"id": "123", "name": "test"}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff == "" {
			t.Error("Expected diff with IgnoreExtraKeys disabled, got no diff")
		}
	})
}

func TestJSONAsserter_ComplexScenarios(t *testing.T) {
	t.Run("complex object with all features", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithAllowPresencePlaceholder(true),
			WithIgnoreExtraKeys(true),
			WithNilToEmptyArray(true),
		)

		actualJSON := `{
			"id": "device123",
			"name": "Test Device",
			"timestamp": 1758348286,
			"services": null,
			"extra_field": "should_be_ignored"
		}`

		expectedJSON := `{
			"id": "device123",
			"name": "Test Device",
			"timestamp": "<<PRESENCE>>",
			"services": []
		}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff != "" {
			t.Errorf("Expected no diff with all features enabled, got: %s", diff)
		}
	})

	t.Run("strict comparison with all features disabled", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithAllowPresencePlaceholder(false),
			WithIgnoreExtraKeys(false),
			WithNilToEmptyArray(false),
		)

		actualJSON := `{
			"id": "device123",
			"name": "Test Device",
			"timestamp": 1758348286,
			"services": null,
			"extra_field": "present"
		}`

		expectedJSON := `{
			"id": "device123",
			"name": "Test Device",
			"timestamp": "<<PRESENCE>>",
			"services": []
		}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff == "" {
			t.Error("Expected diff with all features disabled, got no diff")
		}
		// Should detect presence placeholder, extra key, and null vs array difference
		if !containsString(diff, "<<PRESENCE>>") {
			t.Error("Expected diff to contain presence placeholder mismatch")
		}
	})
}

func TestJSONAsserter_InvalidJSON(t *testing.T) {
	ja := NewJSONAsserter(&testing.T{})

	t.Run("invalid expected JSON", func(t *testing.T) {
		actualJSON := `{"valid": "json"}`
		expectedJSON := `{"invalid": json}` // missing quotes

		diff := ja.diff(actualJSON, expectedJSON)
		if !containsString(diff, "invalid expected JSON") {
			t.Errorf("Expected error about invalid expected JSON, got: %s", diff)
		}
	})

	t.Run("invalid actual JSON", func(t *testing.T) {
		actualJSON := `{"invalid": json}` // missing quotes
		expectedJSON := `{"valid": "json"}`

		diff := ja.diff(actualJSON, expectedJSON)
		if !containsString(diff, "invalid actual JSON") {
			t.Errorf("Expected error about invalid actual JSON, got: %s", diff)
		}
	})
}

// Helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			(len(s) > len(substr) &&
				(s[:len(substr)] == substr ||
					s[len(s)-len(substr):] == substr ||
					indexOfString(s, substr) >= 0)))
}

// Simple string search function
func indexOfString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func TestJSONAsserter_CompareOnlyExpectedKeys(t *testing.T) {
	t.Run("compares only expected keys when enabled", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithCompareOnlyExpectedKeys(true),
			WithIgnoreExtraKeys(false), // disable to ensure CompareOnlyExpectedKeys takes precedence
		)

		actualJSON := `{"id": "123", "name": "test", "extra": "value", "another_extra": 42}`
		expectedJSON := `{"id": "123", "name": "test"}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff != "" {
			t.Errorf("Expected no diff with CompareOnlyExpectedKeys enabled, got: %s", diff)
		}
	})

	t.Run("detects differences in expected keys when enabled", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithCompareOnlyExpectedKeys(true),
		)

		actualJSON := `{"id": "123", "name": "wrong", "extra": "value"}`
		expectedJSON := `{"id": "123", "name": "test"}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff == "" {
			t.Error("Expected diff for mismatched expected key values, got no diff")
		}
		if !containsString(diff, "name") {
			t.Errorf("Expected diff to mention 'name' field, got: %s", diff)
		}
	})

	t.Run("handles nested objects with CompareOnlyExpectedKeys", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithCompareOnlyExpectedKeys(true),
		)

		actualJSON := `{
			"device": {
				"id": "123",
				"name": "test",
				"extra_nested": "ignored"
			},
			"extra_top_level": "ignored"
		}`
		expectedJSON := `{
			"device": {
				"id": "123",
				"name": "test"
			}
		}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff != "" {
			t.Errorf("Expected no diff with nested CompareOnlyExpectedKeys, got: %s", diff)
		}
	})

	t.Run("works with arrays and CompareOnlyExpectedKeys", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithCompareOnlyExpectedKeys(true),
		)

		actualJSON := `{
			"devices": [
				{"id": "1", "name": "device1", "extra": "ignored"},
				{"id": "2", "name": "device2", "extra": "ignored"}
			],
			"extra_field": "ignored"
		}`
		expectedJSON := `{
			"devices": [
				{"id": "1", "name": "device1"},
				{"id": "2", "name": "device2"}
			]
		}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff != "" {
			t.Errorf("Expected no diff with array CompareOnlyExpectedKeys, got: %s", diff)
		}
	})

	t.Run("combines with other options", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithCompareOnlyExpectedKeys(true),
			WithAllowPresencePlaceholder(true),
			WithNilToEmptyArray(true),
		)

		actualJSON := `{
			"id": "123",
			"timestamp": 1758348286,
			"services": null,
			"extra_field": "ignored"
		}`
		expectedJSON := `{
			"id": "123",
			"timestamp": "<<PRESENCE>>",
			"services": []
		}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff != "" {
			t.Errorf("Expected no diff with combined options, got: %s", diff)
		}
	})

	t.Run("disabled by default (standard behavior)", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithIgnoreExtraKeys(false), // ensure extra keys cause failure
		)

		actualJSON := `{"id": "123", "name": "test", "extra": "value"}`
		expectedJSON := `{"id": "123", "name": "test"}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff == "" {
			t.Error("Expected diff with CompareOnlyExpectedKeys disabled (default), got no diff")
		}
	})
}

// Test case for NilToEmptyArray behavior
func TestJSONAsserter_NilToEmptyArrayBehavior(t *testing.T) {
	t.Run("If the expected value is null, the actual value remains null, regardless of NilToEmptyArray", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}) // default options with NilToEmptyArray=true

		actualJSON := `{"null_value": null}`
		expectedJSON := `{"null_value": null}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff != "" {
			t.Errorf("null should equal null, got diff: %s", diff)
		}
	})

	t.Run("When NilToEmptyArray is enabled, a null actual value will be normalized to an empty array if the expected value is an empty array", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}) // default options with NilToEmptyArray=true

		actualJSON := `{"null_value": null}`
		expectedJSON := `{"null_value": []}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff != "" {
			t.Errorf("null should be normalized to [] when NilToEmptyArray=true, got diff: %s", diff)
		}
	})

	t.Run("When NilToEmptyArray is disabled, a null actual value should remain distinct from an empty array expected value", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithNilToEmptyArray(false),
		)

		actualJSON := `{"null_value": null}`
		expectedJSON := `{"null_value": []}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff == "" {
			t.Error("null should NOT equal [] when NilToEmptyArray=false")
		}
	})
}

func TestJSONAsserter_IgnoredFields(t *testing.T) {
	t.Run("WithIgnoredFields sets fields correctly", func(t *testing.T) {
		ja := NewJSONAsserter(t).WithOptions(
			WithIgnoredFields("timestamp", "debug_info"),
		)
		opts := ja.GetOptions()

		expectedFields := []string{"timestamp", "debug_info"}
		if len(opts.IgnoredFields) != len(expectedFields) {
			t.Errorf("Expected %d ignored fields, got %d", len(expectedFields), len(opts.IgnoredFields))
		}
		for i, field := range expectedFields {
			if i >= len(opts.IgnoredFields) || opts.IgnoredFields[i] != field {
				t.Errorf("Expected ignored field %s at index %d, got %v", field, i, opts.IgnoredFields)
			}
		}
	})

	t.Run("ignores specified fields at top level", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithIgnoredFields("timestamp", "debug_info"),
		)

		actualJSON := `{
			"id": "123",
			"name": "test",
			"timestamp": 1758348286,
			"debug_info": "some debug data"
		}`
		expectedJSON := `{
			"id": "123",
			"name": "test",
			"timestamp": 9999999999,
			"debug_info": "different debug data"
		}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff != "" {
			t.Errorf("Expected no diff with ignored fields, got: %s", diff)
		}
	})

	t.Run("still detects differences in non-ignored fields", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithIgnoredFields("timestamp"),
		)

		actualJSON := `{
			"id": "123",
			"name": "wrong",
			"timestamp": 1758348286
		}`
		expectedJSON := `{
			"id": "123",
			"name": "test",
			"timestamp": 9999999999
		}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff == "" {
			t.Error("Expected diff for non-ignored field differences, got no diff")
		}
		if !containsString(diff, "name") {
			t.Errorf("Expected diff to mention 'name' field, got: %s", diff)
		}
	})

	t.Run("ignores fields in nested objects", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithIgnoredFields("timestamp", "debug_info"),
		)

		actualJSON := `{
			"device": {
				"id": "123",
				"name": "test",
				"timestamp": 1758348286,
				"debug_info": "nested debug"
			},
			"timestamp": 9876543210,
			"status": "active"
		}`
		expectedJSON := `{
			"device": {
				"id": "123",
				"name": "test",
				"timestamp": 1111111111,
				"debug_info": "different nested debug"
			},
			"timestamp": 5555555555,
			"status": "active"
		}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff != "" {
			t.Errorf("Expected no diff with ignored nested fields, got: %s", diff)
		}
	})

	t.Run("ignores fields in arrays of objects", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithIgnoredFields("timestamp", "debug_info"),
		)

		actualJSON := `{
			"devices": [
				{
					"id": "1",
					"name": "device1",
					"timestamp": 1758348286,
					"debug_info": "debug1"
				},
				{
					"id": "2",
					"name": "device2",
					"timestamp": 1758348287,
					"debug_info": "debug2"
				}
			]
		}`
		expectedJSON := `{
			"devices": [
				{
					"id": "1",
					"name": "device1",
					"timestamp": 9999999999,
					"debug_info": "different debug1"
				},
				{
					"id": "2",
					"name": "device2",
					"timestamp": 8888888888,
					"debug_info": "different debug2"
				}
			]
		}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff != "" {
			t.Errorf("Expected no diff with ignored array fields, got: %s", diff)
		}
	})

	t.Run("combines with other options", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithIgnoredFields("timestamp"),
			WithAllowPresencePlaceholder(true),
			WithNilToEmptyArray(true),
			WithIgnoreExtraKeys(true),
		)

		actualJSON := `{
			"id": "123",
			"name": "test",
			"timestamp": 1758348286,
			"services": null,
			"extra_field": "ignored",
			"other_timestamp": 1234567890
		}`
		expectedJSON := `{
			"id": "123",
			"name": "test",
			"timestamp": 9999999999,
			"services": [],
			"other_timestamp": "<<PRESENCE>>"
		}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff != "" {
			t.Errorf("Expected no diff with combined options and ignored fields, got: %s", diff)
		}
	})

	t.Run("works when only some fields need to be ignored", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithIgnoredFields("created_at"),
		)

		actualJSON := `{
			"id": "123",
			"name": "test",
			"created_at": "2023-01-01T00:00:00Z",
			"updated_at": "2023-01-02T00:00:00Z"
		}`
		expectedJSON := `{
			"id": "123",
			"name": "test",
			"created_at": "2024-01-01T00:00:00Z",
			"updated_at": "2023-01-02T00:00:00Z"
		}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff != "" {
			t.Errorf("Expected no diff with created_at ignored, got: %s", diff)
		}
	})

	t.Run("empty ignored fields list works normally", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithIgnoredFields(), // empty list
		)

		actualJSON := `{"id": "123", "name": "test"}`
		expectedJSON := `{"id": "123", "name": "test"}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff != "" {
			t.Errorf("Expected no diff with empty ignored fields, got: %s", diff)
		}
	})

	t.Run("detects diff when ignored fields are empty and JSONs differ", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithIgnoredFields(), // empty list
		)

		actualJSON := `{"id": "123", "name": "wrong"}`
		expectedJSON := `{"id": "123", "name": "test"}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff == "" {
			t.Error("Expected diff with empty ignored fields and different values, got no diff")
		}
	})
}

func TestJSONAsserter_IgnoreArrayOrder(t *testing.T) {
	t.Run("arrays with same elements in different order match when enabled", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithIgnoreArrayOrder(true),
		)

		actualJSON := `{"items": [3, 1, 2]}`
		expectedJSON := `{"items": [1, 2, 3]}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff != "" {
			t.Errorf("Expected no diff with IgnoreArrayOrder enabled, got: %s", diff)
		}
	})

	t.Run("arrays with same elements in different order fail when disabled", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithIgnoreArrayOrder(false),
		)

		actualJSON := `{"items": [3, 1, 2]}`
		expectedJSON := `{"items": [1, 2, 3]}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff == "" {
			t.Error("Expected diff with IgnoreArrayOrder disabled, got no diff")
		}
	})

	t.Run("arrays with different elements fail regardless of option", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithIgnoreArrayOrder(true),
		)

		actualJSON := `{"items": [1, 2, 3]}`
		expectedJSON := `{"items": [1, 2, 4]}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff == "" {
			t.Error("Expected diff for different array elements even with IgnoreArrayOrder enabled, got no diff")
		}
	})

	t.Run("nested arrays are sorted correctly", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithIgnoreArrayOrder(true),
		)

		actualJSON := `{"data": [{"values": [3, 2, 1]}, {"values": [6, 5, 4]}]}`
		expectedJSON := `{"data": [{"values": [1, 2, 3]}, {"values": [4, 5, 6]}]}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff != "" {
			t.Errorf("Expected no diff with nested arrays sorted, got: %s", diff)
		}
	})

	t.Run("objects in arrays sorted by JSON representation", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithIgnoreArrayOrder(true),
		)

		actualJSON := `{"devices": [{"id": "2", "name": "b"}, {"id": "1", "name": "a"}]}`
		expectedJSON := `{"devices": [{"id": "1", "name": "a"}, {"id": "2", "name": "b"}]}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff != "" {
			t.Errorf("Expected no diff with object arrays sorted, got: %s", diff)
		}
	})

	t.Run("mixed nested structures with arrays and objects", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithIgnoreArrayOrder(true),
		)

		actualJSON := `{
			"services": [
				{"id": "2", "chars": ["d", "c"]},
				{"id": "1", "chars": ["b", "a"]}
			]
		}`
		expectedJSON := `{
			"services": [
				{"id": "1", "chars": ["a", "b"]},
				{"id": "2", "chars": ["c", "d"]}
			]
		}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff != "" {
			t.Errorf("Expected no diff with mixed nested structures, got: %s", diff)
		}
	})

	t.Run("empty arrays match regardless of order option", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithIgnoreArrayOrder(true),
		)

		actualJSON := `{"items": []}`
		expectedJSON := `{"items": []}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff != "" {
			t.Errorf("Expected no diff for empty arrays, got: %s", diff)
		}
	})

	t.Run("single element arrays match regardless of order", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithIgnoreArrayOrder(true),
		)

		actualJSON := `{"items": [1]}`
		expectedJSON := `{"items": [1]}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff != "" {
			t.Errorf("Expected no diff for single element arrays, got: %s", diff)
		}
	})

	t.Run("combines with IgnoredFields option", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithIgnoreArrayOrder(true),
			WithIgnoredFields("timestamp"),
		)

		actualJSON := `{
			"events": [
				{"id": "2", "timestamp": 2000},
				{"id": "1", "timestamp": 1000}
			]
		}`
		expectedJSON := `{
			"events": [
				{"id": "1", "timestamp": 9999},
				{"id": "2", "timestamp": 8888}
			]
		}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff != "" {
			t.Errorf("Expected no diff with IgnoreArrayOrder + IgnoredFields, got: %s", diff)
		}
	})

	t.Run("BLE notification use case - multiple characteristics", func(t *testing.T) {
		ja := NewJSONAsserter(&testing.T{}).WithOptions(
			WithIgnoreArrayOrder(true),
		)

		// Wrap arrays in an object since jsonassert doesn't support root-level arrays
		actualJSON := `{
			"array": [
				{"record": {"Values": {"6e400003b5a3f393e0a9e50e24dcca9e": "Hello"}}},
				{"record": {"Values": {"2a19": "*"}}},
				{"record": {"Values": {"6e400002b5a3f393e0a9e50e24dcca9e": "World"}}}
			]
		}`
		expectedJSON := `{
			"array": [
				{"record": {"Values": {"6e400003b5a3f393e0a9e50e24dcca9e": "Hello"}}},
				{"record": {"Values": {"6e400002b5a3f393e0a9e50e24dcca9e": "World"}}},
				{"record": {"Values": {"2a19": "*"}}}
			]
		}`

		diff := ja.diff(actualJSON, expectedJSON)
		if diff != "" {
			t.Errorf("Expected no diff for BLE notification case, got: %s", diff)
		}
	})
}
