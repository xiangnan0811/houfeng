package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
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
	server         *http.Server
	workers        []Worker
	handlerMu      sync.Mutex
	activeHandlers sync.WaitGroup
	stopping       bool
}

func New(addr string, handler http.Handler, workers ...Worker) *App {
	if handler == nil {
		handler = http.DefaultServeMux
	}
	a := &App{workers: workers}
	a.server = &http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			a.handlerMu.Lock()
			if a.stopping {
				a.handlerMu.Unlock()
				http.Error(w, "server stopping", http.StatusServiceUnavailable)
				return
			}
			a.activeHandlers.Add(1)
			a.handlerMu.Unlock()
			defer a.activeHandlers.Done()
			handler.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	return a
}

func (a *App) ServerForTest() *http.Server {
	return a.server
}

func (a *App) Run(ctx context.Context) error {
	ctx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()
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
	var runErr error
running:
	for completed < total {
		select {
		case err := <-errCh:
			completed++
			if err != nil {
				runErr = err
				break running
			}
		case <-ctx.Done():
			break running
		}
	}
	a.handlerMu.Lock()
	a.stopping = true
	a.handlerMu.Unlock()
	setReady(false)
	cancelWorkers()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	shutdownErr := a.server.Shutdown(shutdownCtx)
	cancel()
	if shutdownErr != nil {
		runErr = errors.Join(runErr, shutdownErr, a.server.Close())
	}
	for completed < total {
		runErr = errors.Join(runErr, <-errCh)
		completed++
	}
	a.activeHandlers.Wait()
	return runErr
}
