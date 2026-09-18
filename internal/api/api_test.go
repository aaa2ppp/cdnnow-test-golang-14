package api

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aaa2ppp/cdnnow-test-golang-14/internal/calculators"
	"aaa2ppp/cdnnow-test-golang-14/internal/operators"

	"github.com/aaa2ppp/be"
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

type mockService struct {
	calls int
	count int

	calculateFunc    func(int64) error
	printMetricsFunc func(io.Writer) error
}

func (s *mockService) Calculate(num int64) error {
	s.calls++
	if s.calculateFunc != nil {
		return s.calculateFunc(num)
	}
	return nil
}

func (s *mockService) PrintMetrics(w io.Writer) error {
	s.calls++
	if s.printMetricsFunc != nil {
		return s.printMetricsFunc(w)
	}
	return nil
}

func (s *mockService) CountRequests() {
	s.count++
}

func TestAPI(t *testing.T) {
	tests := []struct {
		name       string
		request    string
		newService func() *mockService
		wantStatus int
		wantCalls  int
	}{
		{
			name:       "success",
			request:    "POST /calc?num=42",
			wantStatus: 200,
			wantCalls:  1,
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
			name:       "missing num",
			request:    "POST /calc",
			wantStatus: 400,
		},
		{
			name:       "bad num",
			request:    "POST /calc?num=abc",
			wantStatus: 400,
		},
		{
			name:    "overloaded",
			request: "POST /calc?num=42",
			newService: func() *mockService {
				return &mockService{
					calculateFunc: func(int64) error { return calculators.ErrOverloaded },
				}
			},
			wantStatus: 503,
			wantCalls:  1,
		},
		{
			name:    "unknown error",
			request: "POST /calc?num=42",
			newService: func() *mockService {
				return &mockService{
					calculateFunc: func(int64) error { return errors.New("unknown error") },
				}
			},
			wantStatus: 500,
			wantCalls:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var svc *mockService
			if tt.newService == nil {
				svc = &mockService{}
			} else {
				svc = tt.newService()
			}

			router := New(svc)
			server := httptest.NewServer(router)

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

			be.Equal(t, svc.calls, tt.wantCalls)
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

func (c *mockCalcService) CountRequests() {}

func BenchmarkAPI(b *testing.B) {
	type service interface {
		Calculator
		RequestsCounter
	}

	cases := []struct {
		name    string
		newCalc func() service
	}{
		{
			"stub calc",
			func() service {
				return &mockCalcService{}
			},
		},
		{
			"sync calc",
			func() service {
				return &mockCalcService{
					Calculator: &calculators.SyncCalculator{},
				}
			},
		},
		{
			"async calc",
			func() service {
				c := calculators.NewAsyncCalculator(1024, nil, nil)
				c.IgnoreOverload()
				return &mockCalcService{
					Calculator: c,
				}
			},
		},
		{
			"parallel calc",
			func() service {
				c := calculators.NewParallelCalculator(1024, nil, nil)
				c.IgnoreOverload()
				return &mockCalcService{
					Calculator: c,
				}
			},
		},
	}

	type stopper interface{ Stop() }

	for _, cs := range cases {
		b.Run(cs.name, func(b *testing.B) {
			calc := cs.newCalc()
			if calc, ok := calc.(stopper); ok {
				defer calc.Stop()
			}

			server := httptest.NewServer(calcHandler(calc))

			templ, _ := http.NewRequest("POST", server.URL+"/calc?num=42", nil)
			ctx := context.Background()

			client := http.DefaultClient

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				req := templ.Clone(ctx)
				resp, _ := client.Do(req)
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
		})
	}
}

func BenchmarkAPIParallel(b *testing.B) {
	type service interface {
		Calculator
		RequestsCounter
	}

	cases := []struct {
		name    string
		newCalc func() service
	}{
		{
			"stub calc",
			func() service {
				return &mockCalcService{}
			},
		},
		{
			"sync calc",
			func() service {
				return &mockCalcService{
					Calculator: &calculators.SyncCalculator{},
				}
			},
		},
		{
			"async calc",
			func() service {
				c := calculators.NewAsyncCalculator(1024, nil, nil)
				c.IgnoreOverload()
				return &mockCalcService{
					Calculator: c,
				}
			},
		},
		{
			"parallel calc",
			func() service {
				c := calculators.NewParallelCalculator(1024, nil, nil)
				c.IgnoreOverload()
				return &mockCalcService{
					Calculator: c,
				}
			},
		},
	}

	type stopper interface{ Stop() }

	for _, cs := range cases {
		b.Run(cs.name, func(b *testing.B) {
			calc := cs.newCalc()
			if calc, ok := calc.(stopper); ok {
				defer calc.Stop()
			}

			server := httptest.NewServer(calcHandler(calc))

			templReq, _ := http.NewRequest("POST", server.URL+"/calc?num=42", nil)
			ctx := context.Background()

			client := &http.Client{
				Transport: &http.Transport{
					MaxIdleConns:        100,
					MaxIdleConnsPerHost: 100,
					MaxConnsPerHost:     100,
				},
			}

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					req := templReq.Clone(ctx)
					resp, _ := client.Do(req)
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
				}
			})
		})
	}
}
