package metrics

import (
	"log"
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/pools"
	"aaa2ppp/cdnnow-test-golang-14/internal/queue"

	"github.com/HdrHistogram/hdrhistogram-go"
)

const histMinValue = 1 * time.Microsecond
const histMaxValue = 10 * time.Millisecond
const histBuckets = 60
const rotateInterval = time.Second

type Sample struct {
	AddDuration time.Duration
	SubDuration time.Duration
}

type Snapshot struct {
	AddHist *hdrhistogram.Histogram
	SubHist *hdrhistogram.Histogram
	RPS     []uint64
}

type Aggregator struct {
	addWHist       *hdrhistogram.WindowedHistogram
	subWHist       *hdrhistogram.WindowedHistogram
	rps            *queue.Deque[uint64]
	sampleCh       chan Sample
	samplesCh      chan []Sample
	samplesPool    *pools.BatchPool[Sample]
	addDurationsCh chan []time.Duration
	subDurationsCh chan []time.Duration
	durationsPool  *pools.BatchPool[time.Duration]
	countCh        chan uint64
	getSnapCh      chan chan Snapshot
	stop           chan struct{}
	done           chan struct{}
}

func NewAggregator(queueSize int, samplesPool *pools.BatchPool[Sample], durationsPool *pools.BatchPool[time.Duration]) *Aggregator {
	a := &Aggregator{
		addWHist:       hdrhistogram.NewWindowed(histBuckets, histMinValue.Nanoseconds(), histMaxValue.Nanoseconds(), 3),
		subWHist:       hdrhistogram.NewWindowed(histBuckets, histMinValue.Nanoseconds(), histMaxValue.Nanoseconds(), 3),
		rps:            queue.NewDequeFrom(make([]uint64, histBuckets)),
		sampleCh:       make(chan Sample, queueSize),
		samplesCh:      make(chan []Sample, queueSize),
		samplesPool:    samplesPool,
		addDurationsCh: make(chan []time.Duration, queueSize),
		subDurationsCh: make(chan []time.Duration, queueSize),
		durationsPool:  durationsPool,
		countCh:        make(chan uint64, queueSize),
		getSnapCh:      make(chan chan Snapshot),
		stop:           make(chan struct{}),
		done:           make(chan struct{}),
	}

	go a.serve()
	return a
}

func (a *Aggregator) rotate() {
	a.rps.PopFront()
	a.rps.PushBack(0)
	a.addWHist.Rotate()
	a.subWHist.Rotate()
}

func (a *Aggregator) countRPS(n uint64) {
	a.rps.PushBack(a.rps.PopBack() + n)
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
		a.recordDuration(a.addWHist, sample.AddDuration)
		a.recordDuration(a.subWHist, sample.SubDuration)
	}
}

func (a *Aggregator) closeChannels() {
	close(a.sampleCh)
	close(a.samplesCh)
	close(a.addDurationsCh)
	close(a.subDurationsCh)
	close(a.countCh)
	close(a.getSnapCh)
	close(a.done)
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
			a.recordDuration(a.addWHist, sample.AddDuration)
			a.recordDuration(a.subWHist, sample.SubDuration)

		case samples := <-a.samplesCh:
			a.recordSamples(samples)
			if a.samplesPool != nil {
				a.samplesPool.Put(samples)
			}

		case n := <-a.countCh:
			a.countRPS(n)

		case durations := <-a.addDurationsCh:
			a.recordDurations(a.addWHist, durations)
			if a.durationsPool != nil {
				a.durationsPool.Put(durations)
			}

		case durations := <-a.subDurationsCh:
			a.recordDurations(a.subWHist, durations)
			if a.durationsPool != nil {
				a.durationsPool.Put(durations)
			}

		case snapCh := <-a.getSnapCh:
			snapCh <- Snapshot{
				AddHist: a.addWHist.Merge(),
				SubHist: a.subWHist.Merge(),
				RPS:     a.rps.ToSlice(),
			}

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

// RecordAddDurations записывает продолжительность вызовов Add финкции. После Stop паникует.
func (a *Aggregator) RecordAddDurations(durations []time.Duration) {
	a.addDurationsCh <- durations
}

// RecordSubDurations записывает продолжительность вызовов Sub финкции. После Stop паникует.
func (a *Aggregator) RecordSubDurations(durations []time.Duration) {
	a.subDurationsCh <- durations
}

// CountRequests подсчитывает количество запросов. После Stop паникует.
func (a *Aggregator) CountRequests(n uint64) {
	a.countCh <- n
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
