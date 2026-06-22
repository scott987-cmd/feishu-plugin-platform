// Package httpx provides production HTTP server helpers shared by the services:
// sane timeouts and graceful shutdown on SIGINT/SIGTERM.
package httpx

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

// NewServer builds an *http.Server with production-safe timeouts. WriteTimeout
// (90s) must exceed the drain budget, which must exceed the slowest downstream
// call (the /generate proxy's 60s client timeout): 90s > drain(75s) > 60s.
func NewServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

// drainTimeout is how long to wait for in-flight requests on shutdown. Sized to
// outlast the slowest request; override with SHUTDOWN_TIMEOUT_SECONDS.
func drainTimeout() time.Duration {
	if v := os.Getenv("SHUTDOWN_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 75 * time.Second
}

// Run serves until SIGINT/SIGTERM, then drains in-flight requests before
// returning. A drain-deadline overrun is logged as a warning, not an error, so
// callers don't treat a slow-but-normal shutdown as a fatal failure.
func Run(srv *http.Server) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		d := drainTimeout()
		log.Printf("shutdown signal received, draining (up to %s)...", d)
		shutCtx, cancel := context.WithTimeout(context.Background(), d)
		defer cancel()
		err := srv.Shutdown(shutCtx)
		if errors.Is(err, context.DeadlineExceeded) {
			log.Printf("drain deadline exceeded; some in-flight requests were cut")
			err = nil
		}
		// Surface any serve error that raced the shutdown.
		select {
		case serveErr := <-errCh:
			return serveErr
		default:
			return err
		}
	}
}
