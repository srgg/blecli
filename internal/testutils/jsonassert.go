package testutils

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/mcuadros/go-defaults"
	"github.com/srg/blecli/pkg/device"
	gojsondiff "github.com/yudai/gojsondiff"
	"github.com/yudai/gojsondiff/formatter"
)

func MustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(data)
}

type JSONAssertOptions struct {
	IgnoreExtraKeys          bool `default:"true"`
	NilToEmptyArray          bool `default:"true"`
	AllowPresencePlaceholder bool `default:"true"`
}

// Option is a functional option for configuring JSONAsserter
type Option func(*JSONAssertOptions)

type JSONAsserter struct {
	t       *testing.T
	options JSONAssertOptions
}

// NewJSONAsserter creates a new JSONAsserter with default options
func NewJSONAsserter(t *testing.T) *JSONAsserter {
	opts := JSONAssertOptions{}
	defaults.SetDefaults(&opts)
	return &JSONAsserter{
		t:       t,
		options: opts,
	}
}

// WithOptions applies functional options to the JSONAsserter
func (ja *JSONAsserter) WithOptions(opts ...Option) *JSONAsserter {
	for _, opt := range opts {
		opt(&ja.options)
	}
	return ja
}

// WithOptionsStruct method for backward compatibility
func (ja *JSONAsserter) WithOptionsStruct(opts JSONAssertOptions) *JSONAsserter {
	ja.options.IgnoreExtraKeys = opts.IgnoreExtraKeys
	ja.options.NilToEmptyArray = opts.NilToEmptyArray
	ja.options.AllowPresencePlaceholder = opts.AllowPresencePlaceholder
	return ja
}

// GetOptions returns a copy of the current options (for testing)
func (ja *JSONAsserter) GetOptions() JSONAssertOptions {
	return ja.options
}

// Assert compares actualJSON against expectedJSON
func (ja *JSONAsserter) Assert(actualJSON, expectedJSON string) {
	diff := ja.diff(actualJSON, expectedJSON)
	if diff != "" {
		ja.t.Errorf("JSON assertion failed:\n%s", diff)
	}
}

// Assert compares actual Device against expectedJSON
func (ja *JSONAsserter) AssertDevice(dev device.Device, expectedJSON string) {
	actualJSON := DeviceToJSON(dev)
	ja.Assert(actualJSON, expectedJSON)
}

func (ja *JSONAsserter) diff(actualJSON, expectedJSON string) string {
	// Always unmarshal into fresh copies
	var expected, actual interface{}
	if err := json.Unmarshal([]byte(expectedJSON), &expected); err != nil {
		return fmt.Sprintf("invalid expected JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(actualJSON), &actual); err != nil {
		return fmt.Sprintf("invalid actual JSON: %v", err)
	}

	// Apply placeholder logic only if allowed
	if ja.options.AllowPresencePlaceholder {
		replacePresenceWithActual(expected, actual)
	}

	// Apply other normalization options
	if ja.options.NilToEmptyArray {
		normalizeNilArrays(expected, actual)
	}
	if ja.options.IgnoreExtraKeys {
		pruneExtraKeys(actual, expected)
	}

	// Marshal back to bytes for diff
	expectedBytes, _ := json.Marshal(expected)
	actualBytes, _ := json.Marshal(actual)

	differ := gojsondiff.New()
	diff, err := differ.Compare(expectedBytes, actualBytes)
	if err != nil {
		return fmt.Sprintf("JSON comparison failed: %v", err)
	}

	if !diff.Modified() {
		return ""
	}

	config := formatter.AsciiFormatterConfig{
		ShowArrayIndex: true,
		Coloring:       false,
	}
	f := formatter.NewAsciiFormatter(expected, config)
	diffString, _ := f.Format(diff)

	return diffString
}

// replacePresenceWithActual copies actual values for "<<PRESENCE>>" placeholders
func replacePresenceWithActual(expected, actual interface{}) {
	switch exp := expected.(type) {
	case map[string]interface{}:
		act, ok := actual.(map[string]interface{})
		if !ok {
			return
		}
		for k := range exp {
			if s, ok := exp[k].(string); ok && s == "<<PRESENCE>>" {
				exp[k] = act[k] // copy actual value for comparison
			} else {
				replacePresenceWithActual(exp[k], act[k])
			}
		}
	case []interface{}:
		act, ok := actual.([]interface{})
		if !ok {
			return
		}
		for i := range exp {
			if i < len(act) {
				replacePresenceWithActual(exp[i], act[i])
			}
		}
	}
}

// Normalize nil slices to empty slices, but only when both sides can be normalized
func normalizeNilArrays(expected, actual interface{}) {
	switch exp := expected.(type) {
	case map[string]interface{}:
		act, ok := actual.(map[string]interface{})
		if !ok {
			return
		}
		for k := range exp {
			expVal := exp[k]
			actVal := act[k]

			// Only normalize if both values are nil, or one is nil and the other is empty array
			if shouldNormalize(expVal, actVal) {
				if expVal == nil {
					exp[k] = []interface{}{}
				}
				if actVal == nil {
					act[k] = []interface{}{}
				}
			} else if expVal != nil && actVal != nil {
				// Recursively normalize nested objects
				if s, ok := expVal.(string); !ok || s != "<<PRESENCE>>" {
					normalizeNilArrays(expVal, actVal)
				}
			}
		}
	case []interface{}:
		act, ok := actual.([]interface{})
		if !ok {
			return
		}
		for i := range exp {
			if i < len(act) {
				if shouldNormalize(exp[i], act[i]) {
					if exp[i] == nil {
						exp[i] = []interface{}{}
					}
					if act[i] == nil {
						act[i] = []interface{}{}
					}
				} else if exp[i] != nil && act[i] != nil {
					normalizeNilArrays(exp[i], act[i])
				}
			}
		}
	}
}

// shouldNormalize determines if null values should be converted to empty arrays
// Only normalize when it makes semantic sense (both are nil/empty, or one is nil and other is empty array)
func shouldNormalize(expectedVal, actualVal interface{}) bool {
	// Both are nil - normalize both to []
	if expectedVal == nil && actualVal == nil {
		return true
	}

	// One is nil, other is empty array - normalize the nil one
	if expectedVal == nil {
		if arr, ok := actualVal.([]interface{}); ok && len(arr) == 0 {
			return true
		}
	}
	if actualVal == nil {
		if arr, ok := expectedVal.([]interface{}); ok && len(arr) == 0 {
			return true
		}
	}

	// Don't normalize in other cases (e.g., one is nil, other is non-empty array or different type)
	return false
}

// Remove keys in actual that don’t exist in expected
func pruneExtraKeys(actual, expected interface{}) {
	switch exp := expected.(type) {
	case map[string]interface{}:
		act, ok := actual.(map[string]interface{})
		if !ok {
			return
		}
		for k := range act {
			if _, exists := exp[k]; !exists {
				delete(act, k)
			}
		}
		for k := range exp {
			pruneExtraKeys(act[k], exp[k])
		}
	case []interface{}:
		act, ok := actual.([]interface{})
		if !ok {
			return
		}
		for i := range exp {
			if i < len(act) {
				pruneExtraKeys(act[i], exp[i])
			}
		}
	}
}

// Functional option constructors

// WithIgnoreExtraKeys sets whether to ignore extra keys in actual JSON
func WithIgnoreExtraKeys(ignore bool) Option {
	return func(opts *JSONAssertOptions) {
		opts.IgnoreExtraKeys = ignore
	}
}

// WithNilToEmptyArray sets whether to normalize nil arrays to empty arrays
func WithNilToEmptyArray(normalize bool) Option {
	return func(opts *JSONAssertOptions) {
		opts.NilToEmptyArray = normalize
	}
}

// WithAllowPresencePlaceholder sets whether to allow "<<PRESENCE>>" placeholders
func WithAllowPresencePlaceholder(allow bool) Option {
	return func(opts *JSONAssertOptions) {
		opts.AllowPresencePlaceholder = allow
	}
}
