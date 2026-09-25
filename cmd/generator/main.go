package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"
)

const histMinValue = 10 * time.Microsecond
const histMaxValue = 100 * time.Millisecond

func usage(msg string) {
	out := flag.CommandLine.Output()
	_, _ = fmt.Fprintf(out, "%s\nUsage %s:\n", msg, os.Args[0])
	flag.PrintDefaults()
	os.Exit(1)
}

type Statistics struct {
	Ok     atomic.Uint64
	Errors atomic.Uint64
}

func main() {
	var (
		url         string
		threads     int
		intervalSec float64
		timeoutSec  float64
		noKeepAlive bool
		percentiles bool
		duration    time.Duration
		jitter      time.Duration
	)
	flag.StringVar(&url, "url", "http://localhost:8080/calc", "calculator endpoint")
	flag.IntVar(&threads, "n", 10, "alias for threads")
	flag.IntVar(&threads, "threads", 10, "number of worker threads")
	flag.Float64Var(&intervalSec, "interval", 0.1, "pause between requests per thread, in seconds (0 = as fast as possible)")
	flag.Float64Var(&timeoutSec, "timeout", 5.0, "HTTP request timeout, seconds")
	flag.BoolVar(&noKeepAlive, "no-keep-alive", false, "new connection will be created for each request")
	flag.BoolVar(&percentiles, "p", false, "alias for percentiles")
	flag.BoolVar(&percentiles, "percentiles", false, "calculate percentiles")
	flag.DurationVar(&duration, "d", 0, "alias for duration")
	flag.DurationVar(&duration, "duration", 0, "total duration of the load test, e.g., 10m, 1h (default 0 - unlimited)")
	flag.DurationVar(&jitter, "j", 0, "alias for jitter")
	flag.DurationVar(&jitter, "jitter", 0, "maximum random delay at the start of the worker, e.g., 100ms, 1s")
	flag.Parse()

	if threads <= 0 {
		usage("threads must be positive")
	}

	if duration < 0 {
		usage("duration cannot be negative")
	}

	if jitter < 0 {
		usage("jitter cannot be negative")
	}

	interval := time.Duration(intervalSec * float64(time.Second))
	if interval < 0 {
		usage("interval cannot be negative")
	}

	timeout := time.Duration(timeoutSec * float64(time.Second))
	if timeout <= 0 {
		usage("timeout must be positive")
	}

	client := &Client{
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				DisableKeepAlives:   noKeepAlive,
				MaxIdleConns:        threads,
				MaxIdleConnsPerHost: threads,
				MaxConnsPerHost:     threads,
			}},
		BaseURL: url,
		Timeout: timeout,
	}

	abortCtx, abort := context.WithCancel(context.Background())
	defer abort()

	workCtx, stop := signal.NotifyContext(abortCtx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if duration > 0 {
		ctx, cancel := context.WithTimeout(workCtx, duration)
		defer cancel()
		workCtx = ctx
	}

	stats := &Statistics{}
	hists := make([]*hdrhistogram.Histogram, threads)

	var wg sync.WaitGroup
	wg.Add(threads)

	for i := range threads {
		if percentiles {
			hists[i] = hdrhistogram.New(int64(histMinValue), int64(histMaxValue), 3)
		}
	}

	for i := range threads {
		go func(id int, hist *hdrhistogram.Histogram) {
			defer wg.Done()
			w := Worker{
				ID:        id,
				Interval:  interval,
				Stats:     stats,
				ErrWindow: 2 * time.Second,
				Jitter:    jitter,
			}
			w.Run(workCtx, func() error {
				num := rand.IntN(201) - 100
				start := time.Now()
				if err := client.DoRequest(abortCtx, num); err != nil {
					return err
				}
				if hist != nil {
					if err := hist.RecordValue(int64(time.Since(start))); err != nil {
						log.Printf("hist.RecordValue: %v", err)
					}
				}
				return nil
			})
		}(i+1, hists[i])
	}

	log.Printf("Generator started: %d threads -> %s", threads, url)

	<-workCtx.Done()
	log.Printf("Shutdown: %v, stopping generator...", context.Cause(workCtx))

	tm := time.AfterFunc(2*time.Second, abort)
	defer tm.Stop()

	wg.Wait()
	_, _ = fmt.Printf("\nTotal requests: ok=%d errors=%d\n", stats.Ok.Load(), stats.Errors.Load())

	if percentiles {
		hist := hists[0]
		var dropped int64
		for _, from := range hists[1:] {
			dropped += hist.Merge(from)
		}
		if dropped > 0 {
			log.Printf("hist.Merge: total dropped %d values", dropped)
		}
		_, _ = fmt.Println()
		_, _ = hist.PercentilesPrint(os.Stdout, 1, float64(time.Microsecond))
	}
}

type Client struct {
	HTTPClient *http.Client
	BaseURL    string
	Timeout    time.Duration
}

func (c *Client) DoRequest(ctx context.Context, num int) error {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	url := c.BaseURL + "?num=" + strconv.Itoa(num)

	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return err
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		var b strings.Builder
		_, _ = io.Copy(&b, resp.Body)
		_ = resp.Body.Close()

		msg := strings.TrimSpace(b.String())
		return fmt.Errorf("http %d: %s", resp.StatusCode, msg)
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return nil
}

type Worker struct {
	ID       int
	Interval time.Duration
	Stats    *Statistics

	ErrWindow time.Duration
	lastErrs  map[string]errorCount
	Jitter    time.Duration
}

func (w *Worker) Run(ctx context.Context, work func() error) {
	var tm *time.Timer
	if w.Jitter <= 0 {
		tm = time.NewTimer(time.Hour)
		tm.Stop()
	} else {
		jitter := time.Duration(rand.Int64N(int64(w.Jitter)))
		tm = time.NewTimer(jitter)
		select {
		case <-ctx.Done():
			return
		case <-tm.C:
		}
	}
	defer tm.Stop()

	w.lastErrs = make(map[string]errorCount)
	defer w.flushErrs(true)

	flushTime := time.Now().Add(time.Second)

	for ctx.Err() == nil {
		if err := work(); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			w.Stats.Errors.Add(1)
			w.logErr(err)
		} else {
			w.Stats.Ok.Add(1)
		}

		now := time.Now()
		if now.Sub(flushTime) >= 0 {
			w.flushErrs(false)
			flushTime = now.Add(time.Second)
		}

		if w.Interval > 0 {
			tm.Reset(w.Interval)
			select {
			case <-ctx.Done():
				return
			case <-tm.C:
			}
		}
	}
}

type errorCount struct {
	flushAt    time.Time
	suppressed int
}

func (w *Worker) flushErrs(force bool) {
	now := time.Now()
	for msg, cnt := range w.lastErrs {
		if cnt.suppressed > 0 && (force || now.Sub(cnt.flushAt) >= 0) {
			w.printErr(msg, cnt.suppressed)
		}
		if cnt.suppressed == 0 && now.Sub(cnt.flushAt) >= 0 {
			delete(w.lastErrs, msg)
		}
	}
}

func (w *Worker) printErr(msg string, suppressed int) {
	if suppressed == 0 {
		log.Printf("[worker %d] request failed: %s", w.ID, msg)
	} else {
		log.Printf("[worker %d] request failed: %s (+%d similar)", w.ID, msg, suppressed)
	}
	win := w.ErrWindow
	if win == 0 {
		win = time.Second
	}
	w.lastErrs[msg] = errorCount{flushAt: time.Now().Add(win)}
}

func (w *Worker) logErr(err error) {
	msg := err.Error()
	now := time.Now()

	cnt, ok := w.lastErrs[msg]
	if ok && now.Sub(cnt.flushAt) < 0 {
		cnt.suppressed++
		w.lastErrs[msg] = cnt
		return
	}

	w.printErr(msg, cnt.suppressed)
}
