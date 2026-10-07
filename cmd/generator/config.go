package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"time"
)

type Config struct {
	BaseURL     string
	Threads     int
	Interval    time.Duration
	Timeout     time.Duration
	Duration    time.Duration
	Jitter      time.Duration
	NoKeepAlive bool
	Percentiles bool
}

func ParseConfigOrExit(name string, args ...string) Config {
	cfg, err := ParseConfig(os.Stderr, name, args...)
	switch {
	case errors.Is(err, flag.ErrHelp):
		os.Exit(0)
	case err != nil:
		os.Exit(2)
	}
	return cfg
}

func ParseConfig(output io.Writer, name string, args ...string) (Config, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(output)

	var f struct {
		baseURL     string
		threads     int
		intervalSec float64
		timeoutSec  float64
		noKeepAlive bool
		percentiles bool
		duration    time.Duration
		jitter      time.Duration
	}

	fs.StringVar(&f.baseURL, "url", "http://localhost:8080/calc", "calculator endpoint")
	fs.IntVar(&f.threads, "n", 10, "alias for threads")
	fs.IntVar(&f.threads, "threads", 10, "number of worker threads")
	fs.Float64Var(&f.intervalSec, "interval", 0.1, "pause between requests per thread, seconds")
	fs.Float64Var(&f.timeoutSec, "timeout", 5.0, "HTTP request timeout, seconds")
	fs.DurationVar(&f.duration, "d", 0, "total duration (0 = unlimited)")
	fs.DurationVar(&f.jitter, "j", 0, "max random delay at worker start")
	fs.BoolVar(&f.noKeepAlive, "no-keep-alive", false, "new connection per request")
	fs.BoolVar(&f.percentiles, "p", false, "show latency percentiles")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	fail := func(err error) (Config, error) {
		w := fs.Output()
		_, _ = fmt.Fprintf(w, "error: %v\n", err)
		_, _ = fmt.Fprintf(w, "Usage of %s:\n", fs.Name())
		fs.PrintDefaults()
		return Config{}, err
	}

	interval, err := secToDuration(f.intervalSec)
	if err != nil {
		return fail(fmt.Errorf("invalid interval: %w", err))
	}
	timeout, err := secToDuration(f.timeoutSec)
	if err != nil {
		return fail(fmt.Errorf("invalid timeout: %w", err))
	}

	switch {
	case interval < 0:
		return fail(errors.New("interval must be >= 0"))
	case timeout <= 0:
		return fail(errors.New("timeout must be > 0"))
	case f.threads <= 0:
		return fail(errors.New("threads must be > 0"))
	case f.duration < 0:
		return fail(errors.New("duration must be >= 0"))
	case f.jitter < 0:
		return fail(errors.New("jitter must be >= 0"))
	}

	return Config{
		BaseURL:     f.baseURL,
		Threads:     f.threads,
		Interval:    interval,
		Timeout:     timeout,
		Duration:    f.duration,
		Jitter:      f.jitter,
		NoKeepAlive: f.noKeepAlive,
		Percentiles: f.percentiles,
	}, nil
}

func secToDuration(sec float64) (time.Duration, error) {
	if math.IsNaN(sec) || math.IsInf(sec, 0) {
		return 0, errors.New("cannot be NaN or Inf")
	}
	t := sec * float64(time.Second)
	if t < math.MinInt64 || t >= math.MaxInt64 {
		return 0, errors.New("absolute value too large")
	}
	return time.Duration(t), nil
}
