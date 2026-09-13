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
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

func usage(msg string) {
	out := flag.CommandLine.Output()
	_, _ = fmt.Fprintf(out, "%s\nUsage %s:\n", msg, os.Args[0])
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
	)
	flag.StringVar(&url, "url", "http://localhost:8080/calc", "calculator endpoint")
	flag.IntVar(&threads, "n", 10, "alias for threads")
	flag.IntVar(&threads, "threads", 10, "number of worker threads")
	flag.Float64Var(&intervalSec, "interval", 0.1, "pause between requests per thread, in seconds (0 = as fast as possible)")
	flag.Float64Var(&timeoutSec, "timeout", 5.0, "HTTP request timeout, seconds")
	flag.BoolVar(&noKeepAlive, "no-keep-alive", false, "new connection will be created for each request")
	flag.Parse()

	if threads <= 0 {
		usage("threads must be positive")
	}

	interval := time.Duration(intervalSec * float64(time.Second))
	if interval < 0 {
		usage("interval cannot be negative")
	}

	timeout := time.Duration(timeoutSec * float64(time.Second))
	if timeout <= 0 {
		usage("timeout must be positive")
	}

	stats := &Statistics{}

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

	var wg sync.WaitGroup
	wg.Add(threads)

	for i := range threads {
		go func(id int) {
			defer wg.Done()
			w := Worker{
				ID:       id,
				Interval: interval,
				Stats:    stats,
			}
			w.Run(workCtx, func() error {
				num := rand.IntN(201) - 100
				return client.DoRequest(abortCtx, num)
			})
		}(i + 1)
	}

	log.Printf("Generator started: %d threads -> %s", threads, url)

	<-workCtx.Done()
	log.Printf("Shutdown: %v, stopping generator...", context.Cause(workCtx))

	tm := time.AfterFunc(2*time.Second, abort)
	defer tm.Stop()

	wg.Wait()
	log.Printf("Total requests: ok=%d errors=%d", stats.Ok.Load(), stats.Errors.Load())
}

type Client struct {
	HTTPClient *http.Client
	BaseURL    string
	Timeout    time.Duration
}

func (c *Client) DoRequest(ctx context.Context, num int) error {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	url := fmt.Sprintf("%s?num=%d", c.BaseURL, num)

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
}

func (w *Worker) Run(ctx context.Context, work func() error) {
	tm := time.NewTimer(0)
	defer tm.Stop()

	for ctx.Err() == nil {
		err := work()
		if errors.Is(err, context.Canceled) {
			continue
		}
		if err != nil {
			w.Stats.Errors.Add(1)
			log.Printf("[worker %d] request failed: %v\n", w.ID, err)
		} else {
			w.Stats.Ok.Add(1)
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
