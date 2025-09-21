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

	t.Run("Multiple options", func(t *testing.T) {
		ja := NewJSONAsserter(t).WithOptions(
			WithAllowPresencePlaceholder(false),
			WithIgnoreExtraKeys(false),
			WithNilToEmptyArray(true), // explicitly set to true
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
