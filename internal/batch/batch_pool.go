package batch

import (
	"sync"

	"aaa2ppp/cdnnow-test-golang-14/internal/domain"
)

const batchSize = 1024

type Batch struct {
	Nums []int64
	Kind domain.DurationKind
}

type Pool struct {
	pool sync.Pool
}

func NewPool() *Pool {
	return &Pool{
		pool: sync.Pool{
			New: func() any {
				return &Batch{Nums: make([]int64, 0, batchSize)}
			},
		},
	}
}

func (p *Pool) Get() *Batch {
	return p.pool.Get().(*Batch)
}

func (p *Pool) Put(b *Batch) {
	b.Nums = b.Nums[:0]
	b.Kind = 0
	p.pool.Put(b)
}
