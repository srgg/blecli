//go:build test

package testutils

import (
	"fmt"
	"strings"
	"testing"
)

func TestTextAsserter_DefaultOptions(t *testing.T) {
	ta := NewTextAsserter(t)

	// Test default options are set correctly
	opts := ta.GetOptions()
	if opts.IgnoreLeadingWhitespace != false {
		t.Errorf("Expected IgnoreLeadingWhitespace to be false by default, got %v", opts.IgnoreLeadingWhitespace)
	}
	if opts.IgnoreTrailingWhitespace != false {
		t.Errorf("Expected IgnoreTrailingWhitespace to be false by default, got %v", opts.IgnoreTrailingWhitespace)
	}
	if opts.IgnoreEmptyLines != false {
		t.Errorf("Expected IgnoreEmptyLines to be false by default, got %v", opts.IgnoreEmptyLines)
	}
	if opts.TrimSpace != false {
		t.Errorf("Expected TrimSpace to be false by default, got %v", opts.TrimSpace)
	}
}

func TestTextAsserter_FunctionalOptions(t *testing.T) {
	t.Run("WithIgnoreLeadingWhitespace", func(t *testing.T) {
		ta := NewTextAsserter(t).WithOptions(
			WithIgnoreLeadingWhitespace(true),
		)

		opts := ta.GetOptions()
		if !opts.IgnoreLeadingWhitespace {
			t.Error("Expected IgnoreLeadingWhitespace to be true")
		}
		if opts.IgnoreTrailingWhitespace {
			t.Error("Expected IgnoreTrailingWhitespace to remain false")
		}
	})

	t.Run("WithIgnoreTrailingWhitespace", func(t *testing.T) {
		ta := NewTextAsserter(t).WithOptions(
			WithIgnoreTrailingWhitespace(true),
		)

		opts := ta.GetOptions()
		if !opts.IgnoreTrailingWhitespace {
			t.Error("Expected IgnoreTrailingWhitespace to be true")
		}
		if opts.IgnoreLeadingWhitespace {
			t.Error("Expected IgnoreLeadingWhitespace to remain false")
		}
	})

	t.Run("WithIgnoreEmptyLines", func(t *testing.T) {
		ta := NewTextAsserter(t).WithOptions(
			WithIgnoreEmptyLines(true),
		)

		opts := ta.GetOptions()
		if !opts.IgnoreEmptyLines {
			t.Error("Expected IgnoreEmptyLines to be true")
		}
	})

	t.Run("WithTrimSpace", func(t *testing.T) {
		ta := NewTextAsserter(t).WithOptions(
			WithTrimSpace(true),
		)

		opts := ta.GetOptions()
		if !opts.TrimSpace {
			t.Error("Expected TrimSpace to be true")
		}
	})
}

func TestTextAsserter_LegacyStructOptions(t *testing.T) {
	ta := NewTextAsserter(t).WithOptionsStruct(TextAssertOptions{
		IgnoreLeadingWhitespace:  true,
		IgnoreTrailingWhitespace: true,
		IgnoreEmptyLines:         true,
		TrimSpace:                true,
	})

	opts := ta.GetOptions()
	if !opts.IgnoreLeadingWhitespace {
		t.Error("Expected IgnoreLeadingWhitespace to be true")
	}
	if !opts.IgnoreTrailingWhitespace {
		t.Error("Expected IgnoreTrailingWhitespace to be true")
	}
	if !opts.IgnoreEmptyLines {
		t.Error("Expected IgnoreEmptyLines to be true")
	}
	if !opts.TrimSpace {
		t.Error("Expected TrimSpace to be true")
	}
}

func TestTextAsserter_BasicComparison(t *testing.T) {
	t.Run("IdenticalStrings", func(t *testing.T) {
		ta := NewTextAsserter(&testing.T{})
		diff := ta.diff("hello world", "hello world")
		if diff != "" {
			t.Errorf("Expected no diff for identical strings, got: %s", diff)
		}
	})

	t.Run("DifferentStrings", func(t *testing.T) {
		ta := NewTextAsserter(&testing.T{})
		diff := ta.diff("hello world", "hello universe")
		if diff == "" {
			t.Error("Expected diff for different strings")
		}
	})
}

func TestTextAsserter_IgnoreLeadingWhitespace(t *testing.T) {
	t.Run("IgnoreLeadingWhitespace_True", func(t *testing.T) {
		ta := NewTextAsserter(&testing.T{}).WithOptions(
			WithIgnoreLeadingWhitespace(true),
		)

		diff := ta.diff("  hello\n    world", "hello\nworld")
		if diff != "" {
			t.Errorf("Expected no diff when ignoring leading whitespace, got: %s", diff)
		}
	})

	t.Run("IgnoreLeadingWhitespace_False", func(t *testing.T) {
		ta := NewTextAsserter(&testing.T{}).WithOptions(
			WithIgnoreLeadingWhitespace(false),
		)

		diff := ta.diff("  hello\n    world", "hello\nworld")
		if diff == "" {
			t.Error("Expected diff when not ignoring leading whitespace")
		}
	})
}

func TestTextAsserter_IgnoreTrailingWhitespace(t *testing.T) {
	t.Run("IgnoreTrailingWhitespace_True", func(t *testing.T) {
		ta := NewTextAsserter(&testing.T{}).WithOptions(
			WithIgnoreTrailingWhitespace(true),
		)

		diff := ta.diff("hello  \nworld    ", "hello\nworld")
		if diff != "" {
			t.Errorf("Expected no diff when ignoring trailing whitespace, got: %s", diff)
		}
	})

	t.Run("IgnoreTrailingWhitespace_False", func(t *testing.T) {
		ta := NewTextAsserter(&testing.T{}).WithOptions(
			WithIgnoreTrailingWhitespace(false),
		)

		diff := ta.diff("hello  \nworld    ", "hello\nworld")
		if diff == "" {
			t.Error("Expected diff when not ignoring trailing whitespace")
		}
	})
}

func TestTextAsserter_IgnoreEmptyLines(t *testing.T) {
	t.Run("IgnoreEmptyLines_True", func(t *testing.T) {
		ta := NewTextAsserter(&testing.T{}).WithOptions(
			WithIgnoreEmptyLines(true),
		)

		diff := ta.diff("hello\n\nworld\n\n", "hello\nworld")
		if diff != "" {
			t.Errorf("Expected no diff when ignoring empty lines, got: %s", diff)
		}
	})

	t.Run("IgnoreEmptyLines_False", func(t *testing.T) {
		ta := NewTextAsserter(&testing.T{}).WithOptions(
			WithIgnoreEmptyLines(false),
		)

		diff := ta.diff("hello\n\nworld\n\n", "hello\nworld")
		if diff == "" {
			t.Error("Expected diff when not ignoring empty lines")
		}
	})
}

func TestTextAsserter_TrimSpace(t *testing.T) {
	t.Run("TrimSpace_True", func(t *testing.T) {
		ta := NewTextAsserter(&testing.T{}).WithOptions(
			WithTrimSpace(true),
		)

		diff := ta.diff("  hello\nworld  ", "hello\nworld")
		if diff != "" {
			t.Errorf("Expected no diff when trimming space, got: %s", diff)
		}
	})

	t.Run("TrimSpace_False", func(t *testing.T) {
		ta := NewTextAsserter(&testing.T{}).WithOptions(
			WithTrimSpace(false),
		)

		diff := ta.diff("  hello\nworld  ", "hello\nworld")
		if diff == "" {
			t.Error("Expected diff when not trimming space")
		}
	})
}

func TestTextAsserter_ComplexScenarios(t *testing.T) {
	t.Run("AllOptionsEnabled", func(t *testing.T) {
		ta := NewTextAsserter(&testing.T{}).WithOptions(
			WithIgnoreLeadingWhitespace(true),
			WithIgnoreTrailingWhitespace(true),
			WithIgnoreEmptyLines(true),
			WithTrimSpace(true),
		)

		actual := `
		  hello world

		  goodbye universe

		`

		expected := `hello world
goodbye universe`

		diff := ta.diff(actual, expected)
		if diff != "" {
			t.Errorf("Expected no diff with all normalization options, got: %s", diff)
		}
	})

	t.Run("MultilineWithDifferences", func(t *testing.T) {
		ta := NewTextAsserter(&testing.T{}).WithOptions(
			WithIgnoreLeadingWhitespace(true),
			WithIgnoreTrailingWhitespace(true),
		)

		actual := `  line1
  line2
  line3_different  `

		expected := `line1
line2
line3_expected`

		diff := ta.diff(actual, expected)
		if diff == "" {
			t.Error("Expected diff for different content")
		}

		// Verify the diff contains information about the difference
		if !contains(diff, "line3") {
			t.Errorf("Expected diff to mention the differing line, got: %s", diff)
		}
	})
}

func TestTextAsserter_Assert_Failure(t *testing.T) {
	// Use a mock testing.T to capture error messages
	mockT := &mockTestingT{}
	ta := NewTextAsserterWithInterface(mockT)

	ta.Assert("hello", "world")

	if !mockT.errorCalled {
		t.Error("Expected Errorf to be called for failed assertion")
	}

	if !contains(mockT.errorMessage, "Text assertion failed") {
		t.Errorf("Expected error message to contain 'Text assertion failed', got: %s", mockT.errorMessage)
	}
}

func TestTextAsserter_Assert_Success(t *testing.T) {
	// Use a mock testing.T to verify no error is called
	mockT := &mockTestingT{}
	ta := NewTextAsserterWithInterface(mockT)

	ta.Assert("hello", "hello")

	if mockT.errorCalled {
		t.Errorf("Expected no error for successful assertion, got: %s", mockT.errorMessage)
	}
}

// Helper types and functions for testing

type mockTestingT struct {
	errorCalled  bool
	errorMessage string
}

func (m *mockTestingT) Errorf(format string, args ...interface{}) {
	m.errorCalled = true
	m.errorMessage = fmt.Sprintf(format, args...)
}

func contains(str, substr string) bool {
	return strings.Contains(str, substr)
}
