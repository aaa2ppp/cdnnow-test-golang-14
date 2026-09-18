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
	"syscall"
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/api"
	"aaa2ppp/cdnnow-test-golang-14/internal/calculators"
	"aaa2ppp/cdnnow-test-golang-14/internal/metrics"
	"aaa2ppp/cdnnow-test-golang-14/internal/operators"
	"aaa2ppp/cdnnow-test-golang-14/internal/pools"
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
	)
	flag.StringVar(&host, "host", "0.0.0.0", "bind server to address")
	flag.StringVar(&port, "port", "8080", "server port")
	flag.StringVar(&cLibPath, "c-lib", filepath.Join(libDir, "libcalculator.so"), "path to c-lib")
	flag.StringVar(&rustLibPath, "rust-lib", filepath.Join(libDir, "libcalculator_rust.so"), "path to rust-lib")
	flag.Float64Var(&intervalSec, "interval", 5.0, "seconds between periodic sum/sub reports")
	flag.TextVar(&calcMode, "calc-mode", Async, "calculation execution mode, can be: sync, async, parallel")
	flag.IntVar(&queueSize, "queue-size", calculatorQueueSize, "Maximum task queue capacity. Returns 503 if the queue overflows. Only applies to -calc-mode=async or -calc-mode=parallel.")
	flag.StringVar(&pprofAddr, "pprof-addr", "localhost:6060", "pprof listen address (host:port, empty to disable)")
	flag.Parse()

	interval := time.Duration(intervalSec * float64(time.Second))
	if interval <= 0 {
		usage("interval must be positive")
	}

	if (calcMode == Async || calcMode == Parallel) && queueSize <= 0 {
		usage("queue-size must be positive")
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
}

type Service struct {
	calculator calculators.Calculator
	aggregator *metrics.Aggregator
	printer    *metrics.Printer
}

func (s *Service) Calculate(num int64) error      { return s.calculator.Calculate(num) }
func (s *Service) CountRequests()                 { s.aggregator.CountRequests(1) }
func (s *Service) PrintMetrics(w io.Writer) error { _, err := s.printer.Print(w); return err }

func run(ctx context.Context, cfg Config) error {
	var aggregator *metrics.Aggregator
	var calculator calculators.Calculator

	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()

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
		calculator = calculators.NewParallelCalculator(cfg.QueueSize, aggregator, pool)

	default:
		return fmt.Errorf("unknown sync kind: %v", cfg.CalcMode)
	}

	defer aggregator.Stop()
	if calculator, ok := calculator.(interface{ Stop() }); ok {
		defer calculator.Stop()
	}

	stopPrinter := startPeriodicPrinter(ctx, calculator, cfg.Interval)
	defer stopPrinter()

	router := api.New(&Service{
		calculator: calculator,
		aggregator: aggregator,
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
		slog.Info("server listening on", "addr", cfg.Addr, "mode", cfg.CalcMode, "queue", cfg.QueueSize)
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

func startPeriodicPrinter(ctx context.Context, calc valuer, interval time.Duration) func() {
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer close(done)
		tk := time.NewTicker(interval)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				printTotals("final", calc)
				return
			case <-tk.C:
				printTotals("periodic", calc)
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func printTotals(label string, calc valuer) {
	vals := calc.Values()
	fmt.Printf("[%s] sum=%d sub=%d\n", label, vals.Sum, vals.Sub)
}
