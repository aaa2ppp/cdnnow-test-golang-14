package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/api"
	"aaa2ppp/cdnnow-test-golang-14/internal/calculator"
	"aaa2ppp/cdnnow-test-golang-14/internal/operators"
)

func usage(msg string) {
	out := flag.CommandLine.Output()
	_, _ = fmt.Fprintf(out, "%s\nUsage %s:\n", msg, os.Args[0])
	flag.PrintDefaults()
	os.Exit(1)
}

func main() {
	exe, err := os.Executable()
	if err != nil {
		log.Fatalf("cannot locate executable: %v\n", err)
	}
	libDir := filepath.Dir(exe)

	var (
		host        string
		port        uint
		cLibPath    string
		rustLibPath string
		intervalSec float64
	)
	flag.StringVar(&host, "host", "0.0.0.0", "bind server to address")
	flag.UintVar(&port, "port", 8080, "server port")
	flag.StringVar(&cLibPath, "c-lib", filepath.Join(libDir, "libcalculator.so"), "path to c-lib")
	flag.StringVar(&rustLibPath, "rust-lib", filepath.Join(libDir, "libcalculator_rust.so"), "path to rust-lib")
	flag.Float64Var(&intervalSec, "interval", 5.0, "seconds between periodic sum/sub reports")
	flag.Parse()

	interval := time.Duration(intervalSec * float64(time.Second))
	if interval <= 0 {
		usage("interval must be positive")
	}

	if err := operators.LoadLibraries(cLibPath, rustLibPath); err != nil {
		log.Fatalf("Failed to load native libraries: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err = run(ctx, Config{
		Host:     host,
		Port:     port,
		Interval: interval,
	})
	if err != nil {
		slog.Error("abnormal shutdown", "error", err)
		os.Exit(1)
	}
	slog.Info("server shutdown successfully")
}

type Config struct {
	Host     string
	Port     uint
	Interval time.Duration
}

func run(ctx context.Context, cfg Config) error {
	// calc := &calculator.MutextCalculator{}
	calc, stopCalc := calculator.NewChannelCalculator()
	defer stopCalc()

	stopPrinter := startPeriodicPrinter(ctx, calc, cfg.Interval)
	defer stopPrinter()

	router := api.New(calc)

	server := http.Server{
		Handler:      router,
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	done := make(chan error, 1)
	go func() {
		defer close(done)
		slog.Info("server listening on", "addr", server.Addr)
		done <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutdown server", "cause", context.Cause(ctx))
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return server.Shutdown(ctx)
	case err := <-done:
		return err
	}
}

type valulesGetter interface {
	Values() (sum, sub int64)
}

func startPeriodicPrinter(ctx context.Context, c valulesGetter, interval time.Duration) func() {
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer close(done)
		tk := time.NewTicker(interval)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				printTotals("final", c)
				return
			case <-tk.C:
				printTotals("periodic", c)
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func printTotals(label string, c valulesGetter) {
	sum, sub := c.Values()
	fmt.Printf("[%s] sum=%d sub=%d\n", label, sum, sub)
}
