package servers

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"
)

func runStdHTTPServer(ctx context.Context, listener net.Listener, handler http.Handler) error {
	server := http.Server{
		Handler:      handler,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	done := make(chan error, 1)
	go func() {
		defer close(done)
		done <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutdown server", "cause", context.Cause(ctx))
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			return err
		}
		return nil
	case err := <-done:
		return err
	}
}
