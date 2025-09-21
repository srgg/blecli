package testutils

//import (
//	"github.com/go-ble/ble"
//	"github.com/srg/blecli/internal/testutils/mocks"
//)
//
//// DeviceBuilder builds a mocked Device
//type DeviceBuilder struct {
//	name        string
//	address     string
//	rssi        int
//	services    []string
//	manufData   []byte
//	serviceData map[string][]byte
//	txPower     *int
//	connectable bool
//}
//
//// NewDeviceBuilder creates a new builder
//func NewDeviceBuilder() *DeviceBuilder {
//	return &DeviceBuilder{
//		serviceData: make(map[string][]byte),
//		connectable: true,
//	}
//}
//
//func (b *DeviceBuilder) WithName(name string) *DeviceBuilder {
//	b.name = name
//	return b
//}
//
//func (b *DeviceBuilder) WithAddress(addr string) *DeviceBuilder {
//	b.address = addr
//	return b
//}
//
//func (b *DeviceBuilder) WithRSSI(rssi int) *DeviceBuilder {
//	b.rssi = rssi
//	return b
//}
//
//func (b *DeviceBuilder) WithServices(uuids ...string) *DeviceBuilder {
//	b.services = append(b.services, uuids...)
//	return b
//}
//
//func (b *DeviceBuilder) WithManufacturerData(data []byte) *DeviceBuilder {
//	b.manufData = data
//	return b
//}
//
//func (b *DeviceBuilder) WithServiceData(uuid string, data []byte) *DeviceBuilder {
//	b.serviceData[uuid] = data
//	return b
//}
//
//func (b *DeviceBuilder) WithTxPower(power int) *DeviceBuilder {
//	b.txPower = &power
//	return b
//}
//
//func (b *DeviceBuilder) WithConnectable(connectable bool) *DeviceBuilder {
//	b.connectable = connectable
//	return b
//}
//
//// Build returns a MockDevice with expectations set
//func (b *DeviceBuilder) Build() *mocks.MockDevice {
//	mockDev := &mocks.MockDevice{}
//
//	// Convert string UUIDs to ble.UUID
//	var bleServices []ble.UUID
//	for _, s := range b.services {
//		bleServices = append(bleServices, ble.MustParse(s))
//	}
//
//	// Convert service data
//	bleServiceData := make(map[string][]byte)
//	for uuid, data := rang`e b.serviceData {
//		bleServiceData[uuid] = data
//	}
//
//	// Setup mock expectations
//	mockDev.On("GetID").Return(b.address)
//	mockDev.On("GetName").Return(b.name)
//	mockDev.On("GetAddress").Return(b.address)
//	mockDev.On("GetRSSI").Return(b.rssi)
//	mockDev.On("GetServices").Return(bleServices)
//	mockDev.On("GetManufacturerData").Return(b.manufData)
//	mockDev.On("GetServiceData").Return(bleServiceData)
//	mockDev.On("GetTxPower").Return(b.txPower)
//	mockDev.On("IsConnectable").Return(b.connectable)
//	mockDev.On("DisplayName").Return(func() string {
//		if b.name != "" {
//			return b.name
//		}
//		return b.address
//	})
//
//	return mockDev
//}
