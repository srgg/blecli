package testutils

import (
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
	logger.SetLevel(logrus.PanicLevel) // suppress logs during tests
	return &TestHelper{
		T:      t,
		Logger: logger,
	}
}

func (h *TestHelper) CreateMockAdvertisement(name, address string, rssi int) *AdvertisementBuilder {
	return NewAdvertisementBuilder().WithName(name).WithAddress(address).WithRSSI(rssi)
}

func (h *TestHelper) CreateMockAdvertisementFromJSON(jsonStrFmt string, args ...interface{}) *AdvertisementBuilder {
	return NewAdvertisementBuilder().FromJSON(jsonStrFmt, args...)
}
