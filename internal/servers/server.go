package servers

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"runtime"
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/calculators"
	"aaa2ppp/cdnnow-test-golang-14/internal/metrics"
	"aaa2ppp/cdnnow-test-golang-14/internal/pools"

	"golang.org/x/net/netutil"
)

const aggregatorQueueSize = 100
const samplesBatchSize = 256

//go:generate enumer -type CalcMode -linecomment -text
type CalcMode uint8

const (
	_        CalcMode = iota
	Sync              // sync
	Async             // async
	Parallel          // parallel
)

type Config struct {
	Addr      string
	Interval  time.Duration
	CalcMode  CalcMode
	QueueSize int
	PprofAddr string
	MaxConns  int
}

type service struct {
	calculator calculators.Calculator
	counter    *metrics.Aggregator
	printer    *metrics.Printer
}

func (s *service) Calculate(num int64) error              { return s.calculator.Calculate(num) }
func (s *service) CountRequests(kind metrics.RequestKind) { s.counter.CountRequests(kind, 1) }
func (s *service) PrintMetrics(w io.Writer) error         { return s.printer.Print(w) }

type metricsAggr struct {
	*metrics.Aggregator
}

func (m *metricsAggr) RecordAddDurations(batch []time.Duration) {
	m.RecordDurations(metrics.Add, batch)
}
func (m *metricsAggr) RecordSubDurations(batch []time.Duration) {
	m.RecordDurations(metrics.Sub, batch)
}

func Run(ctx context.Context, cfg Config) error {
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

	svc := &service{
		calculator: calculator,
		counter:    aggregator,
		printer:    metrics.NewPrinter(aggregator),
	}

	queueSize := cfg.QueueSize
	if cfg.CalcMode == Sync {
		queueSize = 0
	}
	slog.Info("server listening", "addr", cfg.Addr, "mode", cfg.CalcMode, "queue", queueSize)

	return runStdHTTPServer(ctx, listener, svc)
}
