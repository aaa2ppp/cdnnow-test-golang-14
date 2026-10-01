package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"
	"github.com/valyala/fasthttp"
)

// TODO: main.go распух. Пора резать на файлы/пакеты

const histMinValue = 1 * time.Microsecond
const histMaxValue = 100 * time.Millisecond

func usage(msg string) {
	out := flag.CommandLine.Output()
	_, _ = fmt.Fprintf(out, "%s\nUsage %s:\n", msg, os.Args[0])
	flag.PrintDefaults()
	os.Exit(1)
}

// TODO: обойтись без атомиков. Каждый воркер ведет свою статистику.
// Собрать в финале, аналогично гистограммам.
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

	target, err := ParseTarget(baseURL)
	if err != nil {
		log.Fatalf("parse %s: %v", baseURL, err)
	}

	if err := ProbeTarget(ctx, target, timeout); err != nil {
		log.Fatalf("probe %s: %v", target.BaseURL, err)
	}

	if duration > 0 {
		toCtx, cancel := context.WithTimeout(ctx, duration)
		defer cancel()
		ctx = toCtx
	}

	ctx, abort := context.WithCancelCause(ctx)
	defer abort(context.Canceled)

	stats := &Statistics{}

	// TODO: вынести в отдельную функцию StartWorkers()
	done := make(chan *hdrhistogram.Histogram, threads)
	for i := 0; i < threads; i++ {
		go func(id int) {
			var hist *hdrhistogram.Histogram
			defer func() { done <- hist }()

			if percentiles {
				hist = hdrhistogram.New(int64(histMinValue), int64(histMaxValue), 3)
			}

			client := NewSingleThreadClient(target, timeout, noKeepAlive)

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

			// TODO: логика размазалась между воркером и работой. По идее, работа только client.DoRequest(num)
			worker.Run(ctx, func(_ context.Context) error {
				num := rand.IntN(201) - 100
				since, err := client.DoRequest(num)
				if err != nil {
					if httpErr, ok := errors.AsType[*HTTPError](err); !ok || httpErr.Fatal() {
						abort(err)
					}
					return err
				}
				if hist != nil {
					if err := hist.RecordValue(int64(since)); err != nil {
						// TODO: поставить барьер на % таких ошибок. Если их много надо править диапазон
						log.Printf("hist.RecordValue: %v", err)
					}
				}
				return nil
			})
		}(i + 1)
	}

	log.Printf("Generator started: %d threads -> %s", threads, target.BaseURL)

	<-ctx.Done()
	log.Printf("Shutdown: %v, stopping generator...", context.Cause(ctx))

	tm := time.NewTimer(2 * time.Second)
	defer tm.Stop()

	var mergedHist *hdrhistogram.Histogram
	var dropped int64
	skipped := threads

	// TODO: вынести в отделную функцию WaitWorkers()
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

type Target struct {
	BaseURL  string        // для req.SetRequestURI: scheme://host/path?query (без user)
	Host     string        // для Header.SetHost
	Hostname string        // для SNI (tls.Config.ServerName)
	DialAddr string        // ip:port для Dialer
	Scheme   string        // "http" | "https"
	User     *url.Userinfo // Basic auth, если был user:pass@
}

func ParseTarget(baseURL string) (Target, error) {
	var zero Target

	u, err := url.Parse(baseURL)
	if err != nil {
		return zero, fmt.Errorf("parse url: %w", err)
	}

	scheme := u.Scheme
	if scheme == "" {
		scheme = "http"
	}
	if scheme != "http" && scheme != "https" {
		return zero, fmt.Errorf("scheme must be http or https, got %q", scheme)
	}

	hostname := u.Hostname()
	if hostname == "" {
		return zero, errors.New("empty host in URL")
	}

	port := u.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	ip := net.ParseIP(hostname)
	if ip == nil {
		ipAddr, err := net.ResolveIPAddr("ip", hostname)
		if err != nil {
			return zero, fmt.Errorf("resolve %q: %w", hostname, err)
		}
		ip = ipAddr.IP
	}

	// query без num - num допишем в DoRequest
	q := u.Query()
	q.Del("num")
	rawQuery := q.Encode()

	// Собираем обратно ручками
	var sb strings.Builder
	sb.Grow(len(baseURL) + 8)
	sb.WriteString(scheme)
	sb.WriteString("://")
	sb.WriteString(u.Host) // hostname[:port] как ввел пользователь
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	sb.WriteString(path)
	if rawQuery != "" {
		sb.WriteByte('?')
		sb.WriteString(rawQuery)
	}

	return Target{
		BaseURL:  sb.String(),
		Host:     u.Host,
		Hostname: hostname,
		DialAddr: net.JoinHostPort(ip.String(), port),
		Scheme:   scheme,
		User:     u.User,
	}, nil
}

func ProbeTarget(ctx context.Context, t Target, timeout time.Duration) error {
	c := NewSingleThreadClient(t, timeout, true)
	defer func() {
		fasthttp.ReleaseRequest(c.req)
		fasthttp.ReleaseResponse(c.resp)
	}()

	done := make(chan error)
	go func() {
		defer close(done)
		c.req.SetRequestURI(t.BaseURL)
		_, err := c.do()
		done <- err
	}()

	var err error
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err = <-done:
	}

	if err != nil {
		if httpErr, ok := errors.AsType[*HTTPError](err); !ok ||
			(httpErr.Fatal() && httpErr.Code != 400) {
			return err
		}
	}

	return nil
}

type HTTPError struct {
	Code    int
	Message string
}

func (e *HTTPError) Error() string {
	code := e.Code
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = http.StatusText(code)
	}
	return fmt.Sprintf("http %d: %s", code, msg)
}

func (e *HTTPError) Fatal() bool {
	switch e.Code {
	case 429, 500, 503: // мы зафлудили и серверу плохо
		return false
	}
	// все остальное неожиданно и требует расследования
	return true
}

type SingleThreadClient struct {
	client *fasthttp.Client
	uriBuf []byte
	req    *fasthttp.Request
	resp   *fasthttp.Response
}

func NewSingleThreadClient(t Target, timeout time.Duration, noKeepAlive bool) *SingleThreadClient {
	client := &fasthttp.Client{
		MaxConnsPerHost:           1,
		MaxIdemponentCallAttempts: 1,
		ReadTimeout:               timeout,
		WriteTimeout:              timeout,
	}

	dialAddr := t.DialAddr
	hostname := t.Hostname
	isTLS := t.Scheme == "https"

	client.Dial = func(_ string) (net.Conn, error) {
		if isTLS {
			return tls.Dial("tcp", dialAddr, &tls.Config{
				ServerName: hostname,
				MinVersion: tls.VersionTLS12,
			})
		}
		return fasthttp.Dial(dialAddr)
	}

	req := fasthttp.AcquireRequest()
	req.Header.SetMethod("POST")
	req.Header.SetHost(t.Host)
	if noKeepAlive {
		req.Header.SetConnectionClose()
	}
	if t.User != nil {
		pass, _ := t.User.Password()
		setBasicAuth(req, t.User.Username(), pass)
	}

	uriBuf := make([]byte, 0, len(t.BaseURL)+32)
	uriBuf = append(uriBuf, t.BaseURL...)
	if strings.ContainsRune(t.BaseURL, '?') {
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

func setBasicAuth(req *fasthttp.Request, username, password string) {
	auth := username + ":" + password
	encoded := base64.StdEncoding.EncodeToString([]byte(auth))
	req.Header.Set("Authorization", "Basic "+encoded)
}

func isOK(status int) bool {
	return status/100*100 == 200
}

func (c *SingleThreadClient) do() (time.Duration, error) {

	// Мы хотим знать время ответа сервера, но since - это server time + client time.
	// Mаксимально сокращаем долю клиента.
	start := time.Now()
	err := c.client.Do(c.req, c.resp)
	if err != nil {
		return 0, err
	}
	since := time.Since(start)

	if code := c.resp.StatusCode(); !isOK(code) {
		msg := bytes.TrimSpace(c.resp.Body())
		if len(msg) > 1024 {
			msg = msg[:1024]
		}
		return 0, &HTTPError{Code: code, Message: string(msg)} // копирование msg
	}

	return since, nil
}

func (c *SingleThreadClient) DoRequest(num int) (time.Duration, error) {
	uri := strconv.AppendInt(c.uriBuf, int64(num), 10)
	c.req.SetRequestURIBytes(uri)
	return c.do()
}

const defaultMinDelay = 100 * time.Millisecond
const defaultMaxDelay = 30 * time.Second
const delayJitterPerc = 20 // 0-100%

type Worker struct {
	ID       int
	Interval time.Duration
	Stats    *Statistics

	ErrWindow time.Duration
	lastErrs  map[string]errorCount
}

func (w *Worker) delayWithJitter(base time.Duration) time.Duration {
	minD := max(w.Interval, defaultMinDelay)
	maxD := max(w.Interval, defaultMaxDelay)

	if base < minD {
		base = minD
	}
	if base > maxD {
		base = maxD
	}

	jitter := time.Duration(rand.Int64N(int64(base * delayJitterPerc / 100)))
	return base*(200-delayJitterPerc)/200 + jitter
}

func (w *Worker) Run(ctx context.Context, work func(context.Context) error) {
	tm := time.NewTimer(time.Hour)
	tm.Stop()
	defer tm.Stop()

	w.lastErrs = make(map[string]errorCount)
	defer w.flushErrs(true)

	flushTime := time.Now().Add(time.Second)
	interval := w.Interval

	for ctx.Err() == nil { // на случай, если interval=0
		err := work(ctx)
		if err != nil {
			w.Stats.Errors.Add(1)
			w.logErr(err)
			interval *= 2
		} else {
			w.Stats.Ok.Add(1)
			interval = w.Interval
		}

		if now := time.Now(); now.Sub(flushTime) >= 0 {
			w.flushErrs(false)
			flushTime = now.Add(time.Second)
		}

		d := interval
		if err != nil {
			d = w.delayWithJitter(interval)
		}

		if d > 0 {
			tm.Reset(d)
			select {
			case <-ctx.Done():
				return
			case <-tm.C:
			}
		} else {
			runtime.Gosched()
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
