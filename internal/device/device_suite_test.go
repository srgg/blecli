//go:build test

package device_test

import (
	"context"
	"reflect"
	"time"
	"unsafe"

	"github.com/srg/blim/internal/device"
	"github.com/srg/blim/internal/devicefactory"
	"github.com/srg/blim/internal/testutils"
)

type DeviceTestSuite struct {
	testutils.MockBLEPeripheralSuite

	device     device.Device
	connection device.Connection
}

// ensureConnected ensures the device is connected, reconnecting if necessary
func (suite *DeviceTestSuite) ensureConnected() {
	if suite.device != nil && suite.device.IsConnected() {
		return
	}

	suite.device = devicefactory.NewDevice("AA:BB:CC:DD:EE:FF", suite.Logger)
	err := suite.device.Connect(context.Background(), &device.ConnectOptions{
		ConnectTimeout:        5 * time.Second,
		DescriptorReadTimeout: 1 * time.Second,
	})

	if err != nil {
		err := suite.device.Disconnect()
		if err != nil {
			suite.Logger.Error(err, "Failed to disconnect device after connect failure")
		}

		suite.device = nil
	}

	suite.Require().NoError(err, "MUST connect successfully")
	suite.connection = suite.device.GetConnection()
	suite.Require().NotNil(suite.connection, "connection MUST not be nil")
}

// SetupTest configures a default peripheral with one service containing all needed characteristics
func (suite *DeviceTestSuite) SetupTest() {
	// Configure a default peripheral with Heart Rate Service (180D) containing all characteristics needed by tests
	suite.WithPeripheral().
		WithService("180F").
		WithCharacteristic("2A19", "read", []byte{85}).
		WithCharacteristic("2A20", "read", []byte{}).
		WithService("180D").                                                                    // Heart Rate Service
		WithCharacteristic("2A37", "notify", []byte{0, 75}).                                    // Heart Rate Measurement (notify)
		WithCharacteristic("2A38", "read", []byte{1}).                                          // Body Sensor Location (read)
		WithCharacteristic("2A39", "write", []byte{}).                                          // Heart Rate Control Point (write)
		WithCharacteristic("2A40", "read,write", []byte{0x00}).                                 // "Battery Level" (read, write)
		WithCharacteristic("2A41", "read", []byte{42}, testutils.WithReadDelay(1*time.Second)). // Timeout read test
		WithCharacteristic("2A42", "write", []byte{}, testutils.WithWriteDelay(1*time.Second))  // Timeout write test

	// Call parent to apply the configuration and set up the device factory
	suite.MockBLEPeripheralSuite.SetupTest()

	suite.ensureConnected()
}

func (suite *DeviceTestSuite) SetupSubTest() {
	suite.ensureConnected()
}

func (suite *DeviceTestSuite) TearDownTest() {
	if suite.device != nil {
		if err := suite.device.Disconnect(); err != nil {
			suite.Logger.Error(err, "Failed to disconnect device")
		}
	}

	suite.device = nil
	suite.connection = nil
	suite.MockBLEPeripheralSuite.TearDownTest()
}

// setDeviceConnectionToNil uses unsafe reflection to set the device's connection field to nil.
// This enables testing defensive checks for error paths that should never happen in production.
// Uses unsafe.Pointer to bypass Go's unexported field access restrictions.
func (suite *DeviceTestSuite) setDeviceConnectionToNil() {
	devValue := reflect.ValueOf(suite.device).Elem()
	connectionField := devValue.FieldByName("connection")

	// Use unsafe to bypass unexported field restrictions
	reflect.NewAt(connectionField.Type(), unsafe.Pointer(connectionField.UnsafeAddr())).
		Elem().
		Set(reflect.Zero(connectionField.Type()))
}
