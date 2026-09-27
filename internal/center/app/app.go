package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

type Worker interface {
	Run(context.Context) error
}

// ReadyAwareWorker observes whether the HTTP receiver can accept requests.
// Database reachability alone must never count as receiver availability.
type ReadyAwareWorker interface {
	SetReady(bool)
}

type App struct {
	server  *http.Server
	workers []Worker
}

func New(addr string, handler http.Handler, workers ...Worker) *App {
	return &App{
		server: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
			MaxHeaderBytes:    1 << 20,
		},
		workers: workers,
	}
}

func (a *App) ServerForTest() *http.Server {
	return a.server
}

func (a *App) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", a.server.Addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	setReady := func(ready bool) {
		for _, worker := range a.workers {
			if observer, ok := worker.(ReadyAwareWorker); ok {
				observer.SetReady(ready)
			}
		}
	}
	setReady(true)
	defer setReady(false)
	total := 1 + len(a.workers)
	errCh := make(chan error, total)

	go func() {
		err := a.server.Serve(listener)
		setReady(false)
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
			setReady(false)
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
