package metrics

import (
	"context"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/pools"
)

const tick = time.Millisecond

func waitDone(t testing.TB, d time.Duration, done chan struct{}, msg string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(d):
		t.Error(msg)
	}
}

func waitFunc(t testing.TB, d time.Duration, fn func(), msg string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	waitDone(t, d, done, msg)
}

func Test_getSnapshot(t *testing.T) {
	const batchSize = 256

	poolD := pools.NewBatchPool[time.Duration](batchSize)
	poolS := pools.NewBatchPool[Sample](batchSize)
	a := NewAggregator(16, poolS, poolD)

	getCtx, stopGet := context.WithCancel(context.Background())
	defer stopGet()
	var getWG sync.WaitGroup

	for i := 0; i < 10; i++ {
		getWG.Go(func() {
			for getCtx.Err() == nil {
				a.getSnapshot(func(snap *snapshot) {
					n := snap.Requests[0][0]
					p := snap.Histograms[0].ValueAtPercentile(0.95)
					_, _ = n, p
				})
			}
		})
	}

	time.Sleep(100 * tick)

	recCtx, stopRec := context.WithCancel(context.Background())
	defer stopRec()
	var recWG sync.WaitGroup

	recWG.Go(func() {
		for recCtx.Err() == nil {
			a.CountRequests(0, 1)
		}
	})

	recWG.Go(func() {
		for recCtx.Err() == nil {
			batch := poolD.Get()[:batchSize]
			for i := range batch {
				batch[i] = time.Duration(rand.Int64N(int64(time.Millisecond)))
			}
			a.RecordDurations(0, batch)
		}
	})

	recWG.Go(func() {
		for recCtx.Err() == nil {
			batch := poolS.Get()[:batchSize]
			for i := range batch {
				batch[i] = Sample{
					time.Duration(rand.Int64N(int64(time.Millisecond))),
					time.Duration(rand.Int64N(int64(time.Millisecond))),
				}
			}
			a.RecordSamples(batch)
		}
	})

	recWG.Go(func() {
		for recCtx.Err() == nil {
			a.RecordSample(Sample{
				time.Duration(rand.Int64N(int64(time.Millisecond))),
				time.Duration(rand.Int64N(int64(time.Millisecond))),
			})
		}
	})

	time.Sleep(100 * tick)
	stopRec()
	waitFunc(t, 10*tick, recWG.Wait, "recording blocked")
	waitFunc(t, 10*tick, a.Stop, "Stop blocked")

	time.Sleep(100 * tick)
	stopGet()
	waitFunc(t, 10*tick, getWG.Wait, "getting blocked")
}

func Benchmark_fillBatch_1000(b *testing.B) {
	d := make([]time.Duration, 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := range d {
			d[j] = 42
		}
		_ = d
	}
}

func BenchmarkRecordDurations_1000(b *testing.B) {
	a := NewAggregator(16, nil, nil)
	d := make([]time.Duration, 1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.RecordDurations(0, d)
	}
	b.StopTimer()

	waitFunc(b, 10*tick, a.Stop, "blocked")
}

func BenchmarkRecordDurations_1000_pool(b *testing.B) {
	a := NewAggregator(16, nil, nil)
	p := pools.NewBatchPool[time.Duration](1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d := p.Get()[:1000]
		a.RecordDurations(0, d)
	}
	b.StopTimer()

	waitFunc(b, 10*tick, a.Stop, "blocked")
}

func BenchmarkRecordDurations_1(b *testing.B) {
	a := NewAggregator(16, nil, nil)
	d := make([]time.Duration, 1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.RecordDurations(0, d)
	}
	b.StopTimer()

	waitFunc(b, 10*tick, a.Stop, "blocked")
}

func Benchmark_recordDuration(b *testing.B) {
	a := NewAggregator(16, nil, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.recordDuration(a.hists[0], 0)
	}
	b.StopTimer()

	waitFunc(b, 10*tick, a.Stop, "blocked")
}
