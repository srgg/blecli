//go:build test

package device_test

import (
	"testing"
	"time"

	"github.com/srg/blim/internal/device"
	"github.com/stretchr/testify/suite"
)

// CharacteristicTestSuite tests characteristic Read/Write operations using MockBLEPeripheralSuite
type CharacteristicTestSuite struct {
	DeviceTestSuite
}

func (suite *CharacteristicTestSuite) TestCharacteristicRead() {
	// GOAL: Verify characteristic read operations work correctly
	//
	// TEST SCENARIO: Various read scenarios → correct data returned → proper error handling

	suite.Run("success with data", func() {
		// GOAL: Verify characteristic read returns data successfully
		//
		// TEST SCENARIO: Read characteristic with data → data returned → no error

		char, err := suite.connection.GetCharacteristic("180f", "2a19")
		suite.Require().NoError(err, "MUST find characteristic")

		data, err := char.Read(5 * time.Second)

		suite.Assert().NoError(err, "MUST read successfully")
		suite.Assert().Equal([]byte{85}, data, "data MUST match expected value")
	})

	suite.Run("empty data", func() {
		// GOAL: Verify read returns empty data correctly
		//
		// TEST SCENARIO: Read characteristic with empty value → empty array returned → no error

		char, err := suite.connection.GetCharacteristic("180f", "2a20")
		suite.Require().NoError(err, "MUST find characteristic")

		data, err := char.Read(5 * time.Second)

		suite.Assert().NoError(err, "MUST read successfully")
		suite.Assert().Empty(data, "data MUST be empty")
		suite.Assert().NotNil(data, "data MUST not be nil")
	})

	suite.Run("multiple sequential reads", func() {
		// GOAL: Verify multiple sequential reads return the same data
		//
		// TEST SCENARIO: Read twice → both return the same data → no errors

		char, err := suite.connection.GetCharacteristic("180f", "2a19")
		suite.Require().NoError(err, "MUST find characteristic")

		data1, err1 := char.Read(5 * time.Second)
		data2, err2 := char.Read(5 * time.Second)

		suite.Assert().NoError(err1, "first read MUST succeed")
		suite.Assert().NoError(err2, "second read MUST succeed")
		suite.Assert().Equal([]byte{85}, data1, "first read data MUST match")
		suite.Assert().Equal([]byte{85}, data2, "second read data MUST match")
		suite.Assert().Equal(data1, data2, "both reads MUST return same data")
	})

	suite.Run("read from write-only characteristic", func() {
		// GOAL: Verify read from write-only characteristic returns ErrUnsupported error
		//
		// TEST SCENARIO: Read from write-only characteristic → error returned → error wraps device.ErrUnsupported

		char, err := suite.connection.GetCharacteristic("180d", "2a39")
		suite.Require().NoError(err, "MUST find characteristic")

		_, err = char.Read(5 * time.Second)

		suite.Assert().Error(err, "read MUST fail on write-only characteristic")
		suite.Assert().ErrorIs(err, device.ErrUnsupported, "error MUST wrap device.ErrUnsupported")
		suite.Assert().Contains(err.Error(), "characteristic 2a39", "error message MUST contain characteristic UUID")
		suite.Assert().Contains(err.Error(), "does not support read operations", "error message MUST describe the unsupported operation")
	})

	suite.Run("read while not connected returns ErrNotConnected", func() {
		// GOAL: Verify ErrNotConnected is returned when reading from disconnected device
		//
		// TEST SCENARIO: Get characteristic → disconnect device → attempt read → ErrNotConnected returned

		char, err := suite.connection.GetCharacteristic("180f", "2a19")
		suite.Require().NoError(err, "MUST find characteristic")

		// Disconnect the device
		err = suite.device.Disconnect()
		suite.Require().NoError(err, "disconnect MUST succeed")

		// Attempt to read while disconnected
		_, err = char.Read(5 * time.Second)

		suite.Assert().Error(err, "read MUST fail when not connected")
		suite.Assert().ErrorIs(err, device.ErrNotConnected, "error MUST be ErrNotConnected")
		suite.Assert().Contains(err.Error(), "2a19", "error message MUST contain characteristic UUID")
	})

	suite.Run("read timeout returns ErrTimeout", func() {
		// GOAL: Verify ErrTimeout is returned when read operation times out
		//
		// TEST SCENARIO: Read characteristic with 1s delay using 500ms timeout → ErrTimeout returned

		char, err := suite.connection.GetCharacteristic("180d", "2a41")
		suite.Require().NoError(err, "MUST find characteristic")

		// Attempt to read with timeout shorter than the mock delay (1s)
		_, err = char.Read(500 * time.Millisecond)

		suite.Assert().Error(err, "read MUST fail on timeout")
		suite.Assert().ErrorIs(err, device.ErrTimeout, "error MUST wrap device.ErrTimeout")
		suite.Assert().Contains(err.Error(), "2a41", "error message MUST contain characteristic UUID")
		suite.Assert().Contains(err.Error(), "500ms", "error message MUST contain timeout duration")
	})
}

func (suite *CharacteristicTestSuite) TestCharacteristicWrite() {
	// GOAL: Verify characteristic write operations work correctly
	//
	// TEST SCENARIO: Various write scenarios → operations succeed → proper error handling

	suite.Run("success with response", func() {
		// GOAL: Verify characteristic write with response succeeds
		//
		// TEST SCENARIO: Write data with response → operation succeeds → no error

		char, err := suite.connection.GetCharacteristic("180d", "2a39")
		suite.Require().NoError(err, "MUST find characteristic")

		err = char.Write([]byte{0x01, 0x02, 0x03}, true, 5*time.Second)

		suite.Assert().NoError(err, "MUST write successfully with response")
	})

	suite.Run("without response", func() {
		// GOAL: Verify write without response succeeds
		//
		// TEST SCENARIO: Write data without response → operation succeeds → no error

		char, err := suite.connection.GetCharacteristic("180d", "2a39")
		suite.Require().NoError(err, "MUST find characteristic")

		err = char.Write([]byte{0xFF, 0xFE}, false, 5*time.Second)

		suite.Assert().NoError(err, "MUST write successfully without response")
	})

	suite.Run("empty data", func() {
		// GOAL: Verify writing empty data is allowed
		//
		// TEST SCENARIO: Write an empty array → operation succeeds → no error

		char, err := suite.connection.GetCharacteristic("180d", "2a39")
		suite.Require().NoError(err, "MUST find characteristic")

		err = char.Write([]byte{}, true, 5*time.Second)

		suite.Assert().NoError(err, "MUST write empty data successfully")
	})

	suite.Run("large data", func() {
		// GOAL: Verify large data writes are handled correctly
		//
		// TEST SCENARIO: Write 512 bytes → operation succeeds → no error

		char, err := suite.connection.GetCharacteristic("180d", "2a39")
		suite.Require().NoError(err, "MUST find characteristic")

		largeData := make([]byte, 512)
		for i := range largeData {
			largeData[i] = byte(i % 256)
		}

		err = char.Write(largeData, true, 10*time.Second)

		suite.Assert().NoError(err, "MUST write large data successfully")
	})

	suite.Run("multiple sequential writes", func() {
		// GOAL: Verify multiple sequential writes succeed
		//
		// TEST SCENARIO: Write three times sequentially → all succeed → no errors

		char, err := suite.connection.GetCharacteristic("180d", "2a39")
		suite.Require().NoError(err, "MUST find characteristic")

		err1 := char.Write([]byte{0x01}, true, 5*time.Second)
		err2 := char.Write([]byte{0x02}, true, 5*time.Second)
		err3 := char.Write([]byte{0x03}, true, 5*time.Second)

		suite.Assert().NoError(err1, "first write MUST succeed")
		suite.Assert().NoError(err2, "second write MUST succeed")
		suite.Assert().NoError(err3, "third write MUST succeed")
	})

	suite.Run("write to read-only characteristic", func() {
		// GOAL: Verify write to read-only characteristic returns ErrUnsupported error
		//
		// TEST SCENARIO: Write to read-only characteristic → error returned → error wraps device.ErrUnsupported

		char, err := suite.connection.GetCharacteristic("180f", "2a19")
		suite.Require().NoError(err, "MUST find characteristic")

		err = char.Write([]byte{0x01}, true, 5*time.Second)

		suite.Assert().Error(err, "write MUST fail on read-only characteristic")
		suite.Assert().ErrorIs(err, device.ErrUnsupported, "error MUST wrap device.ErrUnsupported")
		suite.Assert().Contains(err.Error(), "characteristic 2a19", "error message MUST contain characteristic UUID")
		suite.Assert().Contains(err.Error(), "does not support write operations", "error message MUST describe the unsupported operation")
	})

	suite.Run("write-without-response when write is unavailable", func() {
		// GOAL: Verify write operation succeeds using write-without-response when write-with-response is unavailable
		//
		// TEST SCENARIO: Write with response requested → only write-without-response available → operation succeeds

		char, err := suite.connection.GetCharacteristic("180d", "2a39")
		suite.Require().NoError(err, "MUST find characteristic")

		// Request write with response, but characteristic only supports write-without-response
		// Should succeed by automatically using write-without-response
		err = char.Write([]byte{0x01, 0x02, 0x03}, true, 5*time.Second)

		suite.Assert().NoError(err, "write MUST succeed using write-without-response when write is unavailable")
	})

	suite.Run("write while not connected returns ErrNotConnected", func() {
		// GOAL: Verify ErrNotConnected is returned when writing to disconnected device
		//
		// TEST SCENARIO: Get characteristic → disconnect device → attempt write → ErrNotConnected returned

		char, err := suite.connection.GetCharacteristic("180d", "2a39")
		suite.Require().NoError(err, "MUST find characteristic")

		// Disconnect the device
		err = suite.device.Disconnect()
		suite.Require().NoError(err, "disconnect MUST succeed")

		// Attempt to write while disconnected
		err = char.Write([]byte{0x01}, true, 5*time.Second)

		suite.Assert().Error(err, "write MUST fail when not connected")
		suite.Assert().ErrorIs(err, device.ErrNotConnected, "error MUST be ErrNotConnected")
		suite.Assert().Contains(err.Error(), "2a39", "error message MUST contain characteristic UUID")
	})

	suite.Run("write timeout returns ErrTimeout", func() {
		// GOAL: Verify ErrTimeout is returned when write operation times out
		//
		// TEST SCENARIO: Write characteristic with 1s delay using 500ms timeout → ErrTimeout returned

		char, err := suite.connection.GetCharacteristic("180d", "2a42")
		suite.Require().NoError(err, "MUST find characteristic")

		// Attempt to write with timeout shorter than the mock delay (1s)
		err = char.Write([]byte{0x01}, true, 500*time.Millisecond)

		suite.Assert().Error(err, "write MUST fail on timeout")
		suite.Assert().ErrorIs(err, device.ErrTimeout, "error MUST wrap device.ErrTimeout")
		suite.Assert().Contains(err.Error(), "2a42", "error message MUST contain characteristic UUID")
		suite.Assert().Contains(err.Error(), "500ms", "error message MUST contain timeout duration")
	})
}

func (suite *CharacteristicTestSuite) TestCharacteristicReadWrite() {
	// GOAL: Verify read and write operations work together
	//
	// TEST SCENARIO: Combined read/write scenarios → both operations succeed → proper coordination

	suite.Run("integration", func() {
		// GOAL: Verify read and write operations work together
		//
		// TEST SCENARIO: Read initial value then write → both succeed → no errors

		char, err := suite.connection.GetCharacteristic("180d", "2a40")
		suite.Require().NoError(err, "MUST find characteristic")

		initialData, readErr := char.Read(5 * time.Second)
		writeErr := char.Write([]byte{0xFF}, true, 5*time.Second)

		suite.Assert().NoError(readErr, "read MUST succeed")
		suite.Assert().NoError(writeErr, "write MUST succeed")
		suite.Assert().Equal([]byte{0x00}, initialData, "initial data MUST match")
	})
}

// TestCharacteristicTestSuite runs the test suite
func TestCharacteristicTestSuite(t *testing.T) {
	suite.Run(t, new(CharacteristicTestSuite))
}
