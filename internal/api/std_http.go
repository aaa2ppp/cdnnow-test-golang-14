package api

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"aaa2ppp/cdnnow-test-golang-14/internal/calculators"
	"aaa2ppp/cdnnow-test-golang-14/internal/metrics"
)

func NewStd(cfg Config) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("POST /calc", stdCalcHandler(cfg.Service, cfg.AsyncCalc))
	mux.Handle("GET /metrics", stdMetricsHandler(cfg.Service))

	pong := func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(okBody) }
	mux.HandleFunc("GET /ping", pong)

	return mux
}

func stdCalcHandler(
	svc interface {
		Calculator
		RequestsCounter
	},
	asyncCalc bool,
) http.HandlerFunc {
	statusOk := 200
	if asyncCalc {
		statusOk = http.StatusAccepted // 202
	}

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
			switch {
			case errors.Is(err, calculators.ErrOverloaded):
				svc.CountRequests(metrics.Overload)
				http.Error(w, "calculator overloaded", http.StatusServiceUnavailable) // 503
			default:
				svc.CountRequests(metrics.Failed)
				log.Printf("stdCalcHandler: calculate: %v", err)
				http.Error(w, "internal error", 500)
			}
			return
		}

		svc.CountRequests(metrics.Ok)

		w.WriteHeader(statusOk)
		if _, err = w.Write(okBody); err != nil {
			// Не считаем в метриках: запрос обслужен, ответ посчитан.
			// Ошибка Write - это разрыв соединения/клиент ушел, к нашей работе не относится.
			log.Printf("stdCalcHandler: write response: %v", err)
		}
	}
}

func stdMetricsHandler(svc MetricsPrinter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		if err := svc.PrintMetrics(w); err != nil {
			log.Printf("stdMetricsHandler: write response: %v", err)
		}
	}
}
