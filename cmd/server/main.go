package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/operators"
	"aaa2ppp/cdnnow-test-golang-14/internal/servers"
)

const defaultQueueSize = 1024

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
		port        string
		cLibPath    string
		rustLibPath string
		intervalSec float64
		calcMode    servers.CalcMode
		queueSize   int
		pprofAddr   string
		maxConns    int
	)
	flag.StringVar(&host, "host", "0.0.0.0", "bind server to address")
	flag.StringVar(&port, "port", "8080", "server port")
	flag.StringVar(&cLibPath, "c-lib", filepath.Join(libDir, "libcalculator.so"), "path to c-lib")
	flag.StringVar(&rustLibPath, "rust-lib", filepath.Join(libDir, "libcalculator_rust.so"), "path to rust-lib")
	flag.Float64Var(&intervalSec, "interval", 5.0, "seconds between periodic sum/sub reports")
	flag.TextVar(&calcMode, "calc-mode", servers.Async, "calculation execution mode, can be: sync, async, parallel")
	flag.IntVar(&queueSize, "queue-size", defaultQueueSize, "maximum task queue capacity. Returns 503 if the queue overflows. Only applies to async or parallel modes.")
	flag.StringVar(&pprofAddr, "pprof-addr", "localhost:6060", "pprof listen address (host:port, empty to disable)")
	flag.IntVar(&maxConns, "max-conns", 0, "maximum number of connections (0 - without restrictions)")
	flag.Parse()

	interval := time.Duration(intervalSec * float64(time.Second))
	if interval <= 0 {
		usage("interval must be positive")
	}

	if (calcMode == servers.Async || calcMode == servers.Parallel) && queueSize <= 0 {
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

	err = servers.Run(ctx, servers.Config{
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
