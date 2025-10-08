package device

import (
	"sort"
)

// ----------------------------
// BLE Service
// ----------------------------

// BLEService represents a GATT service and its characteristics
type BLEService struct {
	UUID            string
	knownName       string
	Characteristics map[string]*BLECharacteristic
}

func (s *BLEService) GetUUID() string {
	return s.UUID
}

func (s *BLEService) KnownName() string {
	return s.knownName
}

func (s *BLEService) GetCharacteristics() []Characteristic {
	result := make([]Characteristic, 0, len(s.Characteristics))
	for _, char := range s.Characteristics {
		result = append(result, char)
	}
	// Sort by UUID for consistent ordering
	sort.Slice(result, func(i, j int) bool {
		return result[i].GetUUID() < result[j].GetUUID()
	})
	return result
}
