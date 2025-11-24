# poolswap

[![Go Reference](https://pkg.go.dev/badge/github.com/keilerkonzept/poolswap.svg)](https://pkg.go.dev/github.com/keilerkonzept/poolswap)
[![Go Report Card](https://goreportcard.com/badge/github.com/keilerkonzept/poolswap?)](https://goreportcard.com/report/github.com/keilerkonzept/poolswap)

A goroutine-safe container for hot-swapping heavy objects (e.g., caches, configurations) without blocking readers or generating GC pressure.

Combines `sync.Pool` with atomic reference counting to enable non-blocking reads while ensuring old objects are only recycled after all readers finish.

**Contents**
- [Why?](#why)
- [Features](#features)
- [Usage](#usage)
- [Performance](#performance)
- [Notes](#notes)

## Why?

Read-mostly shared resources that need periodic updates present a tradeoff:

- **In-place update under lock (e.g. `sync.RWMutex`)**: Blocks all readers during updates
- **Pointer swap (e.g. `atomic.Pointer` or `sync.RWMutex`)**: Fast but forces you to allocate a new object on each update, causing GC pressure
- **`sync.Pool` + `atomic.Pointer`**: Seems ideal but is unsafe (see [Notes](#why-not-syncpool--atomicpointer) below)

`poolswap` solves this through reference counting. Objects return to the pool only when all readers release them.

## Features

- Non-blocking reads (lock held only during pointer acquisition)
- Object reuse via `sync.Pool` (zero-allocation at steady state)
- Type-safe API using Go generics

## Usage

### Define Your Object

Embed `poolswap.Ref` as the first field:

```go
import "github.com/keilerkonzept/poolswap"

type MyCache struct {
    poolswap.Ref
    data map[string]string
}
```

### Create a Pool

Provide a factory function and a reset function:

```go
pool := poolswap.NewPool(
    func() *MyCache {
        return &MyCache{data: make(map[string]string)}
    },
    func(c *MyCache) bool {
        clear(c.data)
        return true // true = return to pool, false = discard
    },
)
```

### Create a Container

```go
container := poolswap.NewEmptyContainer(pool)

// Or initialize with an object:
container := poolswap.NewContainer(pool, &MyObject{
    data : map[string]string{"key": "value"},
})
```

### Read from Container

Always `Release` after `Acquire`:

```go
func read(container *poolswap.Container[MyCache, *MyCache]) {
    cache := container.Acquire()
    if cache == nil {
        return // Container empty
    }
    defer container.Release(cache)

    // Use cache safely
    val := cache.data["key"]
}

// Or use the helper (but this may allocate for the closure):
container.WithAcquire(func(cache *MyCache) {
    if cache != nil {
        val := cache.data["key"]
    }
})
```

### Update Container

```go
func update(container *poolswap.Container[MyCache, *MyCache]) {
    newCache := pool.Get()
    newCache.data["key"] = "new_value"

    container.Update(newCache)
    // Old cache automatically returned to pool once all readers finish
}
```

## Performance

The [benchmarks](./poolswap_benchmark_test.go) compare four approaches for managing a 512KB object with concurrent reads/writes on an M1 Pro (10 cores):

| /ratio  | `PoolSwap` (base) | `RWMutexSwap`      | `AtomicPointer`    | `RWMutexInPlace`     |
|:--------|:------------------|:-------------------|:-------------------|:---------------------|
| **Time (sec/op)** |
| 99R_1W  | `127.5µs`         | `131.0µs` (~ )     | `129.6µs` (~ )     | `128.5µs` (~ )       |
| 90R_10W | `127.1µs`         | `114.5µs` (-9.91%) | `110.0µs` (-13.46%)| `254.5µs` (+100.23%) |
| 50R_50W | `132.1µs`         | `114.3µs` (-13.47%)| `114.1µs` (-13.59%)| `974.4µs` (+637.73%) |
| **Memory (B/op)** |
| 99R_1W  | `268 B`           | `5,269 B` (+1866%) | `5,382 B` (+1908%) | `0 B` (-100%)        |
| 90R_10W | `524 B`           | `53,345 B` (+10080%)| `52,517 B` (+9922%)| `1.5 B` (-99%)       |
| 50R_50W | `918 B`           | `263,920 B` (+28649%)|`262,584 B` (+28503%)|`1.5 B` (-99%)       |


For read-heavy workloads (99R/1W, typical of cache + background updater patterns), `poolswap` has negligible overhead versus `AtomicPointer` or `RWMutexSwap` while eliminating their GC pressure.

For mixed workloads (50R/50W), `poolswap` adds constant overhead (14% in the example benchmark) versus pointer-swap approaches (due to reference counting) but eliminates GC pressure. Compared to `RWMutexInPlace`, which has similar allocation characteristics, `poolswap` avoids lock contention on reads (specifically 7-8× faster in this example).

## Notes

### Why not `sync.Pool` + `atomic.Pointer`?

The naive combination is unsafe:

```go
var current atomic.Pointer[MyCache]
var pool = sync.Pool{...}

func update() {
    newCache := pool.Get().(*MyCache)
    // populate newCache
    oldCache := current.Swap(newCache)
    pool.Put(oldCache) // RACE CONDITION: readers may still be using oldCache
}
```

When the writer swaps the pointer, readers may still hold references to `oldCache`. If `oldCache` is immediately returned to the pool, a subsequent `pool.Get()` can return the same memory location while the original reader is still using it—a use-after-free race condition. This is what the reference-counting in `poolswap` fixes.

## License

MIT
