package metrics

import (
	"sync"
	"testing"
	"time"
)

func TestAggregator_Stop_Idempotent(t *testing.T) {
	a := NewAggregator(16, nil, nil)
	a.Stop()
	a.Stop()
	a.Stop()
}

func TestAggregator_RecordAfterStop_NoPanicNoBlock(t *testing.T) {
	a := NewAggregator(16, nil, nil)
	a.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		a.RecordSample(Sample{})
		a.RecordSamples([]Sample{{}})
		a.RecordDurations(0, []time.Duration{0})
		a.CountRequests(0, 1)
		a.getSnapshot(func(snap *snapshot) {})
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("blocked after Stop")
	}
}

func TestAggregator_GetSnapshotAfterStop(t *testing.T) {
	a := NewAggregator(16, nil, nil)
	a.RecordSample(Sample{})
	a.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		a.getSnapshot(func(snap *snapshot) {})
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("GetSnapshot blocked after Stop")
	}
}

func TestAggregator_ConcurrentStopAndRecord(t *testing.T) {
	a := NewAggregator(16, nil, nil)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var k uint
			for {
				select {
				case <-stop:
					return
				default:
					a.RecordSample(Sample{})
					a.RecordSamples([]Sample{{}})
					a.RecordDurations(DurationKind(k)%durationKindSize, []time.Duration{0})
					a.CountRequests(RequestKind(k)%requestKindSize, 1)
				}
				k++
			}
		}()
	}

	var x int
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					a.getSnapshot(func(snap *snapshot) { x = x + 1 })
				}
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(100 * time.Millisecond)
		a.Stop()
		time.Sleep(100 * time.Millisecond)
		close(stop)
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent Record/Stop blocked")
	}

	_ = x
}

func Benchmark_recordDuration(b *testing.B) {
	a := NewAggregator(0, nil, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.recordDuration(0, 1)
	}
	b.StopTimer()
	a.Stop()
}

//go:noinline
func recordToSlice(s []time.Duration, i int, v time.Duration) {
	s[i%len(s)] = v
}

func Benchmark_recordDurationToSlice(b *testing.B) {
	s := make([]time.Duration, 1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		recordToSlice(s, i, time.Duration(i))
	}
	_ = s
}

func BenchmarkRecordSample(b *testing.B) {
	a := NewAggregator(0, nil, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.RecordSample(Sample{})
	}
	a.Stop()
}

func BenchmarkCountRequests(b *testing.B) {
	a := NewAggregator(0, nil, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.CountRequests(0, 1)
	}
	a.Stop()
}

func BenchmarkRecordSamples(b *testing.B) {
	a := NewAggregator(0, nil, nil)

	samples := make([]Sample, 1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.RecordSamples(samples)
	}
	a.Stop()
}

func BenchmarkMakeSnapshot(b *testing.B) {
	a := NewAggregator(0, nil, nil)
	samples := []Sample{{time.Millisecond, time.Millisecond}}
	a.RecordSamples(samples)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.makeSnapshot()
	}
	a.Stop()
}
