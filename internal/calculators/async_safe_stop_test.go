package calculators

import (
	"aaa2ppp/cdnnow-test-golang-14/internal/operators"
	"errors"
	"sync"
	"testing"
	"time"
)

func newTestAsync(t *testing.T) *Async {
	t.Helper()
	return NewAsync(1024, nil, nil)
}

func newTestAsyncSingle(t *testing.T) *asyncSingle {
	t.Helper()
	return newAsyncSingle(operators.AddOp(), 1024, nil, nil)
}

type stopCalculator interface {
	Calculate(int64) error
	Stop()
}

func testStopBehavior(t *testing.T, c stopCalculator) {
	t.Helper()

	// Повторный Stop не паникует и не блокируется.
	c.Stop()
	done := make(chan struct{})
	go func() {
		c.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("second Stop blocked")
	}

	// Calculate после Stop возвращает ErrStopped.
	if err := c.Calculate(1); !errors.Is(err, ErrStopped) {
		t.Fatalf("Calculate after Stop: got %v, want ErrStopped", err)
	}
}

func TestAsync_Stop(t *testing.T) {
	c := newTestAsync(t)
	testStopBehavior(t, c)
}

func TestAsyncSingle_Stop(t *testing.T) {
	c := newTestAsyncSingle(t)
	testStopBehavior(t, c)
}

func TestAsync_ValuesAfterStop(t *testing.T) {
	c := newTestAsync(t)
	c.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Values()
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Values blocked after Stop")
	}
}

func TestAsyncSingle_ValueAfterStop(t *testing.T) {
	c := newTestAsyncSingle(t)
	c.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Value()
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Value blocked after Stop")
	}
}

func TestAsync_ConcurrentStopAndCalculate(t *testing.T) {
	c := newTestAsync(t)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = c.Calculate(1)
				}
			}
		}()
	}

	time.Sleep(10 * time.Millisecond)
	c.Stop()
	close(stop)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent Calculate/Stop blocked")
	}
}

func TestAsyncSingle_ConcurrentStopAndCalculate(t *testing.T) {
	c := newTestAsyncSingle(t)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = c.Calculate(1)
				}
			}
		}()
	}

	time.Sleep(10 * time.Millisecond)
	c.Stop()
	close(stop)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent Calculate/Stop blocked")
	}
}
