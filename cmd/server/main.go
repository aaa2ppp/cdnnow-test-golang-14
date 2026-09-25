package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/api"
	"aaa2ppp/cdnnow-test-golang-14/internal/calculators"
	"aaa2ppp/cdnnow-test-golang-14/internal/metrics"
	"aaa2ppp/cdnnow-test-golang-14/internal/operators"
	"aaa2ppp/cdnnow-test-golang-14/internal/pools"

	"golang.org/x/net/netutil"
)

func usage(msg string) {
	out := flag.CommandLine.Output()
	_, _ = fmt.Fprintf(out, "%s\nUsage %s:\n", msg, os.Args[0])
	flag.PrintDefaults()
	os.Exit(1)
}

//go:generate enumer -type CalcMode -linecomment -text
type CalcMode uint8

const (
	_        CalcMode = iota
	Sync              // sync
	Async             // async
	Parallel          // parallel
)

func main() {
	exe, err := os.Executable()
	if err != nil {
		log.Fatalf("cannot locate executable: %v\n", err)
	}
	libDir := filepath.Dir(exe)

	var (
		host        string
		port        string
		cLibPath    string
		rustLibPath string
		intervalSec float64
		calcMode    CalcMode
		queueSize   int
		pprofAddr   string
		maxConns    int
	)
	flag.StringVar(&host, "host", "0.0.0.0", "bind server to address")
	flag.StringVar(&port, "port", "8080", "server port")
	flag.StringVar(&cLibPath, "c-lib", filepath.Join(libDir, "libcalculator.so"), "path to c-lib")
	flag.StringVar(&rustLibPath, "rust-lib", filepath.Join(libDir, "libcalculator_rust.so"), "path to rust-lib")
	flag.Float64Var(&intervalSec, "interval", 5.0, "seconds between periodic sum/sub reports")
	flag.TextVar(&calcMode, "calc-mode", Async, "calculation execution mode, can be: sync, async, parallel")
	flag.IntVar(&queueSize, "queue-size", calculatorQueueSize, "maximum task queue capacity. Returns 503 if the queue overflows. Only applies to async or parallel modes.")
	flag.StringVar(&pprofAddr, "pprof-addr", "localhost:6060", "pprof listen address (host:port, empty to disable)")
	flag.IntVar(&maxConns, "max-conns", 0, "maximum number of connections (0 - without restrictions)")
	flag.Parse()

	interval := time.Duration(intervalSec * float64(time.Second))
	if interval <= 0 {
		usage("interval must be positive")
	}

	if (calcMode == Async || calcMode == Parallel) && queueSize <= 0 {
		usage("queue-size must be positive")
	}

	if maxConns < 0 {
		usage("max-conns cannot be negative")
	}

	if err := operators.LoadLibraries(cLibPath, rustLibPath); err != nil {
		log.Fatalf("Failed to load native libraries: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err = run(ctx, Config{
		Addr:      fmt.Sprintf("%s:%s", host, port),
		Interval:  interval,
		CalcMode:  calcMode,
		QueueSize: queueSize,
		PprofAddr: pprofAddr,
		MaxConns:  maxConns,
	})
	if err != nil {
		slog.Error("abnormal shutdown", "error", err)
		os.Exit(1)
	}
	slog.Info("server shutdown successfully")
}

const calculatorQueueSize = 1024
const aggregatorQueueSize = 100
const samplesBatchSize = 256

type Config struct {
	Addr      string
	Interval  time.Duration
	CalcMode  CalcMode
	QueueSize int
	PprofAddr string
	MaxConns  int
}

type Service struct {
	calculator calculators.Calculator
	counter    *metrics.Aggregator
	printer    *metrics.Printer
}

func (s *Service) Calculate(num int64) error              { return s.calculator.Calculate(num) }
func (s *Service) CountRequests(kind metrics.RequestKind) { s.counter.CountRequests(kind, 1) }
func (s *Service) PrintMetrics(w io.Writer) error         { return s.printer.Print(w) }

type metricsAggr struct {
	*metrics.Aggregator
}

func (m *metricsAggr) RecordAddDurations(batch []time.Duration) {
	m.RecordDurations(metrics.Add, batch)
}
func (m *metricsAggr) RecordSubDurations(batch []time.Duration) {
	m.RecordDurations(metrics.Sub, batch)
}

func run(ctx context.Context, cfg Config) error {
	var aggregator *metrics.Aggregator
	var calculator calculators.Calculator

	slog.Info("runtime", "NumCPU", runtime.NumCPU(), "GOMAXPROCS", runtime.GOMAXPROCS(0))

	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()

	if cfg.MaxConns != 0 {
		listener = netutil.LimitListener(listener, cfg.MaxConns)
	}

	stopPprof, err := startPprof(cfg.PprofAddr)
	if err != nil {
		return fmt.Errorf("start pprof: %w", err)
	}
	defer stopPprof()

	switch cfg.CalcMode {
	case Sync:
		aggregator = metrics.NewAggregator(aggregatorQueueSize, nil, nil)
		calculator = calculators.NewSyncCalculator(aggregator.RecordSample)

	case Async:
		pool := pools.NewBatchPool[metrics.Sample](samplesBatchSize)
		aggregator = metrics.NewAggregator(aggregatorQueueSize, pool, nil)
		calculator = calculators.NewAsyncCalculator(cfg.QueueSize, aggregator.RecordSamples, pool)

	case Parallel:
		pool := pools.NewBatchPool[time.Duration](samplesBatchSize)
		aggregator = metrics.NewAggregator(aggregatorQueueSize, nil, pool)
		calculator = calculators.NewParallelCalculator(cfg.QueueSize, &metricsAggr{aggregator}, pool)

	default:
		return fmt.Errorf("unknown calculation mode: %v", cfg.CalcMode)
	}

	defer aggregator.Stop()

	stopPrinter := startPeriodicPrinter(calculator, cfg.Interval)
	defer stopPrinter()

	if calculator, ok := calculator.(interface{ Stop() }); ok {
		defer func() {
			slog.Info("stop calculator")
			calculator.Stop()
		}()
	}

	router := api.New(&Service{
		calculator: calculator,
		counter:    aggregator,
		printer:    metrics.NewPrinter(aggregator),
	})

	server := http.Server{
		Handler:      router,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	done := make(chan error, 1)
	go func() {
		defer close(done)
		queueSize := cfg.QueueSize
		if cfg.CalcMode == Sync {
			queueSize = 0
		}
		slog.Info("server listening on", "addr", cfg.Addr, "mode", cfg.CalcMode, "queue", queueSize)
		done <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutdown server", "cause", context.Cause(ctx))
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			_ = server.Close()
			return err
		}
		return nil
	case err := <-done:
		return err
	}
}

func startPprof(addr string) (stop func(), err error) {
	if addr == "" {
		return func() {}, nil
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("pprof listening", "addr", addr)
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("pprof server failed", "error", err)
		}
	}()

	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("pprof shutdown", "error", err)
		}
	}, nil
}

type valuer interface {
	Values() calculators.Values
}

func startPeriodicPrinter(calc valuer, interval time.Duration) func() {
	done := make(chan struct{})
	stopCh := make(chan struct{})
	go func() {
		defer close(done)
		tk := time.NewTicker(interval)
		defer tk.Stop()
		for {
			select {
			case <-stopCh:
				printTotals("final", calc)
				return
			case <-tk.C:
				printTotals("periodic", calc)
			}
		}
	}()
	return func() {
		close(stopCh)
		<-done
	}
}

func printTotals(label string, calc valuer) {
	vals := calc.Values()
	log.Printf("[%s] sum=%d sub=%d\n", label, vals.Sum, vals.Sub)
}
