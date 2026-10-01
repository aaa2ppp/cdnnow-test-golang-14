package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"net"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"
	"github.com/valyala/fasthttp"
)

const histMinValue = 1 * time.Microsecond
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	newBaseURL, host, err := prepareURL(baseURL)
	if err != nil {
		log.Fatalf("can't prepare %s: %v", baseURL, err)
	}

	if duration > 0 {
		ttCtx, cancel := context.WithTimeout(ctx, duration)
		defer cancel()
		ctx = ttCtx
	}

	stats := &Statistics{}

	done := make(chan *hdrhistogram.Histogram, threads)
	for i := 0; i < threads; i++ {
		go func(id int) {
			var hist *hdrhistogram.Histogram
			defer func() { done <- hist }()

			if percentiles {
				hist = hdrhistogram.New(int64(histMinValue), int64(histMaxValue), 3)
			}

			client := NewSingleThreadClient(newBaseURL, host, timeout, noKeepAlive)

			worker := &Worker{
				ID:        id,
				Interval:  interval,
				Stats:     stats,
				ErrWindow: 2 * time.Second,
			}

			if jitter > 0 {
				if err := randomSleep(ctx, jitter); err != nil {
					return
				}
			}

			worker.Run(ctx, func() error {
				num := rand.IntN(201) - 100
				since, err := client.DoRequest(num)
				if err != nil {
					return err
				}
				if hist != nil {
					if err := hist.RecordValue(int64(since)); err != nil {
						log.Printf("hist.RecordValue: %v", err)
					}
				}
				return nil
			})
		}(i + 1)
	}

	log.Printf("Generator started: %d threads -> %s", threads, newBaseURL)

	<-ctx.Done()
	log.Printf("Shutdown: %v, stopping generator...", context.Cause(ctx))

	tm := time.NewTimer(2 * time.Second)
	defer tm.Stop()

	var mergedHist *hdrhistogram.Histogram
	var dropped int64
	skipped := threads

waitLoop:
	for ; skipped > 0; skipped-- {
		select {
		case <-tm.C:
			break waitLoop
		case hist := <-done:
			if mergedHist == nil {
				mergedHist = hist
			} else {
				dropped += mergedHist.Merge(hist)
			}
		}
	}

	_, _ = fmt.Printf("\nTotal requests: ok=%d errors=%d\n", stats.Ok.Load(), stats.Errors.Load())

	if percentiles {
		if skipped > 0 {
			log.Printf("skipped %d worker histograms", skipped)
		}
		if dropped > 0 {
			log.Printf("hist.Merge: total dropped %d values", dropped)
		}
		if mergedHist != nil {
			_, _ = fmt.Println()
			_, _ = mergedHist.PercentilesPrint(os.Stdout, 1, float64(time.Microsecond))
		}
	}
}

func prepareURL(baseURL string) (newBase, host string, err error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", "", err
	}

	var newU url.URL

	scheme := u.Scheme
	if scheme == "" {
		scheme = "http"
	}
	if scheme != "http" {
		return "", "", errors.New("scheme must be 'http'")
	}
	newU.Scheme = scheme

	host = u.Host
	hostname := u.Hostname()
	port := u.Port()

	ip := net.ParseIP(hostname)
	if ip == nil {
		ipAddr, err := net.ResolveIPAddr("ip", hostname)
		if err != nil {
			return "", "", err
		}
		ip = ipAddr.IP
	}

	if port != "" {
		newU.Host = net.JoinHostPort(ip.String(), port)
	} else {
		newU.Host = ip.String()
	}

	query := u.Query()
	query.Del("num")
	newU.RawQuery = query.Encode()

	return newU.String(), host, nil
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

type SingleThreadClient struct {
	client *fasthttp.Client
	uriBuf []byte
	req    *fasthttp.Request
	resp   *fasthttp.Response
}

func NewSingleThreadClient(baseURL string, host string, timeout time.Duration, noKeepAlive bool) *SingleThreadClient {
	client := &fasthttp.Client{
		MaxConnsPerHost:           1,
		MaxIdemponentCallAttempts: 1,
		ReadTimeout:               timeout,
		WriteTimeout:              timeout,
	}

	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("POST")
	if host != "" {
		req.Header.SetHost(host)
	}
	if noKeepAlive {
		req.Header.SetConnectionClose()
	}

	uriBuf := make([]byte, 0, len(baseURL)+32) // "?num=<int>" -> 5 + 20
	uriBuf = append(uriBuf, baseURL...)
	if strings.Contains(baseURL, "?") {
		uriBuf = append(uriBuf, "&num="...)
	} else {
		uriBuf = append(uriBuf, "?num="...)
	}

	return &SingleThreadClient{
		client: client,
		uriBuf: uriBuf,
		req:    req,
		resp:   fasthttp.AcquireResponse(),
	}
}

func isOK(status int) bool {
	return status/100*100 == 200
}

func (c *SingleThreadClient) DoRequest(num int) (time.Duration, error) {
	uri := strconv.AppendInt(c.uriBuf, int64(num), 10)
	c.req.SetRequestURIBytes(uri)

	// Мы хотим знать время ответа сервера, но since - это server time + client time.
	// Mаксимально сокращаем долю клиента.
	start := time.Now()
	err := c.client.Do(c.req, c.resp)
	if err != nil {
		return 0, err
	}
	since := time.Since(start)

	if status := c.resp.StatusCode(); !isOK(status) {
		msg := bytes.TrimSpace(c.resp.Body())
		return 0, fmt.Errorf("http %d: %s", status, msg)
	}

	return since, nil
}

type Worker struct {
	ID       int
	Interval time.Duration
	Stats    *Statistics

	ErrWindow time.Duration
	lastErrs  map[string]errorCount
}

func (w *Worker) Run(ctx context.Context, work func() error) {
	tm := time.NewTimer(time.Hour)
	tm.Stop()
	defer tm.Stop()

	w.lastErrs = make(map[string]errorCount)
	defer w.flushErrs(true)

	flushTime := time.Now().Add(time.Second)

	for ctx.Err() == nil { // на случай, если interval=0
		if err := work(); err != nil {
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
