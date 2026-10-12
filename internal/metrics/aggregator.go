package metrics

import (
	"log"
	"math"
	"math/bits"
	"sync"
	"sync/atomic"
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/pools"
	"aaa2ppp/cdnnow-test-golang-14/internal/queue"

	"github.com/HdrHistogram/hdrhistogram-go"
)

const histMinValue = 1 * time.Microsecond
const histMaxValue = 100 * time.Millisecond
const histBuckets = 60
const rotateInterval = time.Second
const snapshotTTL = 10 * time.Millisecond // makeSnapshot() 30-100us

type Sample struct {
	AddDuration time.Duration
	SubDuration time.Duration
}

type snapshot struct {
	Version    uint64
	Histograms [durationKindSize]*hdrhistogram.Histogram
	Requests   [requestKindSize][]uint64
	deadline   time.Time
	valid      bool
}

//go:generate enumer -type RequestKind
type RequestKind uint8

const (
	Ok RequestKind = iota
	Overload
	BadRequest
	Failed

	requestKindSize // держать последним
)

//go:generate enumer -type DurationKind
type DurationKind uint8

const (
	Add DurationKind = iota
	Sub

	durationKindSize // держать последним
)

type durationsMsg struct {
	kind DurationKind
	vals []time.Duration
}

type histOverflow struct {
	count   int64
	sumLog2 int64
	max_    int64
}

// Для оценки абсолютная точность не требуется
func log2u(x uint64) int64 {
	return int64(bits.Len64(x) - 1)
}

func (ho *histOverflow) add(x time.Duration) {
	ho.count++
	ho.sumLog2 += log2u(uint64(x))
	ho.max_ = max(ho.max_, int64(x))
}

func (ho *histOverflow) max() time.Duration {
	return time.Duration(ho.max_)
}

// При count=0 не имеет смысла, возвращает 0
func (ho *histOverflow) geomean() time.Duration {
	if ho.count == 0 {
		return 0
	}
	meanLog2 := float64(ho.sumLog2) / float64(ho.count)
	return time.Duration(math.Round(math.Exp2(meanLog2)))
}

type Aggregator struct {
	hists         [durationKindSize]*hdrhistogram.WindowedHistogram
	reqs          [requestKindSize]*queue.Deque[uint64]
	cnts          [requestKindSize]atomic.Uint64
	over          histOverflow
	sampleCh      chan Sample
	samplesCh     chan []Sample
	samplesPool   *pools.BatchPool[Sample]
	durationsCh   chan durationsMsg
	durationsPool *pools.BatchPool[time.Duration]
	snapMu        sync.Mutex
	snap          snapshot
	snapCh        chan struct{}
	stop          chan struct{}
	done          chan struct{}
}

func NewAggregator(queueSize int, samplesPool *pools.BatchPool[Sample], durationsPool *pools.BatchPool[time.Duration]) *Aggregator {
	a := &Aggregator{
		sampleCh:      make(chan Sample, queueSize),
		samplesCh:     make(chan []Sample, queueSize),
		samplesPool:   samplesPool,
		durationsCh:   make(chan durationsMsg, queueSize),
		durationsPool: durationsPool,
		snapCh:        make(chan struct{}),
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
	}

	for i := range a.hists {
		a.hists[i] = hdrhistogram.NewWindowed(histBuckets, histMinValue.Nanoseconds(), histMaxValue.Nanoseconds(), 3)
	}

	for i := range a.reqs {
		a.reqs[i] = queue.NewDequeFrom(make([]uint64, histBuckets))
		a.snap.Requests[i] = make([]uint64, histBuckets)
	}

	go a.serve()
	return a
}

func (a *Aggregator) rotate() {
	if a.over.count > 0 {
		log.Printf("Aggregator.rotate: duration overflow: count=%d, max=%v, geomean=%v", a.over.count, a.over.max(), a.over.geomean())
		a.over = histOverflow{}
	}
	for kind, q := range a.reqs {
		q.PopFront()
		q.PushBack(a.cnts[kind].Swap(0))
	}
	for _, h := range a.hists {
		h.Rotate()
	}
}

func (a *Aggregator) recordDuration(hist *hdrhistogram.WindowedHistogram, duration time.Duration) {
	if err := hist.Current.RecordValue(duration.Nanoseconds()); err != nil {
		a.over.add(duration)
	}
}

func (a *Aggregator) recordDurations(hist *hdrhistogram.WindowedHistogram, durations []time.Duration) {
	for _, duration := range durations {
		a.recordDuration(hist, duration)
	}
}

func (a *Aggregator) recordSamples(batch []Sample) {
	for _, sample := range batch {
		a.recordDuration(a.hists[Add], sample.AddDuration)
		a.recordDuration(a.hists[Sub], sample.SubDuration)
	}
}

func (a *Aggregator) closeChannels() {
	close(a.sampleCh)
	close(a.samplesCh)
	close(a.durationsCh)
	close(a.done)
}

func (a *Aggregator) makeSnapshot() {
	a.snap.Version++
	for kind, hist := range a.hists {
		a.snap.Histograms[kind] = hist.Merge()
	}
	for kind := range a.reqs {
		buf := a.snap.Requests[kind]
		a.snap.Requests[kind] = a.reqs[kind].ToSlice(buf)
	}
	a.snap.deadline = time.Now().Add(snapshotTTL)
	a.snap.valid = true
}

func (a *Aggregator) serve() {
	defer a.closeChannels()

	rotateTk := time.NewTicker(rotateInterval)
	defer rotateTk.Stop()

	for {
		select {
		case <-rotateTk.C:
			a.rotate()
			a.snap.valid = false

		case sample := <-a.sampleCh:
			a.recordDuration(a.hists[Add], sample.AddDuration)
			a.recordDuration(a.hists[Sub], sample.SubDuration)
			a.snap.valid = false

		case samples := <-a.samplesCh:
			a.recordSamples(samples)
			a.snap.valid = false
			if a.samplesPool != nil {
				a.samplesPool.Put(samples)
			}

		case msg := <-a.durationsCh:
			a.recordDurations(a.hists[msg.kind], msg.vals)
			a.snap.valid = false
			if a.durationsPool != nil {
				a.durationsPool.Put(msg.vals)
			}

		case <-a.snapCh:
			now := time.Now()
			if !a.snap.valid && !now.Before(a.snap.deadline) {
				a.makeSnapshot()
			}
			a.snapCh <- struct{}{}

		case <-a.stop:
			return
		}
	}
}

// RecordSample записывает семпл. После Stop паникует.
func (a *Aggregator) RecordSample(sample Sample) {
	a.sampleCh <- sample
}

// RecordSamples записывает партию семплов. После Stop паникует.
func (a *Aggregator) RecordSamples(samples []Sample) {
	a.samplesCh <- samples
}

// RecordDurations записывает продолжительность вызовов kind финкции. После Stop паникует.
func (a *Aggregator) RecordDurations(kind DurationKind, durations []time.Duration) {
	a.durationsCh <- durationsMsg{kind, durations}
}

// CountRequests подсчитывает количество запросов.
func (a *Aggregator) CountRequests(kind RequestKind, n uint64) {
	a.cnts[kind].Add(n)
}

// getSnapshot возвращает текущий ro снимок метрик посредством обратного вызова.
// Снимок действителен только во время обратного вызова.
// fn вызывается под внутренним мьютексом. Из fn нельзя повторно вызывать getSnapshot.
// Это вызовет самоблокировку.
func (a *Aggregator) getSnapshot(fn func(snap *snapshot)) {
	a.snapMu.Lock()
	defer a.snapMu.Unlock()

	select {
	case a.snapCh <- struct{}{}:
		<-a.snapCh
	case <-a.done:
		if !a.snap.valid {
			a.makeSnapshot()
		}
	}

	fn(&a.snap)
}

// Stop останавливает сбор метрик. Паникует при повторном вызове.
func (a *Aggregator) Stop() {
	close(a.stop)
	<-a.done
}
