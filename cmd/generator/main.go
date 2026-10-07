package main

import (
	"bufio"
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/generator/clients"
	"aaa2ppp/cdnnow-test-golang-14/internal/generator/loggers"
)

const shutdownTimeout = 2 * time.Second
const loggerWindow = 1 * time.Second

func main() {
	cfg := ParseConfigOrExit(filepath.Base(os.Args[0]), os.Args[1:]...)

	target, err := clients.PrepareTarget(cfg.BaseURL, clients.CalcKeys())
	if err != nil {
		log.Fatalf("parse %s: %v", cfg.BaseURL, err)
	}

	if err := ProbeTarget(target, cfg.Timeout); err != nil {
		log.Fatalf("probe %+v: %v", target, err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.Duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Duration)
		defer cancel()
	}

	logger := loggers.NewDedup(log.Default(), cfg.Threads, loggerWindow)

	done := startWorkers(ctx, cfg.Threads, RunConfig{
		Logger:      logger,
		Jitter:      cfg.Jitter,
		Interval:    cfg.Interval,
		Target:      target,
		Timeout:     cfg.Timeout,
		NoKeepAlive: cfg.NoKeepAlive,
	})

	log.Printf("Generator started: %d threads -> http://%s%s", cfg.Threads, target.DialAddr, target.Path)

	<-ctx.Done()
	log.Printf("Shutdown: %v, stopping generator...", context.Cause(ctx))

	stat, n := waitWorkers(done, shutdownTimeout)
	logger.Close()

	if n < cfg.Threads {
		log.Printf("skipped %d worker statistics", cfg.Threads-n)
	}

	bw := bufio.NewWriter(os.Stdout)
	if err := PrintReport(bw, stat, cfg.Percentiles); err != nil {
		log.Fatal(err)
	}
	if err := bw.Flush(); err != nil {
		log.Fatal(err)
	}
}
