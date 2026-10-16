package metrics

import (
	"log"
	"math"
	"math/bits"
	"sync"
	"sync/atomic"
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/batch"
	"aaa2ppp/cdnnow-test-golang-14/internal/domain"
	"aaa2ppp/cdnnow-test-golang-14/internal/queue"

	"github.com/HdrHistogram/hdrhistogram-go"
)

const metricsWindow = 60
const histLowestDiscernible = int64(time.Microsecond)
const histHighestTrackable = int64(100 * time.Millisecond)
const histSignificantDigits = 3

type histOverflow struct {
	count   int64
	maximum int64
	sumLog2 int64
}

func log2u(x uint64) int64 {
	// для оценки абсолютная точность не требуется
	return int64(bits.Len64(x) - 1)
}

func (ho *histOverflow) add(x int64) {
	if x <= 0 {
		return
	}
	ho.count++
	ho.maximum = max(ho.maximum, x)
	ho.sumLog2 += log2u(uint64(x))
}

// При count=0 не имеет смысла, возвращает 0
func (ho *histOverflow) geomean() time.Duration {
	if ho.count == 0 {
		return 0
	}
	meanLog2 := float64(ho.sumLog2) / float64(ho.count)
	return time.Duration(math.Round(math.Exp2(meanLog2)))
}

func (ho *histOverflow) reset() {
	*ho = histOverflow{}
}

type snapshot struct {
	domain.Snapshot
	deadline time.Time
	valid    bool
}

type histMap [domain.DurationKindSize]hdrhistogram.WindowedHistogram
type overMap [domain.DurationKindSize]histOverflow

type cntMap [domain.RequestKindSize]atomic.Int64
type reqMap [domain.RequestKindSize]queue.Deque[int64]

type Aggregator struct {
	in1  <-chan *batch.Batch
	in2  <-chan domain.Sample
	pool *batch.Pool

	// execute durations
	hists histMap
	over  overMap
	done  chan struct{}

	// request counters
	cnts cntMap
	reqs reqMap

	// snapshot
	snapMu sync.Mutex
	snapCh chan struct{}
	snap   snapshot
}

func NewAggregator(in1 <-chan *batch.Batch, in2 <-chan domain.Sample, pool *batch.Pool) *Aggregator {
	m := &Aggregator{
		in1:    in1,
		in2:    in2,
		pool:   pool,
		done:   make(chan struct{}),
		snapCh: make(chan struct{}),
	}

	for kind := range domain.DurationKindSize {
		m.hists[kind] = *hdrhistogram.NewWindowed(metricsWindow, histLowestDiscernible, histHighestTrackable, histSignificantDigits)
	}

	for kind := range domain.RequestKindSize {
		m.reqs[kind] = *queue.NewDequeFrom(make([]int64, metricsWindow))
	}

	go m.serve()
	return m
}

func (p *Aggregator) rotate() {
	for kind := range domain.DurationKindSize {
		p.hists[kind].Rotate()
		if o := &p.over[kind]; o.count > 0 {
			log.Printf("Metrics.rotate: '%v' histogram overflow: count=%v maximum=%v geomean=%v",
				kind, o.count, time.Duration(o.maximum), time.Duration(o.geomean()))
			o.reset()
		}
	}

	for kind := range domain.RequestKindSize {
		p.reqs[kind].PopFront()
		p.reqs[kind].PushBack(p.cnts[kind].Swap(0))
	}
}

func (p *Aggregator) makeSnapshot() {
	for kind := range p.hists {
		p.snap.Histograms[kind] = p.hists[kind].Merge()
	}

	for kind := range p.reqs {
		b := p.snap.Requests[kind]
		p.snap.Requests[kind] = p.reqs[kind].ToSlice(b)
	}

	p.snap.Version++
	p.snap.deadline = time.Now().Add(10 * time.Millisecond)
	p.snap.valid = true
}

func (p *Aggregator) record(kind domain.DurationKind, x int64) {
	if err := p.hists[kind].Current.RecordValue(x); err != nil {
		p.over[kind].add(x)
	}
}

func (p *Aggregator) batchRecord(kind domain.DurationKind, nums []int64) {
	for _, x := range nums {
		p.record(kind, x)
	}
}

func (p *Aggregator) serve() {
	defer close(p.done)

	in1 := p.in1
	in2 := p.in2
	pool := p.pool

	tk := time.NewTicker(time.Second)
	defer tk.Stop()

	var count int
	if in1 != nil {
		count++
	}
	if in2 != nil {
		count++
	}

	for count > 0 {
		select {
		case ib, ok := <-in1:
			if !ok {
				in1 = nil
				count--
				break
			}
			p.batchRecord(ib.Kind, ib.Nums)
			pool.Put(ib)
			p.snap.valid = false

		case smpl, ok := <-in2:
			if !ok {
				in2 = nil
				count--
				break
			}
			p.record(domain.Add, int64(smpl.Add))
			p.record(domain.Sub, int64(smpl.Sub))
			p.snap.valid = false

		case <-tk.C:
			p.rotate()
			p.snap.valid = false

		case <-p.snapCh:
			if !p.snap.valid && !time.Now().Before(p.snap.deadline) {
				p.makeSnapshot()
			}
			p.snapCh <- struct{}{}
		}
	}
}

func (p *Aggregator) CountRequests(kind domain.RequestKind, n int) {
	p.cnts[kind].Add(int64(n))
}

// GetSnapshot возвращает текущий RO снимок метрик посредством обратного вызова.
// Снимок действителен только во время обратного вызова. Вызывающий код не должен изменять снимок.
// GetSnapshot не блокирует учет текущих метрик, но использует внутренним мьютекс.
// Только один вызов возможен в одно время. Поэтому fn должна отпустить снимок, как можно быстрее.
// Повторный вызов GetSnapshot из fn вызывает самоблокировку.
func (p *Aggregator) GetSnapshot(fn func(snap *domain.Snapshot)) {
	p.snapMu.Lock()
	defer p.snapMu.Unlock()

	select {
	case p.snapCh <- struct{}{}:
		<-p.snapCh
	case <-p.done:
		if !p.snap.valid {
			p.makeSnapshot()
		}
	}

	fn(&p.snap.Snapshot)
}

func (p *Aggregator) Done() <-chan struct{} {
	return p.done
}
