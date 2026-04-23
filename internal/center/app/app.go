package app

import (
	"context"
	"errors"
	"net/http"
	"time"
)

type App struct {
	server *http.Server
}

func New(addr string, handler http.Handler) *App {
	return &App{
		server: &http.Server{
			Addr:    addr,
			Handler: handler,
		},
	}
}

func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		err := a.server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := a.server.Shutdown(shutdownCtx); err != nil {
			return err
		}

		return <-errCh
	}
}
