package api_test

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aaa2ppp/cdnnow-test-golang-14/internal/api"
	"aaa2ppp/cdnnow-test-golang-14/internal/calculator"
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
	calls       int
	processFunc func(int64)
}

func (s *mockService) Process(num int64) {
	s.calls++
	if s.processFunc != nil {
		s.processFunc(num)
	}
}

func TestCalc(t *testing.T) {

	tests := []struct {
		name       string
		request    string
		svc        mockService
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := tt.svc
			router := api.New(&svc)
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

type stubService struct{}

func (stubService) Process(num int64) {}

func BenchmarkCalc(b *testing.B) {
	type startSvcFunc func() (api.Service, func())

	cases := []struct {
		name     string
		startSvc startSvcFunc
	}{
		{
			"stub",
			func() (api.Service, func()) { return stubService{}, func() {} },
		},
		{
			"mutext calculator",
			func() (api.Service, func()) { return &calculator.MutextCalculator{}, func() {} },
		},
		{
			"channel calculator",
			func() (api.Service, func()) { return calculator.NewChannelCalculator() },
		},
	}

	for _, cs := range cases {
		b.Run(cs.name, func(b *testing.B) {
			svc, cancel := cs.startSvc()
			defer cancel()

			router := api.New(svc)
			server := httptest.NewServer(router)

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

func BenchmarkCalcParallel(b *testing.B) {
	type startSvcFunc func() (api.Service, func())

	cases := []struct {
		name     string
		startSvc startSvcFunc
	}{
		{
			"stub",
			func() (api.Service, func()) { return stubService{}, func() {} },
		},
		{
			"mutext calculator",
			func() (api.Service, func()) { return &calculator.MutextCalculator{}, func() {} },
		},
		{
			"channel calculator",
			func() (api.Service, func()) { return calculator.NewChannelCalculator() },
		},
	}

	for _, cs := range cases {
		b.Run(cs.name, func(b *testing.B) {
			svc, cancel := cs.startSvc()
			defer cancel()

			router := api.New(svc)
			server := httptest.NewServer(router)

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
