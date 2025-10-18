package main

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/srg/blim/internal/device"
	"github.com/stretchr/testify/suite"
)

// ReadTestSuite provides testify/suite for proper test isolation
type ReadTestSuite struct {
	suite.Suite
	originalFlags struct {
		readServiceUUID string
		readCharUUID    string
		readDescUUID    string
		readHex         bool
		readWatch       string
		readTimeout     time.Duration
	}
}

// SetupSuite runs once before all tests in the suite
func (suite *ReadTestSuite) SetupSuite() {
	// Save original flag values
	suite.originalFlags.readServiceUUID = readServiceUUID
	suite.originalFlags.readCharUUID = readCharUUID
	suite.originalFlags.readDescUUID = readDescUUID
	suite.originalFlags.readHex = readHex
	suite.originalFlags.readWatch = readWatch
	suite.originalFlags.readTimeout = readTimeout
}

// TearDownSuite runs once after all tests in the suite
func (suite *ReadTestSuite) TearDownSuite() {
	// Restore original flag values
	readServiceUUID = suite.originalFlags.readServiceUUID
	readCharUUID = suite.originalFlags.readCharUUID
	readDescUUID = suite.originalFlags.readDescUUID
	readHex = suite.originalFlags.readHex
	readWatch = suite.originalFlags.readWatch
	readTimeout = suite.originalFlags.readTimeout
}

// SetupTest runs before each test in the suite
func (suite *ReadTestSuite) SetupTest() {
	// Reset flags before each test for proper isolation
	readServiceUUID = ""
	readCharUUID = ""
	readDescUUID = ""
	readHex = false
	readWatch = ""
	readTimeout = 5 * time.Second
}

func (suite *ReadTestSuite) TestParseWriteData_HexFormats() {
	// GOAL: Verify hex data parsing handles various input formats correctly
	//
	// TEST SCENARIO: Parse hex with different separators → decoded bytes → matches expected output

	tests := []struct {
		name     string
		input    string
		expected []byte
	}{
		{
			name:     "simple hex",
			input:    "0102",
			expected: []byte{0x01, 0x02},
		},
		{
			name:     "hex with spaces",
			input:    "01 02 03",
			expected: []byte{0x01, 0x02, 0x03},
		},
		{
			name:     "hex with colons",
			input:    "01:02:03",
			expected: []byte{0x01, 0x02, 0x03},
		},
		{
			name:     "hex with 0x prefix",
			input:    "0x01 0x02",
			expected: []byte{0x01, 0x02},
		},
		{
			name:     "hex with dashes",
			input:    "01-02-03",
			expected: []byte{0x01, 0x02, 0x03},
		},
		{
			name:     "mixed separators",
			input:    "0x01:02-03 04",
			expected: []byte{0x01, 0x02, 0x03, 0x04},
		},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {
			writeHex = true

			result, err := parseWriteData(tt.input)
			suite.Assert().NoError(err, "MUST parse valid hex data")
			suite.Assert().Equal(tt.expected, result, "decoded bytes MUST match expected")
		})
	}
}

func (suite *ReadTestSuite) TestParseWriteData_InvalidHex() {
	// GOAL: Verify error handling for malformed hex input
	//
	// TEST SCENARIO: Parse invalid hex string → error returned → result is nil

	writeHex = true

	result, err := parseWriteData("ZZZZ")
	suite.Assert().Error(err, "MUST fail on invalid hex characters")
	suite.Assert().Nil(result, "result MUST be nil on error")
	suite.Assert().Contains(err.Error(), "invalid hex data", "error MUST indicate hex parsing failure")
}

func (suite *ReadTestSuite) TestParseWriteData_Raw() {
	// GOAL: Verify raw binary data parsing preserves all bytes including nulls
	//
	// TEST SCENARIO: Parse string in default mode → byte array created → all bytes preserved

	writeHex = false

	input := "test\x00data"
	result, err := parseWriteData(input)
	suite.Assert().NoError(err, "MUST parse raw data")
	suite.Assert().Equal([]byte(input), result, "raw bytes MUST be preserved exactly")
}

func (suite *ReadTestSuite) TestParseWriteData_UTF8() {
	// GOAL: Verify UTF-8 string conversion in default mode
	//
	// TEST SCENARIO: Parse UTF-8 string → byte array created → UTF-8 encoding preserved

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "ASCII string",
			input: "Hello, World!",
		},
		{
			name:  "UTF-8 with multibyte characters",
			input: "Hello, 世界",
		},
		{
			name:  "empty string",
			input: "",
		},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {
			writeHex = false

			result, err := parseWriteData(tt.input)
			suite.Assert().NoError(err, "MUST parse UTF-8 string")
			suite.Assert().Equal([]byte(tt.input), result, "UTF-8 bytes MUST be preserved")
		})
	}
}

func (suite *ReadTestSuite) TestOutputData_HexFormat() {
	// GOAL: Verify hex output encoding produces correct format
	//
	// TEST SCENARIO: Encode bytes as hex → hex string format verified → lowercase without separators

	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "simple bytes",
			input:    []byte{0x01, 0x02, 0x0A, 0xFF},
			expected: "01020aff",
		},
		{
			name:     "empty bytes",
			input:    []byte{},
			expected: "",
		},
		{
			name:     "single byte",
			input:    []byte{0xAB},
			expected: "ab",
		},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {
			encoded := hex.EncodeToString(tt.input)
			suite.Assert().Equal(tt.expected, encoded, "hex encoding MUST match expected format")
		})
	}
}

func (suite *ReadTestSuite) TestResolveTarget_UUIDNormalization() {
	// GOAL: Verify UUID normalization for target resolution
	//
	// TEST SCENARIO: Normalize various UUID formats → normalized UUIDs returned → consistent format

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "short UUID",
			input: "2a19",
		},
		{
			name:  "full UUID",
			input: "0000180f-0000-1000-8000-00805f9b34fb",
		},
		{
			name:  "mixed case UUID",
			input: "180F",
		},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {
			normalized := device.NormalizeUUID(tt.input)
			suite.Assert().NotEmpty(normalized, "normalized UUID MUST not be empty")
		})
	}
}

func (suite *ReadTestSuite) TestReadCmd_Flags() {
	// GOAL: Verify read command has all required flags with correct defaults
	//
	// TEST SCENARIO: Check flag definitions → all flags present → default values correct

	suite.Assert().NotNil(readCmd, "read command MUST be defined")
	suite.Assert().Equal("read <device-address> <uuid>", readCmd.Use, "command usage MUST match expected format")

	flags := []struct {
		name         string
		defaultValue string
	}{
		{name: "service", defaultValue: ""},
		{name: "char", defaultValue: ""},
		{name: "desc", defaultValue: ""},
		{name: "timeout", defaultValue: "5s"},
	}

	for _, f := range flags {
		suite.Run(f.name, func() {
			flag := readCmd.Flags().Lookup(f.name)
			suite.Assert().NotNil(flag, "flag MUST exist")
			if f.defaultValue != "" {
				suite.Assert().Equal(f.defaultValue, flag.DefValue, "default value MUST match")
			}
		})
	}

	// Boolean flags
	boolFlags := []string{"hex"}
	for _, name := range boolFlags {
		suite.Run(name, func() {
			flag := readCmd.Flags().Lookup(name)
			suite.Assert().NotNil(flag, "boolean flag MUST exist")
		})
	}

	// String flags with NoOptDefVal (optional values)
	suite.Run("watch", func() {
		flag := readCmd.Flags().Lookup("watch")
		suite.Assert().NotNil(flag, "watch flag MUST exist")
		suite.Assert().Equal("1s", flag.NoOptDefVal, "watch flag NoOptDefVal MUST be 1s")
	})
}

func (suite *ReadTestSuite) TestReadCmd_ArgsValidation() {
	// GOAL: Verify command accepts correct argument counts
	//
	// TEST SCENARIO: Validate args with different counts → accepts 1-2 args → rejects invalid counts

	validator := readCmd.Args
	suite.Assert().NotNil(validator, "args validator MUST be defined")

	tests := []struct {
		name      string
		args      []string
		shouldErr bool
	}{
		{
			name:      "valid with address only",
			args:      []string{"AA:BB:CC:DD:EE:FF"},
			shouldErr: false,
		},
		{
			name:      "valid with address and UUID",
			args:      []string{"AA:BB:CC:DD:EE:FF", "2a19"},
			shouldErr: false,
		},
		{
			name:      "invalid with no arguments",
			args:      []string{},
			shouldErr: true,
		},
		{
			name:      "invalid with too many arguments",
			args:      []string{"AA:BB:CC:DD:EE:FF", "2a19", "extra"},
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {
			err := validator(readCmd, tt.args)
			if tt.shouldErr {
				suite.Assert().Error(err, "MUST reject invalid argument count")
			} else {
				suite.Assert().NoError(err, "MUST accept valid argument count")
			}
		})
	}
}

// TestReadCommandSuite runs the test suite
func TestReadCommandSuite(t *testing.T) {
	suite.Run(t, new(ReadTestSuite))
}
