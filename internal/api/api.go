package api

import (
	"aaa2ppp/cdnnow-test-golang-14/internal/calculators"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
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
	CountRequests()
}

var okBody = []byte("ok")

func calcHandler(svc interface {
	Calculator
	RequestsCounter
}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc.CountRequests() // считаем все запросы

		numStr := r.URL.Query().Get("num")
		if numStr == "" {
			http.Error(w, "missing 'num' query parameter", 400)
			return
		}

		num, err := strconv.ParseInt(numStr, 10, 64)
		if err != nil {
			http.Error(w, "'num' must be an integer", 400)
			return
		}

		if err := svc.Calculate(num); err != nil {
			// TODO: надо считать ошибки
			status := 500
			if errors.Is(err, calculators.ErrOverloaded) {
				status = 503
			}
			http.Error(w, err.Error(), status)
			return
		}

		if _, err = w.Write(okBody); err != nil {
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
