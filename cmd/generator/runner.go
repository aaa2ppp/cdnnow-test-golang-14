package main

import (
	"context"
	"math/rand/v2"
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/generator/clients"
	"aaa2ppp/cdnnow-test-golang-14/internal/generator/workers"
)

type RunConfig struct {
	Logger      workers.Logger
	Jitter      time.Duration
	Interval    time.Duration
	Target      clients.Target
	Timeout     time.Duration
	NoKeepAlive bool
}

func startGenerator(ctx context.Context) <-chan int {
	nums := make(chan int)

	go func() {
		defer close(nums)

		r := rand.New(rand.NewPCG(42, uint64(time.Now().UnixNano())))
		for {
			select {
			case nums <- r.IntN(201) - 100:
			case <-ctx.Done():
				return
			}
		}
	}()

	return nums
}

func startWorkers(ctx context.Context, n int, cfg RunConfig) <-chan *workers.Stat {
	done := make(chan *workers.Stat, n)
	input := startGenerator(ctx)

	for i := 0; i < n; i++ {
		go func(i int) {
			client := clients.NewSingle(clients.Config{
				Target:      cfg.Target,
				Timeout:     cfg.Timeout,
				NoKeepAlive: cfg.NoKeepAlive,
			})

			worker := workers.Worker{
				Client:   client,
				Logger:   cfg.Logger,
				Interval: cfg.Interval,
			}

			if cfg.Jitter > 0 {
				if err := randomSleep(ctx, cfg.Jitter); err != nil {
					done <- nil
					return
				}
			}

			done <- worker.Run(ctx, input)
		}(i)
	}

	return done
}

func waitWorkers(done <-chan *workers.Stat, timeout time.Duration) (*workers.Stat, int) {
	tm := time.NewTimer(timeout)
	defer tm.Stop()

	merged := workers.NewStat()
	n := cap(done)

	for i := 0; i < n; i++ {
		select {
		case stat := <-done:
			merged.Merge(stat)
		case <-tm.C:
			return merged, i
		}
	}

	return merged, n
}

func randomSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	d = time.Duration(rand.Int64N(int64(d)))
	tm := time.NewTimer(d)
	select {
	case <-ctx.Done():
		tm.Stop()
		return ctx.Err()
	case <-tm.C:
		return nil
	}
}
