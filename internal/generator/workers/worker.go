package workers

import (
	"context"
	"time"
)

type Client interface {
	DoRequest(num int) (latency time.Duration, _ error)
}

type Logger interface {
	Printf(format string, v ...any)
}

type Worker struct {
	Client   Client
	Logger   Logger
	Interval time.Duration
}

func (w Worker) Run(ctx context.Context, nums <-chan int) *Stat {
	stat := NewStat()

	var tm *time.Timer
	var num int
	var ok bool

	for {
		select {
		case num, ok = <-nums:
		case <-ctx.Done():
			return stat
		}

		if !ok {
			return stat
		}

		latency, err := w.Client.DoRequest(num)

		if err != nil {
			stat.Errors++
			w.Logger.Printf("request failed: %v", err)
		} else {
			stat.Ok++
			if err := stat.Hist.RecordValue(int64(latency)); err != nil {
				stat.Overflow.add(latency)
			}
		}

		if w.Interval <= 0 {
			continue
		}

		if tm == nil {
			tm = time.NewTimer(w.Interval)
		} else {
			tm.Reset(w.Interval)
		}

		select {
		case <-tm.C:
		case <-ctx.Done():
			tm.Stop()
			return stat
		}
	}
}
