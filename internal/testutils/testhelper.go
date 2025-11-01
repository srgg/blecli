//go:build test

package testutils

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	devicemocks "github.com/srg/blim/internal/testutils/mocks/device"
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

func CreateMockAdvertisementFromJSON(jsonStrFmt string, args ...interface{}) *AdvertisementBuilder[*devicemocks.MockAdvertisement] {
	return NewAdvertisementBuilder().FromJSON(jsonStrFmt, args...)
}

func LoadScript(relPath string) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	// Clean the path to normalize './', '../', and redundant separators
	relPath = filepath.Clean(relPath)

	var fullPath string

	// If path starts with '/', treat it as relative to project root
	if len(relPath) > 0 && relPath[0] == '/' {
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

		// Strip leading '/' and join with project root
		fullPath = filepath.Join(projectRoot, relPath[1:])
	} else {
		// Treat as relative to the current working directory
		fullPath = filepath.Join(wd, relPath)
	}

	// Read the file contents
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", fullPath, err)
	}

	return string(data), nil
}
