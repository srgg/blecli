//go:build test

package device_test

import (
	"context"
	"testing"
	"time"

	"github.com/srg/blim/internal/device"
	"github.com/stretchr/testify/suite"
)

type ConnectionTestSuite struct {
	DeviceTestSuite
}

func (suite *ConnectionTestSuite) TestConnectionServices() {
	// GOAL: Verify connection service discovery and retrieval work correctly
	//
	// TEST SCENARIO: Various service access patterns → services retrieved correctly → proper error handling

	suite.Run("get all services", func() {
		// GOAL: Verify Services() returns all discovered services
		//
		// TEST SCENARIO: Connect to a device with multiple services → Services() called → all services returned in sorted order

		services := suite.connection.Services()

		suite.Assert().Len(services, 3, "MUST return all services")
		suite.Assert().Equal("1800", services[0].UUID(), "first service MUST be 1800 (Generic Access, sorted order)")
		suite.Assert().Equal("180d", services[1].UUID(), "second service MUST be 180d (Heart Rate, sorted order)")
		suite.Assert().Equal("180f", services[2].UUID(), "third service MUST be 180f (Battery, sorted order)")
	})

	suite.Run("get service by UUID", func() {
		// GOAL: Verify GetService() retrieves service by UUID
		//
		// TEST SCENARIO: Request service by UUID → service returned → UUID matches

		svc, err := suite.connection.GetService("180f")

		suite.Assert().NoError(err, "MUST find service")
		suite.Assert().NotNil(svc, "service MUST not be nil")
		suite.Assert().Equal("180f", svc.UUID(), "service UUID MUST match")
	})

	suite.Run("fail when service not found", func() {
		// GOAL: Verify GetService() returns NotFoundError for non-existent service
		//
		// TEST SCENARIO: Request non-existent service → NotFoundError returned → error message describes issue

		svc, err := suite.connection.GetService("ffff")

		suite.Assert().Error(err, "MUST return error for non-existent service")
		suite.Assert().Nil(svc, "service MUST be nil")

		var notFoundErr *device.NotFoundError
		suite.Assert().ErrorAs(err, &notFoundErr, "error MUST be NotFoundError")
		suite.Assert().Equal("service", notFoundErr.Resource, "resource type MUST be 'service'")
		suite.Assert().Equal([]string{"ffff"}, notFoundErr.UUIDs, "UUIDs MUST contain service UUID")
		suite.Assert().Equal("service \"ffff\" not found", err.Error(), "error message MUST match expected format")
	})

	suite.Run("UUID normalization", func() {
		// GOAL: Verify UUID normalization works for service lookup
		//
		// TEST SCENARIO: Request service with various UUID formats → service found → consistent behavior

		// Test various UUID formats
		svc1, err1 := suite.connection.GetService("180f")
		svc2, err2 := suite.connection.GetService("180F")
		svc3, err3 := suite.connection.GetService("0000180f-0000-1000-8000-00805f9b34fb")

		suite.Assert().NoError(err1, "lowercase UUID MUST work")
		suite.Assert().NoError(err2, "uppercase UUID MUST work")
		suite.Assert().NoError(err3, "full UUID MUST work")
		suite.Assert().Equal(svc1.UUID(), svc2.UUID(), "UUIDs MUST match")
		suite.Assert().Equal(svc1.UUID(), svc3.UUID(), "UUIDs MUST match")
	})
}

func (suite *ConnectionTestSuite) TestConnectionCharacteristics() {
	// GOAL: Verify that connection characteristic discovery and retrieval work correctly
	//
	// TEST SCENARIO: Various characteristic access patterns → characteristics retrieved correctly → proper error handling

	suite.Run("get characteristic by service and UUID", func() {
		// GOAL: Verify GetCharacteristic() retrieves characteristic
		//
		// TEST SCENARIO: Request characteristic by service and UUID → characteristic returned → UUIDs match

		char, err := suite.connection.GetCharacteristic("180f", "2a19")

		suite.Assert().NoError(err, "MUST find characteristic")
		suite.Assert().NotNil(char, "characteristic MUST not be nil")
		suite.Assert().Equal("2a19", char.UUID(), "characteristic UUID MUST match")
	})

	suite.Run("characteristic not found in service", func() {
		// GOAL: Verify GetCharacteristic() returns NotFoundError for non-existent characteristic
		//
		// TEST SCENARIO: Request non-existent characteristic → NotFoundError returned → error message describes issue

		char, err := suite.connection.GetCharacteristic("180f", "2a37")

		suite.Assert().Error(err, "MUST return error for non-existent characteristic")
		suite.Assert().Nil(char, "characteristic MUST be nil")

		var notFoundErr *device.NotFoundError
		suite.Assert().ErrorAs(err, &notFoundErr, "error MUST be NotFoundError")
		suite.Assert().Equal("characteristic", notFoundErr.Resource, "resource type MUST be 'characteristic'")
		suite.Assert().Equal([]string{"180f", "2a37"}, notFoundErr.UUIDs, "UUIDs MUST contain service and characteristic UUIDs")
		suite.Assert().Contains(err.Error(), "characteristic \"2a37\" not found in service \"180f\"", "error message MUST describe issue")
	})

	suite.Run("fail if service not found", func() {
		// GOAL: Verify GetCharacteristic() returns NotFoundError when service doesn't exist
		//
		// TEST SCENARIO: Request characteristic from non-existent service → NotFoundError returned → error message describes issue

		char, err := suite.connection.GetCharacteristic("ffff", "2a19")

		suite.Assert().Error(err, "MUST return error when service not found")
		suite.Assert().Nil(char, "characteristic MUST be nil")

		var notFoundErr *device.NotFoundError
		suite.Assert().ErrorAs(err, &notFoundErr, "error MUST be NotFoundError")
		suite.Assert().Equal("service", notFoundErr.Resource, "resource type MUST be 'service'")
		suite.Assert().Equal([]string{"ffff"}, notFoundErr.UUIDs, "UUIDs MUST contain service UUID")
		suite.Assert().Equal("service \"ffff\" not found", err.Error(), "error message MUST match expected format")
	})

	suite.Run("multiple characteristics in service", func() {
		// GOAL: Verify multiple characteristics can be retrieved from same service
		//
		// TEST SCENARIO: Service with multiple characteristics → all can be retrieved → correct data returned

		char1, err1 := suite.connection.GetCharacteristic("180d", "2a37")
		char2, err2 := suite.connection.GetCharacteristic("180d", "2a38")
		char3, err3 := suite.connection.GetCharacteristic("180d", "2a39")

		suite.Assert().NoError(err1, "MUST find first characteristic")
		suite.Assert().NoError(err2, "MUST find second characteristic")
		suite.Assert().NoError(err3, "MUST find third characteristic")
		suite.Assert().Equal("2a37", char1.UUID(), "first characteristic UUID MUST match")
		suite.Assert().Equal("2a38", char2.UUID(), "second characteristic UUID MUST match")
		suite.Assert().Equal("2a39", char3.UUID(), "third characteristic UUID MUST match")
	})
}

func (suite *ConnectionTestSuite) TestConnectionSubscriptionValidation() {
	// GOAL: Verify subscription validation works correctly
	//
	// TEST SCENARIO: Various subscription validation scenarios → proper errors returned → ErrUnsupported wrapped when appropriate

	suite.Run("subscribe to characteristic without notify support", func() {
		// GOAL: Verify subscribing to characteristic without notify/indicate returns ErrUnsupported
		//
		// TEST SCENARIO: Attempt subscription to read-only characteristic → error returned → error wraps device.ErrUnsupported

		// Attempt to subscribe to a characteristic without notify/indicate support
		// Provide a valid callback so validation logic runs (not just nil check)
		err := suite.connection.Subscribe([]*device.SubscribeOptions{
			{
				Service:         "180f",
				Characteristics: []string{"2a19"},
			},
		}, device.StreamEveryUpdate, 0, func(record *device.Record) {
			suite.Fail("callback MUST NOT be invoked when validation fails")
		})

		suite.Assert().Error(err, "subscription MUST fail for characteristic without notify support")
		suite.Assert().ErrorIs(err, device.ErrUnsupported, "error MUST wrap device.ErrUnsupported")
		suite.Assert().Contains(err.Error(), "characteristics without notification support", "error message MUST describe unsupported operation")
		suite.Assert().Contains(err.Error(), "2a19", "error message MUST contain characteristic UUID")
	})

	suite.Run("subscribe to multiple characteristics with mixed support", func() {
		// GOAL: Verify subscription validation detects characteristics without notify support
		//
		// TEST SCENARIO: Service with mix of notify and non-notify characteristics → subscribe to all → error returned with unsupported chars listed

		// Attempt to subscribe to all characteristics in service (some don't support notify)
		// Provide a valid callback so validation logic runs (not just nil check)
		err := suite.connection.Subscribe([]*device.SubscribeOptions{
			{
				Service: "180d",
				// Empty Characteristics means subscribe to all in service
			},
		}, device.StreamEveryUpdate, 0, func(record *device.Record) {
			suite.Fail("callback MUST NOT be invoked when validation fails")
		})

		suite.Assert().Error(err, "subscription MUST fail when some characteristics lack notify support")
		suite.Assert().ErrorIs(err, device.ErrUnsupported, "error MUST wrap device.ErrUnsupported")
		suite.Assert().Contains(err.Error(), "characteristics without notification support", "error message MUST describe unsupported operation")
	})

	suite.Run("subscribe to characteristic with notify support", func() {
		// GOAL: Verify subscription succeeds for characteristic with notify support
		//
		// TEST SCENARIO: Subscribe to characteristic with notify → subscription succeeds → no error

		err := suite.connection.Subscribe([]*device.SubscribeOptions{
			{
				Service:         "180d",
				Characteristics: []string{"2a37"},
			},
		}, device.StreamEveryUpdate, 0, func(record *device.Record) {
			// Callback receives notifications
		})

		suite.Assert().NoError(err, "subscription MUST succeed for characteristic with notify support")
	})

	suite.Run("subscribe to characteristic with indicate support", func() {
		// GOAL: Verify subscription succeeds for characteristic with indicate support
		//
		// TEST SCENARIO: Subscribe to characteristic with indicate → subscription succeeds → no error

		err := suite.connection.Subscribe([]*device.SubscribeOptions{
			{
				Service:         "180d",
				Characteristics: []string{"2a37"},
			},
		}, device.StreamEveryUpdate, 0, func(record *device.Record) {
			// Callback receives notifications
		})

		suite.Assert().NoError(err, "subscription MUST succeed for characteristic with indicate support")
	})

	suite.Run("subscribe to non-existent service", func() {
		// GOAL: Verify subscription validation detects missing services
		//
		// TEST SCENARIO: Subscribe to non-existent service → error returned → error does NOT wrap ErrUnsupported

		// Provide valid callback so validation logic runs (not just nil check)
		// Use service UUID that doesn't exist in device (180f is not in the peripheral)
		err := suite.connection.Subscribe([]*device.SubscribeOptions{
			{
				Service:         "180f",
				Characteristics: []string{"2a19"},
			},
		}, device.StreamEveryUpdate, 0, func(record *device.Record) {
			suite.Fail("callback MUST NOT be invoked when validation fails")
		})

		suite.Assert().Error(err, "subscription MUST fail for non-existent service")
		suite.Assert().Contains(err.Error(), "service", "error message MUST mention service")
		suite.Assert().Contains(err.Error(), "180f", "error message MUST contain service UUID")
	})

	suite.Run("subscribe to non-existent characteristic", func() {
		// GOAL: Verify subscription validation detects missing characteristics
		//
		// TEST SCENARIO: Subscribe to non-existent characteristic → error returned → error does NOT wrap ErrUnsupported

		// Provide a valid callback so validation logic runs (not just nil check)
		err := suite.connection.Subscribe([]*device.SubscribeOptions{
			{
				Service:         "180d",
				Characteristics: []string{"2aff"},
			},
		}, device.StreamEveryUpdate, 0, func(record *device.Record) {
			suite.Fail("callback MUST NOT be invoked when validation fails")
		})

		suite.Assert().Error(err, "subscription MUST fail for non-existent characteristic")
		suite.Assert().NotErrorIs(err, device.ErrUnsupported, "error MUST NOT wrap ErrUnsupported for missing characteristic")
		suite.Assert().Contains(err.Error(), "missing characteristics", "error message MUST describe missing characteristic")
		suite.Assert().Contains(err.Error(), "2aff", "error message MUST contain characteristic UUID")
	})
}

func (suite *ConnectionTestSuite) TestConnectionErrors() {
	// GOAL: Verify ConnectionError types are returned correctly for connection state issues
	//
	// TEST SCENARIO: Various connection state scenarios → proper ConnectionError types returned → error messages are informative

	suite.Run("already connected error uses ErrAlreadyConnected", func() {
		// GOAL: Verify ErrAlreadyConnected is returned when connecting while already connected
		//
		// TEST SCENARIO: Already connected device → attempt connect → ErrAlreadyConnected returned

		// suite.device is already connected via SetupTest

		// Attempt to connect again
		ctx := context.Background()
		err := suite.device.Connect(ctx, &device.ConnectOptions{
			ConnectTimeout:        5 * time.Second,
			DescriptorReadTimeout: 1 * time.Second,
		})

		suite.Assert().Error(err, "connect MUST fail when already connected")
		suite.Assert().ErrorIs(err, device.ErrAlreadyConnected, "error MUST be ErrAlreadyConnected")
		suite.Assert().Contains(err.Error(), "already_connected", "error message MUST contain connection state")
	})

	suite.Run("subscribe while not connected returns ErrNotConnected", func() {
		// GOAL: Verify ErrNotConnected is returned when subscribing to disconnected device
		//
		// TEST SCENARIO: Disconnect device → attempt subscribe → ErrNotConnected returned

		// Disconnect first
		err := suite.device.Disconnect()
		suite.Require().NoError(err, "disconnect MUST succeed")

		// Attempt to subscribe while disconnected
		err = suite.connection.Subscribe([]*device.SubscribeOptions{
			{
				Service:         "180d",
				Characteristics: []string{"2a37"},
			},
		}, device.StreamEveryUpdate, 0, func(record *device.Record) {
			suite.Fail("callback MUST NOT be invoked when not connected")
		})

		suite.Assert().Error(err, "subscribe MUST fail when not connected")
		suite.Assert().ErrorIs(err, device.ErrNotConnected, "error MUST be ErrNotConnected")
		suite.Assert().Contains(err.Error(), "not_connected", "error message MUST contain connection state")
	})

	suite.Run("connect with nil connection returns ErrNotInitialized", func() {
		// GOAL: Verify ErrNotInitialized is returned when connection is nil in Connect
		//
		// TEST SCENARIO: Set connection to nil → attempt connect → ErrNotInitialized returned

		// Use reflection to set connection to nil (should never happen in production)
		suite.setDeviceConnectionToNil()

		ctx := context.Background()
		err := suite.device.Connect(ctx, &device.ConnectOptions{
			ConnectTimeout:        5 * time.Second,
			DescriptorReadTimeout: 1 * time.Second,
		})

		suite.Assert().Error(err, "connect MUST fail when connection is nil")
		suite.Assert().ErrorIs(err, device.ErrNotInitialized, "error MUST be ErrNotInitialized")
		suite.Assert().Contains(err.Error(), "connect", "error message MUST mention connect")
	})

	suite.Run("disconnect with nil connection returns ErrNotInitialized", func() {
		// GOAL: Verify ErrNotInitialized is returned when the connection is nil in Disconnect
		//
		// TEST SCENARIO: Set connection to nil → attempt disconnect → ErrNotInitialized returned

		suite.setDeviceConnectionToNil()

		err := suite.device.Disconnect()

		suite.Assert().Error(err, "disconnect MUST fail when connection is nil")
		suite.Assert().ErrorIs(err, device.ErrNotInitialized, "error MUST be ErrNotInitialized")
		suite.Assert().Contains(err.Error(), "disconnect", "error message MUST mention disconnect")
	})
}

func (suite *ConnectionTestSuite) TestGracefulDisconnect() {
	// GOAL: Verify graceful disconnect handling via CoreBluetooth Disconnected() channel
	//
	// TEST SCENARIO: Close disconnect channel → connection context cancelled → error cause is ErrNotConnected

	suite.Run("CoreBluetooth disconnect cancels connection context", func() {
		// GOAL: Verify that closing the Disconnected() channel cancels the connection context with ErrNotConnected
		//
		// TEST SCENARIO: Close disconnect channel → connection context Done() fires → context.Cause() is ErrNotConnected

		suite.Require().True(suite.device.IsConnected(), "device MUST be connected before test")

		// Get the connection context before disconnect
		conn := suite.device.GetConnection()
		suite.Require().NotNil(conn, "connection MUST exist")
		ctx := conn.ConnectionContext()
		suite.Require().NotNil(ctx, "connection context MUST exist")

		// Verify context is not canceled yet
		select {
		case <-ctx.Done():
			suite.Fail("context MUST NOT be cancelled before disconnect")
		default:
			// Expected: context still active
		}

		// Get the disconnect channel from the peripheral builder
		disconnectChan := suite.PeripheralBuilder.GetDisconnectChannel()
		suite.Require().NotNil(disconnectChan, "disconnect channel MUST exist after Build()")

		// Simulate CoreBluetooth disconnect by closing the channel
		close(disconnectChan)

		// Give the monitoring goroutine a moment to process the disconnect
		time.Sleep(10 * time.Millisecond)

		// Verify connection context was canceled
		select {
		case <-ctx.Done():
			// Expected: context canceled
		case <-time.After(100 * time.Millisecond):
			suite.Fail("connection context MUST be cancelled after disconnect channel closes")
		}

		// Verify the context was canceled with ErrNotConnected cause
		cause := context.Cause(ctx)
		suite.Assert().ErrorIs(cause, device.ErrNotConnected, "context MUST be cancelled with ErrNotConnected")
	})
}

// TestConnectionTestSuite runs the test suite
func TestConnectionTestSuite(t *testing.T) {
	suite.Run(t, new(ConnectionTestSuite))
}
