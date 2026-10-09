package loggers

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/aaa2ppp/be"
)

type mockLogger struct {
	b []string
}

func (m *mockLogger) Print(a ...any) {
	m.b = append(m.b, fmt.Sprint(a...))
}

func (m *mockLogger) Printf(f string, a ...any) {
	m.b = append(m.b, fmt.Sprintf(f, a...))
}

func TestDedup(t *testing.T) {
	t.Run("one", func(t *testing.T) {
		m := &mockLogger{}
		lg := NewDedup(m, 0, 0)
		defer lg.Close()
		lg.Printf("message")
		runtime.Gosched()
		be.Equal(t, len(m.b), 1)
		be.Equal(t, m.b[0], "message")
	})

	t.Run("one+1 (timeout)", func(t *testing.T) {
		m := &mockLogger{}
		lg := NewDedup(m, 0, 1*time.Millisecond)
		defer lg.Close()
		lg.Printf("message")
		lg.Printf("message")
		time.Sleep(10 * time.Millisecond)
		be.Equal(t, len(m.b), 2)
		be.Equal(t, m.b, []string{"message", "message (+1 more)"})
	})

	t.Run("one+1 (close)", func(t *testing.T) {
		m := &mockLogger{}
		lg := NewDedup(m, 0, 1*time.Millisecond)
		defer lg.Close()
		lg.Printf("message")
		lg.Printf("message")
		lg.Close()
		be.Equal(t, len(m.b), 2)
		be.Equal(t, m.b, []string{"message", "message (+1 more)"})
	})

	t.Run("one+one", func(t *testing.T) {
		m := &mockLogger{}
		lg := NewDedup(m, 0, 1*time.Millisecond)
		defer lg.Close()
		lg.Printf("message")
		time.Sleep(10 * time.Millisecond)
		lg.Printf("message")
		time.Sleep(10 * time.Millisecond)
		be.Equal(t, len(m.b), 2)
		be.Equal(t, m.b, []string{"message", "message"})
	})

	t.Run("print after close", func(t *testing.T) {
		m := &mockLogger{}
		lg := NewDedup(m, 0, 1*time.Millisecond)
		lg.Close()
		done := make(chan struct{})
		go func() { defer close(done); lg.Printf("message") }()
		select {
		case <-time.After(10 * time.Millisecond):
			t.Error("blocked")
		case <-done:
		}
		be.Equal(t, len(m.b), 0)
	})

	t.Run("close after close", func(t *testing.T) {
		m := &mockLogger{}
		lg := NewDedup(m, 0, 1*time.Millisecond)
		lg.Close()
		done := make(chan struct{})
		go func() { defer close(done); lg.Close() }()
		select {
		case <-time.After(10 * time.Millisecond):
			t.Error("blocked")
		case <-done:
		}
		be.Equal(t, len(m.b), 0)
	})

	t.Run("print after close (buffered)", func(t *testing.T) {
		m := &mockLogger{}
		lg := NewDedup(m, 10, 1*time.Millisecond)
		lg.Close()
		done := make(chan struct{})
		go func() { defer close(done); lg.Printf("message") }()
		select {
		case <-time.After(10 * time.Millisecond):
			t.Error("blocked")
		case <-done:
		}
		be.Equal(t, len(m.b), 0)
	})

	t.Run("close after close (buffered)", func(t *testing.T) {
		m := &mockLogger{}
		lg := NewDedup(m, 10, 1*time.Millisecond)
		lg.Close()
		done := make(chan struct{})
		go func() { defer close(done); lg.Close() }()
		select {
		case <-time.After(10 * time.Millisecond):
			t.Error("blocked")
		case <-done:
		}
		be.Equal(t, len(m.b), 0)
	})
}
