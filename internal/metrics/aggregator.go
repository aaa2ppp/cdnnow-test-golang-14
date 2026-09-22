package metrics

import (
	"log"
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/pools"
	"aaa2ppp/cdnnow-test-golang-14/internal/queue"

	"github.com/HdrHistogram/hdrhistogram-go"
)

const histMinValue = 1 * time.Microsecond
const histMaxValue = 100 * time.Millisecond
const histBuckets = 60
const rotateInterval = time.Second

type Sample struct {
	AddDuration time.Duration
	SubDuration time.Duration
}

type Snapshot struct {
	Histograms [durationKindSize]*hdrhistogram.Histogram
	Requests   [requestKindSize][]uint64
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

type Aggregator struct {
	hists         [durationKindSize]*hdrhistogram.WindowedHistogram
	reqs          [requestKindSize]*queue.Deque[uint64]
	sampleCh      chan Sample
	samplesCh     chan []Sample
	samplesPool   *pools.BatchPool[Sample]
	durationsCh   chan durationsMsg
	durationsPool *pools.BatchPool[time.Duration]
	reqsCh        chan requestsMsg
	getSnapCh     chan chan Snapshot
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
		reqsCh:        make(chan requestsMsg, queueSize),
		getSnapCh:     make(chan chan Snapshot),
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
	}

	for i := range a.hists {
		a.hists[i] = hdrhistogram.NewWindowed(histBuckets, histMinValue.Nanoseconds(), histMaxValue.Nanoseconds(), 3)
	}

	for i := range a.reqs {
		a.reqs[i] = queue.NewDequeFrom(make([]uint64, histBuckets))
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

func (a *Aggregator) recordDuration(hist *hdrhistogram.WindowedHistogram, duration time.Duration) {
	if err := hist.Current.RecordValue(duration.Nanoseconds()); err != nil {
		log.Printf("Aggregator.recordDuration: %v", err)
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
	close(a.reqsCh)
	close(a.getSnapCh)
	close(a.done)
}

func (a *Aggregator) makeSnapshot() Snapshot {
	var snap Snapshot
	for kind := range a.hists {
		snap.Histograms[kind] = a.hists[kind].Merge()
	}
	for kind := range a.reqs {
		snap.Requests[kind] = a.reqs[kind].ToSlice()
	}
	return snap
}

func (a *Aggregator) serve() {
	defer a.closeChannels()

	rotateTk := time.NewTicker(rotateInterval)
	defer rotateTk.Stop()

	for {
		select {
		case <-rotateTk.C:
			a.rotate()

		case sample := <-a.sampleCh:
			a.recordDuration(a.hists[Add], sample.AddDuration)
			a.recordDuration(a.hists[Sub], sample.SubDuration)

		case samples := <-a.samplesCh:
			a.recordSamples(samples)
			if a.samplesPool != nil {
				a.samplesPool.Put(samples)
			}

		case msg := <-a.reqsCh:
			a.countReqs(msg.kind, msg.n)

		case msg := <-a.durationsCh:
			a.recordDurations(a.hists[msg.kind], msg.vals)
			if a.durationsPool != nil {
				a.durationsPool.Put(msg.vals)
			}

		case snapCh := <-a.getSnapCh:
			snapCh <- a.makeSnapshot()

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

// CountRequests подсчитывает количество запросов. После Stop паникует.
func (a *Aggregator) CountRequests(kind RequestKind, n uint64) {
	a.reqsCh <- requestsMsg{kind, n}
}

// GetSnapshot возвращает текущий срез метрик. После Stop паникует.
func (a *Aggregator) GetSnapshot() Snapshot {
	snapCh := make(chan Snapshot, 1)
	a.getSnapCh <- snapCh
	return <-snapCh
}

// Stop останавливает сбор метрик. Паникует при повторном вызове.
func (a *Aggregator) Stop() {
	close(a.stop)
	<-a.done
}
