package servers

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"runtime"
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/api"
	"aaa2ppp/cdnnow-test-golang-14/internal/calculators"
	"aaa2ppp/cdnnow-test-golang-14/internal/domain"
	"aaa2ppp/cdnnow-test-golang-14/internal/metrics"
	"aaa2ppp/cdnnow-test-golang-14/internal/pipeline"
	report "aaa2ppp/cdnnow-test-golang-14/internal/report"

	"golang.org/x/net/netutil"
)

//go:generate enumer -type ServerType -linecomment -text
type ServerType uint8

const (
	_          ServerType = iota
	ServerStd             // stdhttp
	ServerFast            // fasthttp
)

//go:generate enumer -type CalcMode -linecomment -text
type CalcMode uint8

const (
	_        CalcMode = iota
	Sync              // sync
	Async             // async
	Parallel          // parallel
)

type Config struct {
	Addr       string
	Interval   time.Duration
	ServerType ServerType
	CalcMode   CalcMode
	QueueSize  int
	PprofAddr  string
	MaxConns   int
	Workers    int
}

type service interface {
	Calculate(num int64) error
	CountRequests(kind domain.RequestKind)
	PrintMetrics(w io.Writer) error
	Values() domain.Values
	Stop()
}

type syncService struct {
	aggr    *metrics.Aggregator
	calc    *calculators.Sync
	printer *report.Printer
}

func (s *syncService) Calculate(num int64) error             { return s.calc.Calculate(num) }
func (s *syncService) CountRequests(kind domain.RequestKind) { s.aggr.CountRequests(kind, 1) }
func (s *syncService) PrintMetrics(w io.Writer) error        { return s.printer.Print(w) }
func (s *syncService) Values() domain.Values                 { return s.calc.Values() }
func (s *syncService) Stop()                                 {}

type asyncService struct {
	pipeline *pipeline.Pipeline
	printer  *report.Printer
}

func (s *asyncService) Calculate(num int64) error             { return s.pipeline.Calculate(num) }
func (s *asyncService) CountRequests(kind domain.RequestKind) { s.pipeline.CountRequests(kind, 1) }
func (s *asyncService) PrintMetrics(w io.Writer) error        { return s.printer.Print(w) }
func (s *asyncService) Values() domain.Values                 { return s.pipeline.Values() }
func (s *asyncService) Stop()                                 { s.pipeline.Close() }

func newService(cfg Config) service {
	if cfg.CalcMode == Sync {
		smplCh := make(chan domain.Sample, 50)
		aggr := metrics.NewAggregator(nil, smplCh, nil)
		calc := calculators.NewSync(func(smpl domain.Sample) { smplCh <- smpl })
		printer := report.NewPrinter(aggr)

		return &syncService{
			aggr:    aggr,
			calc:    calc,
			printer: printer,
		}
	}

	pipeline := pipeline.NewPipeline(cfg.QueueSize, cfg.Workers)
	printer := report.NewPrinter(pipeline)

	return &asyncService{
		pipeline: pipeline,
		printer:  printer,
	}
}

func Run(ctx context.Context, cfg Config) error {
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

	svc := newService(cfg)

	stopPrinter := startPeriodicPrinter(svc, cfg.Interval)
	defer stopPrinter()

	defer func() {
		slog.Info("stop calculator")
		svc.Stop()
	}()

	apiCfg := api.Config{
		AsyncCalc: cfg.CalcMode != Sync,
		Service:   svc,
	}

	queueSize := cfg.QueueSize
	if cfg.CalcMode == Sync {
		queueSize = 0
	}
	slog.Info("server listening", "addr", cfg.Addr, "type", cfg.ServerType, "mode", cfg.CalcMode, "queue", queueSize)

	switch cfg.ServerType {
	case ServerStd:
		handler := api.NewStd(apiCfg)
		return runStdHTTPServer(ctx, listener, handler)
	case ServerFast:
		handler := api.NewFast(apiCfg)
		return runFastHTTPServer(ctx, listener, handler)
	}
	return fmt.Errorf("servers.Run: unknown server type: %v", cfg.ServerType)
}
