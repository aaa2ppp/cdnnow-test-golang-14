package api

import (
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"aaa2ppp/cdnnow-test-golang-14/internal/calculators"
	"aaa2ppp/cdnnow-test-golang-14/internal/metrics"
	"aaa2ppp/cdnnow-test-golang-14/internal/operators"

	"github.com/aaa2ppp/be"
	"github.com/valyala/fasthttp"
)

const (
	cLibPath    = "../../bin/libcalculator.so"
	rustLibPath = "../../bin/libcalculator_rust.so"
)

func init() {
	err := operators.LoadLibraries(cLibPath, rustLibPath)
	if err != nil {
		log.Fatal(err)
	}
}

type metricMap map[metrics.RequestKind]int

type mockService struct {
	metrics           metricMap
	calculateFunc     func(int64) error
	calculateCalls    int
	printMetricsFunc  func(io.Writer) error
	printMetricsCalls int
}

func (s *mockService) Calculate(num int64) error {
	s.calculateCalls++
	if s.calculateFunc != nil {
		return s.calculateFunc(num)
	}
	return nil
}

func (s *mockService) PrintMetrics(w io.Writer) error {
	s.printMetricsCalls++
	if s.printMetricsFunc != nil {
		return s.printMetricsFunc(w)
	}
	return nil
}

func (s *mockService) CountRequests(kind metrics.RequestKind) {
	if s.metrics == nil {
		s.metrics = metricMap{}
	}
	s.metrics[kind]++
}

func TestAPI(t *testing.T) {
	defer slog.SetDefault(slog.Default())
	slog.SetDefault(slog.New(slog.NewTextHandler(t.Output(), nil)))

	// NOTE: метрики считаются только на ручке `POST /calc`

	tests := []struct {
		name                  string
		request               string
		newService            func() *mockService
		asyncCalc             bool
		wantStatus            int
		wantCalculateCalls    int
		wantPrintMetricsCalls int
		wantMetrics           metricMap
	}{
		{
			name:               "success",
			request:            "POST /calc?num=42",
			wantStatus:         200,
			wantMetrics:        metricMap{metrics.Ok: 1},
			wantCalculateCalls: 1,
		},
		{
			name:               "async calc",
			request:            "POST /calc?num=42",
			asyncCalc:          true,
			wantStatus:         202,
			wantMetrics:        metricMap{metrics.Ok: 1},
			wantCalculateCalls: 1,
		},
		{
			name:       "unknown method",
			request:    "GET /calc?num=42",
			wantStatus: 405,
		},
		{
			name:       "unknown path",
			request:    "POST /unknown",
			wantStatus: 404,
		},
		{
			name:        "missing num",
			request:     "POST /calc",
			wantMetrics: metricMap{metrics.BadRequest: 1},
			wantStatus:  400,
		},
		{
			name:        "bad num",
			request:     "POST /calc?num=abc",
			wantMetrics: metricMap{metrics.BadRequest: 1},
			wantStatus:  400,
		},
		{
			name:    "overloaded",
			request: "POST /calc?num=42",
			newService: func() *mockService {
				return &mockService{
					calculateFunc: func(int64) error { return calculators.ErrOverloaded },
				}
			},
			wantCalculateCalls: 1,
			wantMetrics:        metricMap{metrics.Overload: 1},
			wantStatus:         503,
		},
		{
			name:    "unknown error",
			request: "POST /calc?num=42",
			newService: func() *mockService {
				return &mockService{
					calculateFunc: func(int64) error { return errors.New("unknown error") },
				}
			},
			wantCalculateCalls: 1,
			wantMetrics:        metricMap{metrics.Failed: 1},
			wantStatus:         500,
		},
		{
			name:                  "metrics",
			request:               "GET /metrics",
			wantPrintMetricsCalls: 1,
			wantStatus:            200,
		},
		{
			name:       "ping",
			request:    "GET /ping",
			wantStatus: 200,
		},
	}

	servers := []struct {
		name  string
		start func(t *testing.T, cfg Config) *testServer
	}{
		{
			"std",
			func(t *testing.T, cfg Config) *testServer { return startStdServer(t, NewStd(cfg)) },
		},
		{
			"fast",
			func(t *testing.T, cfg Config) *testServer { return startFastServer(t, NewFast(cfg)) },
		},
	}

	for _, svr := range servers {
		t.Run(svr.name, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					var svc *mockService
					if tt.newService == nil {
						svc = &mockService{}
					} else {
						svc = tt.newService()
					}

					server := svr.start(t, Config{Service: svc, AsyncCalc: tt.asyncCalc})
					defer server.Close()

					method, url, _ := strings.Cut(tt.request, " ")
					url = server.URL + url
					req, err := http.NewRequest(method, url, nil)
					be.Err(t, err, nil)

					client := http.DefaultClient
					resp, err := client.Do(req)
					be.Err(t, err, nil)
					be.Equal(t, resp.StatusCode, tt.wantStatus)

					_, err = io.Copy(io.Discard, resp.Body)
					be.Err(t, err, nil)

					err = resp.Body.Close()
					be.Err(t, err, nil)

					be.Equal(t, svc.calculateCalls, tt.wantCalculateCalls)
					be.Equal(t, svc.printMetricsCalls, tt.wantPrintMetricsCalls)
					be.Equal(t, svc.metrics, tt.wantMetrics)
				})
			}
		})
	}
}

type mockCalcService struct {
	Calculator
}

func (c *mockCalcService) Calculate(num int64) error {
	if c.Calculator != nil {
		return c.Calculator.Calculate(num)
	}
	return nil
}

func (c *mockCalcService) CountRequests(metrics.RequestKind) {}

func (c *mockCalcService) Stop() {
	if calc, ok := c.Calculator.(interface{ Stop() }); ok {
		calc.Stop()
	}
}

type calcCase struct {
	name      string
	newCalc   func() *mockCalcService
	asyncCalc bool
}

func calcCases() []calcCase {
	return []calcCase{
		{
			"stub calc",
			func() *mockCalcService {
				return &mockCalcService{}
			},
			false,
		},
		{
			"sync calc",
			func() *mockCalcService {
				return &mockCalcService{
					Calculator: calculators.NewSync(nil),
				}
			},
			false,
		},
		{
			"async calc",
			func() *mockCalcService {
				c := calculators.NewAsync(1024, nil, nil)
				c.IgnoreOverload()
				return &mockCalcService{
					Calculator: c,
				}
			},
			true,
		},
		{
			"parallel calc",
			func() *mockCalcService {
				c := calculators.NewParallel(1024, nil, nil)
				c.IgnoreOverload()
				return &mockCalcService{
					Calculator: c,
				}
			},
			true,
		},
	}
}

type serverCase struct {
	name  string
	start func(b *testing.B, calc *mockCalcService, asyncCalc bool) *testServer
}

func serverCases() []serverCase {
	return []serverCase{
		{
			"std",
			func(b *testing.B, calc *mockCalcService, asyncCalc bool) *testServer {
				return startStdServer(b, stdCalcHandler(calc, asyncCalc))
			},
		},
		{
			"fast",
			func(b *testing.B, calc *mockCalcService, asyncCalc bool) *testServer {
				return startFastServer(b, fastCalcHandler(calc, asyncCalc))
			},
		},
	}
}

func BenchmarkAPI(b *testing.B) {
	for _, svr := range serverCases() {
		b.Run(svr.name, func(b *testing.B) {
			for _, cs := range calcCases() {
				func() {
					calc := cs.newCalc()
					defer calc.Stop()

					server := svr.start(b, calc, cs.asyncCalc)
					defer server.Close()

					client := &fasthttp.Client{
						MaxConnsPerHost:           1,
						MaxIdemponentCallAttempts: 1,
						ReadTimeout:               5 * time.Second,
						WriteTimeout:              5 * time.Second,
					}

					req := fasthttp.AcquireRequest()
					defer fasthttp.ReleaseRequest(req)

					resp := fasthttp.AcquireResponse()
					defer fasthttp.ReleaseResponse(resp)

					req.SetRequestURI(server.URL + "/calc?num=42")
					req.Header.SetMethod("POST")

					b.Run(cs.name, func(b *testing.B) {
						for i := 0; i < b.N; i++ {
							err := client.Do(req, resp)
							if err != nil {
								b.Fatalf("Ошибка запроса: %v", err)
							}

							if !isOK(resp.StatusCode()) {
								b.Fatalf("Ожидался статус 2xx, получен %d", resp.StatusCode())
							}
						}
					})
				}()
			}
		})
	}
}

func BenchmarkAPIParallel(b *testing.B) {
	for _, svr := range serverCases() {
		b.Run(svr.name, func(b *testing.B) {
			for _, cs := range calcCases() {
				func() {
					calc := cs.newCalc()
					defer calc.Stop()

					server := svr.start(b, calc, cs.asyncCalc)
					defer server.Close()

					var failed atomic.Bool

					b.Run(cs.name, func(b *testing.B) {
						b.RunParallel(func(pb *testing.PB) {
							client := &fasthttp.Client{
								MaxConnsPerHost:           1,
								MaxIdemponentCallAttempts: 1,
								ReadTimeout:               5 * time.Second,
								WriteTimeout:              5 * time.Second,
							}

							req := fasthttp.AcquireRequest()
							defer fasthttp.ReleaseRequest(req)

							resp := fasthttp.AcquireResponse()
							defer fasthttp.ReleaseResponse(resp)

							req.SetRequestURI(server.URL + "/calc?num=42")
							req.Header.SetMethod("POST")

							for pb.Next() {
								if failed.Load() {
									return
								}

								err := client.Do(req, resp)
								if err != nil {
									failed.Store(true)
									b.Errorf("Ошибка запроса: %v", err)
									return
								}

								if !isOK(resp.StatusCode()) {
									failed.Store(true)
									b.Errorf("Ожидался статус 2xx, получен %d", resp.StatusCode())
									return
								}
							}
						})
					})
				}()
			}
		})
	}
}

func isOK(code int) bool {
	return code/100*100 == http.StatusOK
}

type testServer struct {
	URL   string
	Close func()
}

func startStdServer(t testing.TB, handler http.Handler) *testServer {
	t.Helper()

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	be.Err(t, err, nil)
	url := "http://" + listener.Addr().String()

	server := &http.Server{
		Handler: handler,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.Errorf("server.Serve: %v", err)
		}
	}()

	return &testServer{
		URL:   url,
		Close: func() { _ = server.Close(); <-done },
	}
}

func startFastServer(t testing.TB, handler fasthttp.RequestHandler) *testServer {
	t.Helper()

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	be.Err(t, err, nil)
	url := "http://" + listener.Addr().String()

	server := &fasthttp.Server{
		Handler: handler,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := server.Serve(listener); err != nil {
			t.Errorf("server.Serve: %v", err)
		}
	}()

	return &testServer{
		URL:   url,
		Close: func() { _ = listener.Close(); <-done },
	}
}
