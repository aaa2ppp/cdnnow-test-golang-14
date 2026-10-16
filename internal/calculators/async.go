package calculators

import (
	"log/slog"
	"sync"
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/batch"
	"aaa2ppp/cdnnow-test-golang-14/internal/domain"
	"aaa2ppp/cdnnow-test-golang-14/internal/operators"
)

const batchTimeout = 100 * time.Millisecond

func batchCompute(op operators.Op, val int64, nums []int64, durs []int64) (int64, []int64) {
	for _, num := range nums {
		start := time.Now()
		val = op.Apply(val, num)
		durs = append(durs, int64(time.Since(start)))
	}
	return val, durs
}

type worker struct {
	res  *Async
	in   <-chan *batch.Batch
	out  chan<- *batch.Batch
	pool *batch.Pool
}

func (w worker) run() {
	for ib := range w.in {
		fn := func(op operators.Op, kind domain.DurationKind) int64 {
			ob := w.pool.Get()
			val, durs := batchCompute(op, 0, ib.Nums, ob.Nums[:0])
			ob.Kind = kind
			ob.Nums = durs
			w.out <- ob
			return val
		}

		sum := fn(w.res.addOp, domain.Add)
		sub := fn(w.res.subOp, domain.Sub)

		w.res.update(sum, sub)
		w.pool.Put(ib)
	}
}

type batcher struct {
	in   <-chan int64
	outs []chan *batch.Batch
	pool *batch.Pool
}

func (b batcher) run() {
	defer func() {
		for _, out := range b.outs {
			close(out)
		}
	}()

	tm := time.NewTimer(time.Hour)
	tm.Stop()

	var ob *batch.Batch
	var i int

	flush := func() {
		tm.Stop()

		b.outs[i] <- ob
		ob = nil

		i++
		if i == len(b.outs) {
			i = 0
		}
	}

	var batchCap int
	for {
		select {
		case num, ok := <-b.in:
			if !ok {
				if ob != nil {
					flush()
				}
				return
			}

			if ob == nil {
				ob = b.pool.Get()
				batchCap = cap(ob.Nums)
				tm.Reset(batchTimeout)
			}

			ob.Nums = append(ob.Nums, num)
			if len(ob.Nums) >= batchCap {
				flush()
			}

		case <-tm.C:
			flush()
		}
	}
}

type Async struct {
	addOp operators.Op
	subOp operators.Op
	mu    sync.Mutex
	sum   int64
	sub   int64
}

func (c *Async) update(sum, sub int64) {
	c.mu.Lock()
	c.sum = c.addOp.Apply(c.sum, sum)
	c.sub = c.subOp.Apply(c.sub, c.subOp.Apply(0, sub))
	c.mu.Unlock()
}

func (c *Async) Values() domain.Values {
	c.mu.Lock()
	vals := domain.Values{
		Sum: c.sum,
		Sub: c.sub,
	}
	c.mu.Unlock()
	return vals
}

func NewAsync(workers int, in <-chan int64, out chan<- *batch.Batch, pool *batch.Pool) *Async {
	slog.Info("new parallel calculator", "workers", workers)

	c := &Async{
		addOp: operators.AddOp(),
		subOp: operators.SubOp(),
	}

	N := workers

	tasks := make([]chan *batch.Batch, N)
	for i := range tasks {
		tasks[i] = make(chan *batch.Batch, 1)
	}

	var wg sync.WaitGroup
	wg.Add(N)

	for i := range N {
		go func(i int) {
			defer wg.Done()
			worker{
				res:  c,
				in:   tasks[i],
				out:  out,
				pool: pool,
			}.run()
		}(i)
	}

	go func() {
		defer close(out)
		batcher{
			in:   in,
			outs: tasks,
			pool: pool,
		}.run()
		wg.Wait()
	}()

	return c
}
