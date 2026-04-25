package app

import (
	"context"
	"errors"
	"net/http"
	"time"
)

type Worker interface {
	Run(context.Context) error
}

type App struct {
	server  *http.Server
	workers []Worker
}

func New(addr string, handler http.Handler, workers ...Worker) *App {
	return &App{
		server: &http.Server{
			Addr:    addr,
			Handler: handler,
		},
		workers: workers,
	}
}

func (a *App) Run(ctx context.Context) error {
	total := 1 + len(a.workers)
	errCh := make(chan error, total)

	go func() {
		err := a.server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	for _, worker := range a.workers {
		go func(worker Worker) {
			if err := worker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				errCh <- err
				return
			}
			errCh <- nil
		}(worker)
	}

	completed := 0
	for {
		select {
		case err := <-errCh:
			completed++
			if err != nil {
				return err
			}
			if completed == total {
				return nil
			}
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := a.server.Shutdown(shutdownCtx)
			cancel()
			if err != nil {
				return err
			}
			for completed < total {
				err := <-errCh
				completed++
				if err != nil {
					return err
				}
			}
			return nil
		}
	}
}
