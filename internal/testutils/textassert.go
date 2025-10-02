package testutils

import (
	"fmt"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/mcuadros/go-defaults"
)

// TestingT is an interface that matches the methods we need from testing.T
type TestingT interface {
	Errorf(format string, args ...interface{})
}

type TextAssertOptions struct {
	IgnoreLeadingWhitespace  bool `default:"false"`
	IgnoreTrailingWhitespace bool `default:"false"`
	IgnoreEmptyLines         bool `default:"false"`
	TrimSpace                bool `default:"false"`
	EnableColors             bool `default:"false"`
}

// TextOption is a functional option for configuring TextAsserter
type TextOption func(*TextAssertOptions)

type TextAsserter struct {
	t       TestingT
	options TextAssertOptions
}

// NewTextAsserter creates a new TextAsserter with default options
func NewTextAsserter(t *testing.T) *TextAsserter {
	return NewTextAsserterWithInterface(t)
}

// NewTextAsserterWithInterface creates a new TextAsserter with default options using the TestingT interface
func NewTextAsserterWithInterface(t TestingT) *TextAsserter {
	opts := TextAssertOptions{}
	defaults.SetDefaults(&opts)
	return &TextAsserter{
		t:       t,
		options: opts,
	}
}

// WithOptions applies functional options to the TextAsserter
func (ta *TextAsserter) WithOptions(opts ...TextOption) *TextAsserter {
	for _, opt := range opts {
		opt(&ta.options)
	}
	return ta
}

// WithOptionsStruct method for backward compatibility
func (ta *TextAsserter) WithOptionsStruct(opts TextAssertOptions) *TextAsserter {
	ta.options.IgnoreLeadingWhitespace = opts.IgnoreLeadingWhitespace
	ta.options.IgnoreTrailingWhitespace = opts.IgnoreTrailingWhitespace
	ta.options.IgnoreEmptyLines = opts.IgnoreEmptyLines
	ta.options.TrimSpace = opts.TrimSpace
	ta.options.EnableColors = opts.EnableColors
	return ta
}

// GetOptions returns a copy of the current options (for testing)
func (ta *TextAsserter) GetOptions() TextAssertOptions {
	return ta.options
}

// Assert compares actual text against expected text
func (ta *TextAsserter) Assert(actual, expected string) {
	diff := ta.diff(actual, expected)
	if diff != "" {
		ta.t.Errorf("Text assertion failed:\n%s", diff)
	}
}

func (ta *TextAsserter) diff(actual, expected string) string {
	// Apply normalization based on options
	normalizedActual := ta.normalize(actual)
	normalizedExpected := ta.normalize(expected)

	// Check if texts are identical after normalization
	if normalizedActual == normalizedExpected {
		return ""
	}

	// Use gotextdiff to generate unified diff
	edits := myers.ComputeEdits("", normalizedExpected, normalizedActual)
	unified := gotextdiff.ToUnified("expected", "actual", normalizedExpected, edits)

	// Convert to string and apply colors to the unified diff output
	colorized := ta.colorizeUnifiedDiff(fmt.Sprint(unified))

	return fmt.Sprintf("Text assertion failed - unified diff:\n%s", colorized)
}

// colorizeUnifiedDiff applies colors to unified diff output
func (ta *TextAsserter) colorizeUnifiedDiff(diff string) string {
	// If colors are disabled, return the diff as-is
	if !ta.options.EnableColors {
		return diff
	}

	lines := strings.Split(diff, "\n")
	var colorized []string

	// Define colors and force enable them for test environments
	red := color.New(color.FgRed)
	red.EnableColor()
	green := color.New(color.FgGreen)
	green.EnableColor()
	cyan := color.New(color.FgCyan)
	cyan.EnableColor()
	yellow := color.New(color.FgYellow)
	yellow.EnableColor()

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++"):
			// File headers in yellow
			colorized = append(colorized, yellow.Sprint(line))
		case strings.HasPrefix(line, "@@"):
			// Hunk headers in cyan
			colorized = append(colorized, cyan.Sprint(line))
		case strings.HasPrefix(line, "-"):
			// Deletions in red - highlight whitespace
			colorized = append(colorized, red.Sprint(ta.highlightWhitespace(line)))
		case strings.HasPrefix(line, "+"):
			// Additions in green - highlight whitespace
			colorized = append(colorized, green.Sprint(ta.highlightWhitespace(line)))
		default:
			// Context lines remain normal
			colorized = append(colorized, line)
		}
	}

	return strings.Join(colorized, "\n")
}

// highlightWhitespace makes whitespace visible by replacing spaces and tabs with visible characters
func (ta *TextAsserter) highlightWhitespace(line string) string {
	if !ta.options.EnableColors {
		return line
	}

	// Replace spaces with middle dot (·) and tabs with arrow (→)
	result := strings.ReplaceAll(line, " ", "·")
	result = strings.ReplaceAll(result, "\t", "→")

	// Also show trailing newlines if present
	if strings.HasSuffix(line, "\n") && !strings.HasSuffix(result, "\n") {
		result += "¬"
	}

	return result
}

func (ta *TextAsserter) normalize(text string) string {
	if ta.options.TrimSpace {
		text = strings.TrimSpace(text)
	}

	lines := strings.Split(text, "\n")

	// Apply line-by-line transformations
	var result []string
	for _, line := range lines {
		if ta.options.IgnoreEmptyLines && strings.TrimSpace(line) == "" {
			continue
		}

		if ta.options.IgnoreLeadingWhitespace {
			line = strings.TrimLeft(line, " \t")
		}

		if ta.options.IgnoreTrailingWhitespace {
			line = strings.TrimRight(line, " \t")
		}

		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// Functional option constructors

// WithIgnoreLeadingWhitespace sets whether to ignore leading whitespace on each line
func WithIgnoreLeadingWhitespace(ignore bool) TextOption {
	return func(opts *TextAssertOptions) {
		opts.IgnoreLeadingWhitespace = ignore
	}
}

// WithIgnoreTrailingWhitespace sets whether to ignore trailing whitespace on each line
func WithIgnoreTrailingWhitespace(ignore bool) TextOption {
	return func(opts *TextAssertOptions) {
		opts.IgnoreTrailingWhitespace = ignore
	}
}

// WithIgnoreEmptyLines sets whether to ignore empty lines
func WithIgnoreEmptyLines(ignore bool) TextOption {
	return func(opts *TextAssertOptions) {
		opts.IgnoreEmptyLines = ignore
	}
}

// WithTrimSpace sets whether to trim leading and trailing whitespace from entire text
func WithTrimSpace(trim bool) TextOption {
	return func(opts *TextAssertOptions) {
		opts.TrimSpace = trim
	}
}

// WithEnableColors sets whether to enable colored diff output
func WithEnableColors(enable bool) TextOption {
	return func(opts *TextAssertOptions) {
		opts.EnableColors = enable
	}
}
