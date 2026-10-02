package main

import (
	"aaa2ppp/cdnnow-test-golang-14/internal/generator/clients"
	"aaa2ppp/cdnnow-test-golang-14/internal/generator/workers"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"
)

// TODO: main.go распух. Пора резать на файлы/пакеты

const shutdownTimeout = 2 * time.Second

func usage(msg string) {
	out := flag.CommandLine.Output()
	_, _ = fmt.Fprintf(out, "%s\nUsage %s:\n", msg, os.Args[0])
	flag.PrintDefaults()
	os.Exit(1)
}

type Stat struct {
	Skipped int
	Ok      int64
	Errors  int64
	Hist    *hdrhistogram.Histogram
	Dropped int64
}

func main() {
	var (
		baseURL     string
		threads     int
		intervalSec float64
		timeoutSec  float64
		noKeepAlive bool
		percentiles bool
		duration    time.Duration
		jitter      time.Duration
	)
	flag.StringVar(&baseURL, "url", "http://localhost:8080/calc", "calculator endpoint")
	flag.IntVar(&threads, "n", 10, "alias for threads")
	flag.IntVar(&threads, "threads", 10, "number of worker threads")
	flag.Float64Var(&intervalSec, "interval", 0.1, "pause between requests per thread, in seconds (0 = as fast as possible)")
	flag.Float64Var(&timeoutSec, "timeout", 5.0, "HTTP request timeout, seconds")
	flag.BoolVar(&noKeepAlive, "no-keep-alive", false, "new connection will be created for each request")
	flag.BoolVar(&percentiles, "p", false, "show latency percentiles")
	flag.DurationVar(&duration, "d", 0, "total duration of the load test, e.g., 10m, 1h (default 0 - unlimited)")
	flag.DurationVar(&jitter, "j", 0, "maximum random delay at the start of the worker, e.g., 100ms, 1s")
	flag.Parse()

	if threads <= 0 {
		usage("threads must be > 0")
	}
	if duration < 0 {
		usage("duration must be >= 0")
	}
	if jitter < 0 {
		usage("jitter must be >= 0")
	}

	interval, err := secToDuration(intervalSec)
	if err != nil || interval < 0 {
		usage("interval must be >= 0")
	}

	timeout, err := secToDuration(timeoutSec)
	if err != nil || timeout <= 0 {
		usage("timeout must be > 0")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	exclude := []string{"num"}
	target, err := clients.ParseTarget(baseURL, exclude)
	if err != nil {
		log.Fatalf("parse %s: %v", baseURL, err)
	}

	if err := ProbeTarget(ctx, target, 3, 3*time.Second, timeout); err != nil {
		log.Fatalf("probe %s: %v", target.BaseURL, err)
	}

	if duration > 0 {
		toCtx, cancel := context.WithTimeout(ctx, duration)
		defer cancel()
		ctx = toCtx
	}

	ctx, abort := context.WithCancelCause(ctx)
	defer abort(context.Canceled)

	done := startWorkers(ctx, threads, abort, Config{
		// worker config
		interval: interval,
		jitter:   jitter,

		// client config
		target:      target,
		timeout:     timeout,
		noKeepAlive: noKeepAlive,
	})

	log.Printf("Generator started: %d threads -> %s", threads, target.BaseURL)

	<-ctx.Done()
	log.Printf("Shutdown: %v, stopping generator...", context.Cause(ctx))

	stat := waitWorkers(done, shutdownTimeout)

	if stat.Skipped > 0 {
		log.Printf("skipped %d worker statistics", stat.Skipped)
	}
	_, _ = fmt.Printf("\nTotal requests: ok=%d errors=%d\n", stat.Ok, stat.Errors)

	if percentiles {
		if stat.Dropped > 0 {
			log.Printf("hist.Merge: total dropped %d values", stat.Dropped)
		}
		if stat.Hist != nil {
			_, _ = fmt.Println()
			_, _ = stat.Hist.PercentilesPrint(os.Stdout, 1, float64(time.Microsecond))
		}
	}
}

func secToDuration(sec float64) (time.Duration, error) {
	if math.IsNaN(sec) || math.IsInf(sec, 0) {
		return 0, errors.New("canot be NaN or Inf")
	}
	t := sec * float64(time.Second)
	if t < math.MinInt64 || t >= math.MaxInt64 {
		return 0, errors.New("absolute value too large")
	}
	return time.Duration(t), nil
}

func randomSleep(ctx context.Context, d time.Duration) error {
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

type Config struct {
	// worker config
	jitter   time.Duration
	interval time.Duration

	// client config
	target      *clients.Target
	timeout     time.Duration
	noKeepAlive bool
}

func startWorkers(ctx context.Context, threads int, abort func(error), cfg Config) <-chan workers.Stat {
	done := make(chan workers.Stat, threads)

	for i := 0; i < threads; i++ {
		go func(i int) {
			client := clients.NewSingle(clients.Config{
				BaseURL:     cfg.target.BaseURL,
				DialAddr:    cfg.target.DialAddr,
				UserInfo:    cfg.target.UserInfo,
				Timeout:     cfg.timeout,
				NoKeepAlive: cfg.noKeepAlive,
			})
			worker := &workers.Worker{
				ID:       i + 1,
				Client:   client,
				Interval: cfg.interval,
			}
			stat, err := worker.Run(ctx)
			if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
				abort(err)
			}
			done <- stat
		}(i)
	}

	return done
}

func waitWorkers(done <-chan workers.Stat, timeout time.Duration) Stat {
	tm := time.NewTimer(timeout)
	defer tm.Stop()

	var merged Stat
	merged.Skipped = cap(done)

	for i := 0; i < cap(done); i++ {
		select {
		case <-tm.C:
			return merged

		case stat := <-done:
			merged.Skipped--
			merged.Ok += stat.Ok
			merged.Errors += stat.Errors

			if stat.Hist != nil {
				if merged.Hist == nil {
					merged.Hist = stat.Hist
				} else {
					merged.Dropped += merged.Hist.Merge(stat.Hist)
				}
			}
		}
	}

	return merged
}

func ProbeTarget(ctx context.Context, t *clients.Target, retries int, interval, timeout time.Duration) error {
	c := clients.NewSingle(clients.Config{
		BaseURL:     t.BaseURL,
		DialAddr:    t.DialAddr,
		UserInfo:    t.UserInfo,
		Timeout:     timeout,
		NoKeepAlive: true,
	})

	var err error
	done := make(chan error, 1)
	var tm *time.Timer

	for i := 0; i < retries; i++ {
		if i > 0 {
			if tm == nil {
				tm = time.NewTimer(interval)
			} else {
				tm.Reset(interval)
			}
			select {
			case <-ctx.Done():
				tm.Stop()
				return ctx.Err()
			case <-tm.C:
			}
		}

		go func() {
			done <- c.Probe()
		}()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case err = <-done:
		}
		if err == nil {
			return nil
		}
	}

	return err
}
