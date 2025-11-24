package poolswap_test

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/keilerkonzept/poolswap"
)

const dataSize = 512 << 10 // 512 KB

// HeavyObject simulates a cache or config object.
// It embeds poolswap.Ref to work with poolswap.
type HeavyObject struct {
	poolswap.Ref
	data []byte
}

func newHeavyObject() *HeavyObject {
	h := &HeavyObject{
		data: make([]byte, dataSize),
	}
	for i := range h.data {
		h.data[i] = byte(i)
	}
	return h
}

func (h *HeavyObject) Reset() bool {
	clear(h.data)
	return true
}

func run(b *testing.B, readRatio int, reader func(), writer func()) {
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// simulate read/write mix.
			if rand.Intn(100) < readRatio {
				reader()
			} else {
				writer()
			}
		}
	})
}

func readWork(_ *HeavyObject) {
	time.Sleep(1 * time.Millisecond)
}

func writeWork(_ *HeavyObject) {
	time.Sleep(1 * time.Millisecond)
}

func runPoolSwap(b *testing.B, readRatio int) {
	p := poolswap.NewPool(newHeavyObject, (*HeavyObject).Reset)
	initial := p.Get()
	c := poolswap.NewContainer(p, initial)
	run(b, readRatio,
		func() { obj := c.Acquire(); readWork(obj); c.Release(obj) },
		func() { newObj := p.Get(); writeWork(newObj); c.Update(newObj) },
	)
}

func runAtomicPointer(b *testing.B, readRatio int) {
	var ptr atomic.Pointer[HeavyObject]
	ptr.Store(newHeavyObject())
	run(b, readRatio,
		func() { obj := ptr.Load(); readWork(obj) },
		func() { newObj := newHeavyObject(); writeWork(newObj); ptr.Store(newObj) },
	)
}

func runRWMutexSwap(b *testing.B, readRatio int) {
	var mu sync.RWMutex
	current := newHeavyObject()
	run(b, readRatio,
		func() { mu.RLock(); obj := current; mu.RUnlock(); readWork(obj) },
		func() { newObj := newHeavyObject(); writeWork(newObj); mu.Lock(); current = newObj; mu.Unlock() },
	)
}

func runRWMutexInPlace(b *testing.B, readRatio int) {
	var mu sync.RWMutex
	current := newHeavyObject()
	run(b, readRatio,
		func() { mu.RLock(); readWork(current); mu.RUnlock() },
		func() { mu.Lock(); current.Reset(); writeWork(current); mu.Unlock() },
	)
}
func Benchmark(b *testing.B) {
	impls := map[string]func(*testing.B, int){
		"PoolSwap":       runPoolSwap,
		"RWMutexSwap":    runRWMutexSwap,
		"AtomicPointer":  runAtomicPointer,
		"RWMutexInPlace": runRWMutexInPlace,
	}
	ratios := map[string]int{
		"99R_1W":  99,
		"90R_10W": 90,
		"50R_50W": 50,
	}

	for implName, implFunc := range impls {
		for ratioName, ratioVal := range ratios {
			// Format: BenchmarkName/impl=<name>/ratio=<name>
			b.Run(fmt.Sprintf("impl=%s/ratio=%s", implName, ratioName), func(b *testing.B) {
				implFunc(b, ratioVal)
			})
		}
	}
}
