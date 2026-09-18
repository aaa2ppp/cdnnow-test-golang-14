package pools

import (
	"sync"
	"unsafe"
)

type BatchPool[T any] struct {
	batchSize int
	pool      sync.Pool
}

func NewBatchPool[T any](batchSize int) *BatchPool[T] {
	if batchSize <= 0 {
		panic("batchSize must be positive")
	}
	return &BatchPool[T]{
		batchSize: batchSize,
		pool:      sync.Pool{},
	}
}

func (p *BatchPool[T]) BatchSize() int {
	return p.batchSize
}

func (p *BatchPool[T]) Get() []T {
	ptr := p.pool.Get()
	if ptr == nil {
		return make([]T, 0, p.batchSize)
	}
	return unsafe.Slice(ptr.(*T), p.batchSize)[:0]
}

func (p *BatchPool[T]) Put(batch []T) {
	if cap(batch) == p.batchSize {
		p.pool.Put(unsafe.SliceData(batch))
	}
}
