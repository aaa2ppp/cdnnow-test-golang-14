package api

import (
	"errors"
	"log"
	"strconv"

	"github.com/valyala/fasthttp"

	"aaa2ppp/cdnnow-test-golang-14/internal/calculators"
	"aaa2ppp/cdnnow-test-golang-14/internal/metrics"
)

func NewFast(cfg Config) fasthttp.RequestHandler {
	calc := fastCalcHandler(cfg.Service, cfg.AsyncCalc)
	metrics := fastMetricsHandler(cfg.Service)

	return func(ctx *fasthttp.RequestCtx) {
		switch unsafeString(ctx.Path()) {
		case "/calc":
			if !ctx.IsPost() {
				ctx.SetStatusCode(fasthttp.StatusMethodNotAllowed) // 405
				return
			}
			calc(ctx)
		case "/metrics":
			if !ctx.IsGet() {
				ctx.SetStatusCode(fasthttp.StatusMethodNotAllowed) // 405
				return
			}
			metrics(ctx)
		case "/ping":
			if !ctx.IsGet() {
				ctx.SetStatusCode(fasthttp.StatusMethodNotAllowed) // 405
				return
			}
			_, _ = ctx.Write(okBody)
		default:
			ctx.SetStatusCode(fasthttp.StatusNotFound) // 404
		}
	}
}

func fastCalcHandler(
	svc interface {
		Calculator
		RequestsCounter
	},
	asyncCalc bool,
) fasthttp.RequestHandler {
	statusOk := 200
	if asyncCalc {
		statusOk = fasthttp.StatusAccepted // 202
	}

	return func(ctx *fasthttp.RequestCtx) {
		numStr := string(ctx.QueryArgs().Peek("num"))
		if numStr == "" {
			svc.CountRequests(metrics.BadRequest)
			ctx.Error("missing 'num' query parameter", 400)
			return
		}

		num, err := strconv.ParseInt(numStr, 10, 64)
		if err != nil {
			svc.CountRequests(metrics.BadRequest)
			ctx.Error("'num' must be an integer", 400)
			return
		}

		if err := svc.Calculate(num); err != nil {
			switch {
			case errors.Is(err, calculators.ErrOverloaded):
				svc.CountRequests(metrics.Overload)
				ctx.Error("calculator overloaded", fasthttp.StatusServiceUnavailable) // 503
			default:
				svc.CountRequests(metrics.Failed)
				log.Printf("fastCalcHandler: calculate: %v", err)
				ctx.Error("internal error", 500)
			}
			return
		}

		svc.CountRequests(metrics.Ok)

		ctx.SetStatusCode(statusOk)
		if _, err := ctx.Write(okBody); err != nil {
			// Не считаем в метриках: запрос обслужен, ответ посчитан.
			// Ошибка Write - это разрыв соединения/клиент ушел, к нашей работе не относится.
			log.Printf("fastCalcHandler: write response: %v", err)
		}
	}
}

func fastMetricsHandler(svc MetricsPrinter) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		ctx.Response.Header.Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		ctx.Response.Header.Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		ctx.Response.Header.Set("Pragma", "no-cache")
		ctx.Response.Header.Set("Expires", "0")
		if err := svc.PrintMetrics(ctx); err != nil {
			log.Printf("fastMetricsHandler: write response: %v", err)
		}
	}
}
