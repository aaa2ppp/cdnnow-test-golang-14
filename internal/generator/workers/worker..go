package workers

import (
	"context"
	"errors"
	"math/rand/v2"
	"runtime"
	"slices"
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/generator/clients"

	"github.com/HdrHistogram/hdrhistogram-go"
)

const histMinValue = 1 * time.Microsecond
const histMaxValue = 100 * time.Millisecond

var transientCodes = []int{429, 500, 503}

type Client interface {
	DoRequest(num int) (latency time.Duration, _ error)
}

type Stat struct {
	Ok     int64
	Errors int64
	Hist   *hdrhistogram.Histogram
}

type Worker struct {
	ID       int
	Client   Client
	Interval time.Duration
}

func isFatal(err error) bool {
	var httpErr *clients.HTTPError
	if !errors.As(err, &httpErr) {
		return true
	}
	return !slices.Contains(transientCodes, httpErr.Code)
}

func (w *Worker) Run(ctx context.Context) (Stat, error) {
	tm := time.NewTimer(0)
	defer tm.Stop()

	stat := Stat{
		Hist: hdrhistogram.New(int64(histMinValue), int64(histMaxValue), 3),
	}

	lg := &errorLogger{ID: w.ID}
	defer lg.maybeFlush(true)

	flushTime := time.Now().Add(time.Second)
	interval := w.Interval

	for ctx.Err() == nil { // на случай, если interval=0
		num := rand.IntN(201) - 100
		latency, err := w.Client.DoRequest(num)

		if err != nil {
			stat.Errors++
			if isFatal(err) {
				return stat, err
			}
			lg.logError(err)
		} else {
			stat.Ok++
			if err := stat.Hist.RecordValue(int64(latency)); err != nil {
				// TODO: поставить барьер на % таких ошибок. Если их много надо править диапазон
				lg.logError(err)
			}
		}

		if now := time.Now(); now.Sub(flushTime) >= 0 {
			lg.maybeFlush(false)
			flushTime = now.Add(time.Second)
		}

		if interval > 0 {
			tm.Reset(interval)
			select {
			case <-ctx.Done():
				return stat, nil
			case <-tm.C:
			}
		} else {
			runtime.Gosched()
		}
	}

	return stat, nil
}
