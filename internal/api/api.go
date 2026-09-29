package api

import (
	"aaa2ppp/cdnnow-test-golang-14/internal/calculators"
	"aaa2ppp/cdnnow-test-golang-14/internal/metrics"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
)

type Service interface {
	Calculator
	RequestsCounter
	MetricsPrinter
}

func New(svc Service) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("POST /calc", calcHandler(svc))
	mux.Handle("GET /metrics", metricsHandler(svc))

	pong := func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(okBody) }
	mux.HandleFunc("GET /ping", pong)

	return mux
}

type Calculator interface {
	Calculate(num int64) error
}

type RequestsCounter interface {
	CountRequests(kind metrics.RequestKind)
}

var okBody = []byte("ok")

// TODO: костыль, убрать после admission control. Не трогать без перепроверки. См. TODO.md.git
const rejectTimeout = 5 * time.Millisecond

func calcHandler(svc interface {
	Calculator
	RequestsCounter
}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		numStr := r.URL.Query().Get("num")
		if numStr == "" {
			svc.CountRequests(metrics.BadRequest)
			http.Error(w, "missing 'num' query parameter", 400)
			return
		}

		num, err := strconv.ParseInt(numStr, 10, 64)
		if err != nil {
			svc.CountRequests(metrics.BadRequest)
			http.Error(w, "'num' must be an integer", 400)
			return
		}

		if err := svc.Calculate(num); err != nil {
			time.Sleep(rejectTimeout)
			switch {
			case errors.Is(err, calculators.ErrOverloaded):
				svc.CountRequests(metrics.Overload)
				http.Error(w, "calculator overloaded", http.StatusServiceUnavailable) // 503
			default:
				svc.CountRequests(metrics.Failed)
				log.Printf("calcHandler: calculate: %v", err)
				http.Error(w, "internal error", 500)
			}
			return
		}

		svc.CountRequests(metrics.Ok)

		if _, err = w.Write(okBody); err != nil {
			// Не считаем в метриках: запрос обслужен, ответ посчитан.
			// Ошибка Write - это разрыв соединения/клиент ушел, к нашей работе не относится.
			log.Printf("calcHandler: write response: %v", err)
		}
	}
}

type MetricsPrinter interface {
	PrintMetrics(w io.Writer) error
}

func metricsHandler(svc MetricsPrinter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		if err := svc.PrintMetrics(w); err != nil {
			log.Printf("metricsHandler: write response: %v", err)
		}
	}
}
