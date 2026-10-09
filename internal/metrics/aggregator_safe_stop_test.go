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
		a.RecordDurations(0, []time.Duration{time.Millisecond})
		a.CountRequests(0, 1)
		_ = a.getSnapshot()
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
		_ = a.getSnapshot()
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
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					a.RecordSample(Sample{})
					a.RecordSamples([]Sample{{}})
					a.RecordDurations(0, []time.Duration{time.Millisecond})
					a.CountRequests(0, 1)
					_ = a.getSnapshot()
				}
			}
		})
	}

	time.Sleep(10 * time.Millisecond)
	a.Stop()
	close(stop)

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
}
