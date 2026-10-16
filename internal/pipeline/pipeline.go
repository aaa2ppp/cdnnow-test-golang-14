package pipeline

import (
	"aaa2ppp/cdnnow-test-golang-14/internal/batch"
	"aaa2ppp/cdnnow-test-golang-14/internal/calculators"
	"aaa2ppp/cdnnow-test-golang-14/internal/domain"
	"aaa2ppp/cdnnow-test-golang-14/internal/metrics"
)

type calculator interface {
	Values() domain.Values
}

type Pipeline struct {
	numCh   chan int64
	calc    calculator
	metrics *metrics.Aggregator

	ignoreOver bool
}

func NewPipeline(queueSize int, workers int) *Pipeline {
	pool := batch.NewPool()

	dursCh := make(chan *batch.Batch, 50) // TODO: как выбрать?
	aggr := metrics.NewAggregator(dursCh, nil, pool)

	numCh := make(chan int64, queueSize)
	calc := calculators.NewAsync(workers, numCh, dursCh, pool)

	return &Pipeline{
		numCh:   numCh,
		calc:    calc,
		metrics: aggr,
	}
}

func (p *Pipeline) Done() <-chan struct{} {
	return p.metrics.Done()
}

func (p *Pipeline) Close() {
	close(p.numCh)
	<-p.Done()
}

func (p *Pipeline) Calculate(num int64) error {
	if p.ignoreOver {
		p.numCh <- num
	}
	select {
	case p.numCh <- num:
		return nil
	default:
		return domain.ErrOverload
	}
}

func (p *Pipeline) Values() domain.Values {
	return p.calc.Values()
}

func (p *Pipeline) CountRequests(kind domain.RequestKind, n int) {
	p.metrics.CountRequests(kind, n)
}

func (p *Pipeline) GetSnapshot(fn func(*domain.Snapshot)) {
	p.metrics.GetSnapshot(fn)
}
