package metrics

import (
	"log"
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

type requestsMsg struct {
	kind RequestKind
	n    uint64
}

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

//go:generate enumer -type aggrCmdKind
type aggrCmdKind uint8

const (
	aggrCmdData aggrCmdKind = iota
	aggrCmdStop
)

type aggrCmd[T any] struct {
	kind aggrCmdKind
	data T
}

type Aggregator struct {
	hists         [durationKindSize]*hdrhistogram.WindowedHistogram
	reqs          [requestKindSize]*queue.Deque[uint64]
	sampleCh      chan aggrCmd[Sample]
	samplesCh     chan aggrCmd[[]Sample]
	samplesPool   *pools.BatchPool[Sample]
	durationsCh   chan aggrCmd[durationsMsg]
	durationsPool *pools.BatchPool[time.Duration]
	reqsCh        chan aggrCmd[requestsMsg]
	stop          chan struct{}
	done          chan struct{}
	stopMu        sync.Mutex
	stopped       atomic.Bool
	snapMu        sync.Mutex
	snap          *snapshot
	snapCh        chan struct{}
}

func NewAggregator(queueSize int, samplesPool *pools.BatchPool[Sample], durationsPool *pools.BatchPool[time.Duration]) *Aggregator {
	a := &Aggregator{
		sampleCh:      make(chan aggrCmd[Sample], queueSize),
		samplesCh:     make(chan aggrCmd[[]Sample], queueSize),
		samplesPool:   samplesPool,
		durationsCh:   make(chan aggrCmd[durationsMsg], queueSize),
		durationsPool: durationsPool,
		reqsCh:        make(chan aggrCmd[requestsMsg], queueSize),
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
		snap:          &snapshot{},
		snapCh:        make(chan struct{}),
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
	for _, q := range a.reqs {
		q.PopFront()
		q.PushBack(0)
	}
	for _, h := range a.hists {
		h.Rotate()
	}
}

func (a *Aggregator) countReqs(kind RequestKind, n uint64) {
	a.reqs[kind].PushBack(a.reqs[kind].PopBack() + n)
}

func (a *Aggregator) recordDuration(kind DurationKind, duration time.Duration) {
	hist := a.hists[kind]
	if err := hist.Current.RecordValue(duration.Nanoseconds()); err != nil {
		log.Printf("Aggregator.recordDuration: %v", err)
		return
	}
}

func (a *Aggregator) recordDurations(kind DurationKind, durations []time.Duration) {
	for _, duration := range durations {
		a.recordDuration(kind, duration)
	}
}

func (a *Aggregator) recordSamples(batch []Sample) {
	for _, sample := range batch {
		a.recordDuration(Add, sample.AddDuration)
		a.recordDuration(Sub, sample.SubDuration)
	}
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
	defer close(a.done)

	rotateTk := time.NewTicker(rotateInterval)
	defer rotateTk.Stop()

	inputs := 4
	for inputs > 0 {
		select {
		case <-rotateTk.C:
			a.rotate()
			a.snap.valid = false

		case cmd := <-a.sampleCh: // 1
			if cmd.kind == aggrCmdStop {
				a.sampleCh = nil
				inputs--
				break
			}
			a.recordDuration(Add, cmd.data.AddDuration)
			a.recordDuration(Sub, cmd.data.SubDuration)
			a.snap.valid = false

		case cmd := <-a.samplesCh: // 2
			if cmd.kind == aggrCmdStop {
				a.samplesCh = nil
				inputs--
				break
			}
			a.recordSamples(cmd.data)
			a.snap.valid = false
			if a.samplesPool != nil {
				a.samplesPool.Put(cmd.data)
			}

		case cmd := <-a.reqsCh: // 3
			if cmd.kind == aggrCmdStop {
				a.reqsCh = nil
				inputs--
				break
			}
			msg := &cmd.data
			a.countReqs(msg.kind, msg.n)
			a.snap.valid = false

		case cmd := <-a.durationsCh: // 4
			if cmd.kind == aggrCmdStop {
				a.durationsCh = nil
				inputs--
				break
			}
			msg := &cmd.data
			a.recordDurations(msg.kind, msg.vals)
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

// RecordSample записывает семпл. После Stop no-op.
func (a *Aggregator) RecordSample(sample Sample) {
	if a.stopped.Load() {
		return
	}
	a.sampleCh <- aggrCmd[Sample]{data: sample}
}

// RecordSamples записывает партию семплов. После Stop no-op.
func (a *Aggregator) RecordSamples(samples []Sample) {
	if a.stopped.Load() {
		return
	}
	a.samplesCh <- aggrCmd[[]Sample]{data: samples}
}

// RecordDurations записывает продолжительность вызовов kind финкции. После Stop no-op.
func (a *Aggregator) RecordDurations(kind DurationKind, durations []time.Duration) {
	if a.stopped.Load() {
		return
	}
	a.durationsCh <- aggrCmd[durationsMsg]{data: durationsMsg{kind, durations}}
}

// CountRequests подсчитывает количество запросов. После Stop no-op.
func (a *Aggregator) CountRequests(kind RequestKind, n uint64) {
	if a.stopped.Load() {
		return
	}
	a.reqsCh <- aggrCmd[requestsMsg]{data: requestsMsg{kind, n}}
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

	fn(a.snap)
}

// Stop останавливает сбор метрик. Безопасен при повторном вызове.
func (a *Aggregator) Stop() {
	a.stopMu.Lock()
	defer a.stopMu.Unlock()

	if a.stopped.Load() {
		<-a.done
		return
	}

	a.stopped.Store(true)
	a.sampleCh <- aggrCmd[Sample]{kind: aggrCmdStop}
	a.samplesCh <- aggrCmd[[]Sample]{kind: aggrCmdStop}
	a.durationsCh <- aggrCmd[durationsMsg]{kind: aggrCmdStop}
	a.reqsCh <- aggrCmd[requestsMsg]{kind: aggrCmdStop}
	<-a.done
}
