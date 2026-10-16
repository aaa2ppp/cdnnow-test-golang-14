package calculators

import (
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/domain"
	"aaa2ppp/cdnnow-test-golang-14/internal/operators"
)

type syncState struct {
	addOp operators.Op
	subOp operators.Op
	vals  domain.Values
}

func (c *syncState) add(num int64) time.Duration {
	start := time.Now()
	c.vals.Sum = c.addOp.Apply(c.vals.Sum, num)
	return time.Since(start)
}

func (c *syncState) sub(num int64) time.Duration {
	start := time.Now()
	c.vals.Sub = c.subOp.Apply(c.vals.Sub, num)
	return time.Since(start)
}

func (c *syncState) calculate(num int64) domain.Sample {
	return domain.Sample{
		Add: c.add(num),
		Sub: c.sub(num),
	}
}

// Sync выполняет вычисления синхронно.
// Клиент ждет результата; Sum и Sub согласованы.
type Sync struct {
	syncState
	recordFn func(domain.Sample)
	lock     chan struct{} // FIFO-мьютекс, cap=1
}

func NewSync(recordFn func(domain.Sample)) *Sync {
	return &Sync{
		recordFn: recordFn,
		lock:     make(chan struct{}, 1),
		syncState: syncState{
			addOp: operators.AddOp(),
			subOp: operators.SubOp(),
		},
	}
}

func (c *Sync) record(sample domain.Sample) {
	if c.recordFn != nil {
		c.recordFn(sample)
	}
}

func (c *Sync) Calculate(num int64) error {
	c.lock <- struct{}{}
	sample := c.calculate(num)
	<-c.lock
	c.record(sample)
	return nil
}

func (c *Sync) Values() domain.Values {
	c.lock <- struct{}{}
	vals := c.vals
	<-c.lock
	return vals
}
