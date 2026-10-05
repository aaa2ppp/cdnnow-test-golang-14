package api

import (
	"io"

	"aaa2ppp/cdnnow-test-golang-14/internal/metrics"
)

type Service interface {
	Calculator
	RequestsCounter
	MetricsPrinter
}

type Config struct {
	Service   Service
	AsyncCalc bool
}

type Calculator interface {
	Calculate(num int64) error
}

type RequestsCounter interface {
	CountRequests(kind metrics.RequestKind)
}

type MetricsPrinter interface {
	PrintMetrics(w io.Writer) error
}

var okBody = []byte("ok")
