package testutils

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
)

type TestHelper struct {
	T      *testing.T
	Logger *logrus.Logger
}

// NewTestHelper creates a test helper with a suppressed logger.
func NewTestHelper(t *testing.T) *TestHelper {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel) // enable debug logs to track execution flow
	return &TestHelper{
		T:      t,
		Logger: logger,
	}
}

func CreateMockAdvertisement(name, address string, rssi int) *AdvertisementBuilder {
	return NewAdvertisementBuilder().WithName(name).WithAddress(address).WithRSSI(rssi)
}

func CreateMockAdvertisementFromJSON(jsonStrFmt string, args ...interface{}) *AdvertisementBuilder {
	return NewAdvertisementBuilder().FromJSON(jsonStrFmt, args...)
}

func CreateMockPeripheralDevice() *PeripheralDeviceBuilder {
	return NewPeripheralDeviceBuilder()
}

func CreateMockPeripheralDeviceFromJSON(jsonStrFmt string, args ...interface{}) *PeripheralDeviceBuilder {
	return NewPeripheralDeviceBuilder().FromJSON(jsonStrFmt, args...)
}

//// CreateComprehensiveMockDevice creates a mock device with all services needed for comprehensive testing
//func CreateComprehensiveMockDevice() *PeripheralDeviceBuilder {
//	return CreateMockPeripheralDeviceFromJSON(`{
//		"services": [
//			{
//				"uuid": "1234",
//				"characteristics": [
//					{
//						"uuid": "5678",
//						"properties": "read,notify",
//						"value": [0]
//					}
//				]
//			},
//			{
//				"uuid": "180D",
//				"characteristics": [
//					{ "uuid": "2A37", "properties": "read,notify", "value": [0] },
//					{ "uuid": "2A38", "properties": "read,notify", "value": [0] }
//				]
//			},
//			{
//				"uuid": "180F",
//				"characteristics": [
//					{ "uuid": "2A19", "properties": "read,notify", "value": [0] }
//				]
//			},
//			{
//				"uuid": "6e400001-b5a3-f393-e0a9-e50e24dcca9e",
//				"characteristics": [
//					{
//						"uuid": "6e400003-b5a3-f393-e0a9-e50e24dcca9e",
//						"properties": "read,notify",
//						"value": [0]
//					},
//					{
//						"uuid": "6e400002-b5a3-f393-e0a9-e50e24dcca9e",
//						"properties": "read,notify",
//						"value": [0]
//					}
//				]
//			},
//			{
//				"uuid": "0000180d-0000-1000-8000-00805f9b34fb",
//				"characteristics": [
//					{
//						"uuid": "00002a37-0000-1000-8000-00805f9b34fb",
//						"properties": "read,notify",
//						"value": [0]
//					}
//				]
//			},
//			{
//				"uuid": "1000180d-0000-1000-8000-00805f9b34fb",
//				"characteristics": [
//					{
//						"uuid": "10002a37-0000-1000-8000-00805f9b34fb",
//						"properties": "read,notify",
//						"value": [0]
//					}
//				]
//			}
//		]
//	}`)
//}

func LoadScript(relPath string) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	// Navigate up to find the project root (look for go.mod file)
	projectRoot := wd
	for {
		if _, err := os.Stat(filepath.Join(projectRoot, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(projectRoot)
		if parent == projectRoot {
			return "", fmt.Errorf("could not find project root (go.mod not found)")
		}
		projectRoot = parent
	}

	// Join project root with the given relative path
	fullPath := filepath.Join(projectRoot, relPath)

	// Read the file contents
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", fullPath, err)
	}

	return string(data), nil
}
