package servers

import (
	"context"
	"log/slog"
	"net"
	"time"

	"github.com/valyala/fasthttp"
)

func runFastHTTPServer(ctx context.Context, listener net.Listener, handler fasthttp.RequestHandler) error {
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
