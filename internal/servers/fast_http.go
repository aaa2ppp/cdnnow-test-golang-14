package servers

import (
	"context"
	"log/slog"
	"net"
	"time"

	"github.com/valyala/fasthttp"

	"aaa2ppp/cdnnow-test-golang-14/internal/api"
)

func runFastHTTPServer(ctx context.Context, listener net.Listener, svc *service) error {
	handler := api.NewFast(api.Config{
		Service:   svc,
		AsyncCalc: svc.asyncCalc,
	})

	server := &fasthttp.Server{
		Handler:               handler,
		ReadTimeout:           2 * time.Second,
		WriteTimeout:          2 * time.Second,
		IdleTimeout:           30 * time.Second,
		SecureErrorLogMessage: true,
	}

	done := make(chan error, 1)
	go func() {
		defer close(done)
		done <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutdown server", "cause", context.Cause(ctx))
		if err := server.Shutdown(); err != nil {
			return err
		}
		return nil
	case err := <-done:
		return err
	}
}
