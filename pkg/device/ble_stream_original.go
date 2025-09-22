package device

//
//import (
//	"context"
//	"sync"
//	"time"
//
//	"github.com/go-ble/ble"
//	"github.com/go-ble/ble/linux"
//)
//
//// ----------------------------
//// Flags
//// ----------------------------
//const (
//	FlagDropped uint32 = 1 << iota
//	FlagMissing
//)
//
//// ----------------------------
//// BLEValue with Pooling
//// ----------------------------
//type BLEValue struct {
//	TsUs  int64
//	Data  []byte
//	Seq   uint64
//	Flags uint32
//}
//
//var valuePool = sync.Pool{
//	New: func() interface{} { return &BLEValue{Data: make([]byte, 256)} },
//}
//
//func newBLEValue(data []byte) *BLEValue {
//	v := valuePool.Get().(*BLEValue)
//	v.TsUs = time.Now().UnixMicro()
//	v.Seq++
//	v.Flags = 0
//	if cap(v.Data) < len(data) {
//		v.Data = make([]byte, len(data))
//	}
//	v.Data = v.Data[:len(data)]
//	copy(v.Data, data)
//	return v
//}
//
//func releaseBLEValue(v *BLEValue) {
//	valuePool.Put(v)
//}
//
//// ----------------------------
//// Characteristic
//// ----------------------------
//type Characteristic struct {
//	UUID    string
//	updates chan *BLEValue
//	mu      sync.RWMutex
//	subs    []func(*BLEValue)
//}
//
//func NewCharacteristic(uuid string, buffer int) *Characteristic {
//	return &Characteristic{
//		UUID:    uuid,
//		updates: make(chan *BLEValue, buffer),
//		subs:    nil,
//	}
//}
//
//func (c *Characteristic) EnqueueValue(v *BLEValue) {
//	select {
//	case c.updates <- v:
//	default:
//		// Channel full, drop oldest
//		old := <-c.updates
//		old.Flags |= FlagDropped
//		releaseBLEValue(old)
//		c.updates <- v
//	}
//}
//
//func (c *Characteristic) Subscribe(fn func(*BLEValue)) {
//	c.mu.Lock()
//	defer c.mu.Unlock()
//	c.subs = append(c.subs, fn)
//}
//
//func (c *Characteristic) notifySubscribers(v *BLEValue) {
//	c.mu.RLock()
//	defer c.mu.RUnlock()
//	for _, fn := range c.subs {
//		fn(v)
//	}
//}
//
//// ----------------------------
//// Lua Subscription
//// ----------------------------
//type StreamPattern int
//
//const (
//	StreamEveryUpdate StreamPattern = iota
//	StreamBatched
//	StreamAggregated
//)
//
//type Record struct {
//	TsUs   int64
//	Seq    uint64
//	Values map[string][]byte
//	Flags  uint32
//}
//
//var recordPool = sync.Pool{
//	New: func() interface{} {
//		return &Record{Values: make(map[string][]byte)}
//	},
//}
//
//func newRecord() *Record {
//	r := recordPool.Get().(*Record)
//	r.TsUs = time.Now().UnixMicro()
//	r.Seq++
//	r.Flags = 0
//	for k := range r.Values {
//		delete(r.Values, k)
//	}
//	return r
//}
//
//func releaseRecord(r *Record) {
//	for k := range r.Values {
//		delete(r.Values, k)
//	}
//	recordPool.Put(r)
//}
//
//type LuaSubscription struct {
//	Chars    []*Characteristic
//	Pattern  StreamPattern
//	MaxRate  time.Duration
//	Callback func(*Record)
//	buffer   []*BLEValue
//}
//
//// ----------------------------
//// BLE Stream Manager
//// ----------------------------
//type BLEStreamManager struct {
//	client          ble.Client
//	characteristics map[string]*Characteristic
//	subscriptions   []*LuaSubscription
//	mu              sync.Mutex
//	ctx             context.Context
//	cancel          context.CancelFunc
//}
//
//func NewManager() (*BLEStreamManager, error) {
//	d, err := linux.NewDevice()
//	if err != nil {
//		return nil, err
//	}
//	ble.SetDefaultDevice(d)
//	ctx, cancel := context.WithCancel(context.Background())
//
//	return &BLEStreamManager{
//		client:          nil,
//		characteristics: make(map[string]*Characteristic),
//		subscriptions:   nil,
//		ctx:             ctx,
//		cancel:          cancel,
//	}, nil
//}
//
//func (m *BLEStreamManager) Stop() {
//	m.cancel()
//	if m.client != nil {
//		m.client.CancelConnection()
//	}
//}
//
//// ----------------------------
//// Connect & Subscribe to BLE
//// ----------------------------
//func (m *BLEStreamManager) Connect(addr string, charUUIDs []string) error {
//	ctx := ble.WithSigHandler(context.WithTimeout(m.ctx, 30*time.Second))
//	cln, err := ble.Dial(ctx, ble.NewAddr(addr))
//	if err != nil {
//		return err
//	}
//	m.client = cln
//
//	for _, uuid := range charUUIDs {
//		c := NewCharacteristic(uuid, 128)
//		m.characteristics[uuid] = c
//
//		char, err := cln.DiscoverCharacteristic(ble.MustParse(uuid))
//		if err != nil {
//			return err
//		}
//
//		err = cln.Subscribe(char, false, func(req []byte) {
//			val := newBLEValue(req)
//			c.EnqueueValue(val)
//			c.notifySubscribers(val)
//		})
//		if err != nil {
//			return err
//		}
//	}
//
//	return nil
//}
//
//// ----------------------------
//// Lua Subscriptions
//// ----------------------------
//func (m *BLEStreamManager) SubscribeLua(chars []string, pattern StreamPattern, maxRate time.Duration, callback func(*Record)) {
//	m.mu.Lock()
//	defer m.mu.Unlock()
//
//	var charObjs []*Characteristic
//	for _, uuid := range chars {
//		if c, ok := m.characteristics[uuid]; ok {
//			charObjs = append(charObjs, c)
//		}
//	}
//
//	sub := &LuaSubscription{
//		Chars:    charObjs,
//		Pattern:  pattern,
//		MaxRate:  maxRate,
//		Callback: callback,
//		buffer:   make([]*BLEValue, 0, 128),
//	}
//
//	m.subscriptions = append(m.subscriptions, sub)
//
//	go m.runLuaSubscription(sub)
//}
//
//// ----------------------------
//// Run Lua Subscription
//// ----------------------------
//func (m *BLEStreamManager) runLuaSubscription(sub *LuaSubscription) {
//	ticker := time.NewTicker(sub.MaxRate)
//	defer ticker.Stop()
//
//	for {
//		select {
//		case <-m.ctx.Done():
//			return
//		case <-ticker.C:
//			if sub.Pattern == StreamBatched && len(sub.buffer) > 0 {
//				record := newRecord()
//				for _, val := range sub.buffer {
//					record.Values[valUUID(val)] = val.Data
//					if val.Flags != 0 {
//						record.Flags |= val.Flags
//					}
//					record.TsUs = val.TsUs
//					releaseBLEValue(val)
//				}
//				sub.buffer = sub.buffer[:0]
//				sub.Callback(record)
//				releaseRecord(record)
//			}
//			if sub.Pattern == StreamAggregated {
//				record := newRecord()
//				for _, c := range sub.Chars {
//					select {
//					case val := <-c.updates:
//						record.Values[c.UUID] = val.Data
//						if val.Flags != 0 {
//							record.Flags |= val.Flags
//						}
//						record.TsUs = val.TsUs
//						releaseBLEValue(val)
//					default:
//						record.Flags |= FlagMissing
//					}
//				}
//				sub.Callback(record)
//				releaseRecord(record)
//			}
//		default:
//			if sub.Pattern == StreamEveryUpdate {
//				for _, c := range sub.Chars {
//					select {
//					case val := <-c.updates:
//						record := newRecord()
//						record.Values[c.UUID] = val.Data
//						record.TsUs = val.TsUs
//						if val.Flags != 0 {
//							record.Flags |= val.Flags
//						}
//						sub.Callback(record)
//						releaseBLEValue(val)
//						releaseRecord(record)
//					default:
//					}
//				}
//			} else {
//				time.Sleep(time.Millisecond)
//			}
//		}
//	}
//}
//
//func valUUID(v *BLEValue) string {
//	// Replace with real UUID mapping if needed
//	return "uuid-placeholder"
//}
