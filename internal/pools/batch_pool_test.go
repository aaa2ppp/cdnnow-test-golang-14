//go:build !race

package pools

import (
	"fmt"
	"runtime"
	"testing"
	"unsafe"

	"github.com/aaa2ppp/be"
)

func TestBatchPool(t *testing.T) {
	const batchSize = 10
	const iters = 100

	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))

	for n := 0; n < iters; n++ {
		if !t.Run(fmt.Sprintf("iter_%d", n), func(t *testing.T) {
			pool := NewBatchPool[int](batchSize)

			batch := pool.Get()
			be.Equal(t, 0, len(batch))
			be.Equal(t, batchSize, cap(batch))

			ptr := uintptr(unsafe.Pointer(unsafe.SliceData(batch)))

			for i := 0; i < batchSize; i++ {
				batch = append(batch, i+n)
			}
			be.Equal(t, ptr, uintptr(unsafe.Pointer(unsafe.SliceData(batch))))

			pool.Put(batch)
			batch = nil //nolint:ineffassign // drop the last reference so GC sees only the pool's ref

			runtime.GC() // объект в sync.Pool переживает один GC

			batch = pool.Get()
			be.Equal(t, ptr, uintptr(unsafe.Pointer(unsafe.SliceData(batch))))
		}) {
			break
		}
	}
}
