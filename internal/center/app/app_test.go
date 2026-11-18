package app_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	centerapp "houfeng/internal/center/app"
)

type fakeWorker struct {
	exited chan struct{}
}

type readinessWorker struct {
	ready   atomic.Bool
	started chan struct{}
}

func (w *readinessWorker) SetReady(ready bool) { w.ready.Store(ready) }
func (w *readinessWorker) Run(ctx context.Context) error {
	close(w.started)
	<-ctx.Done()
	return nil
}

func TestReceiverReadinessRequiresListenerAndEndsOnShutdown(t *testing.T) {
	worker := &readinessWorker{started: make(chan struct{})}
	app := centerapp.New("127.0.0.1:0", http.NewServeMux(), worker)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	select {
	case <-worker.started:
	case <-time.After(time.Second):
		t.Fatal("receiver did not start")
	}
	if !worker.ready.Load() {
		t.Fatal("listening receiver is not ready")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if worker.ready.Load() {
		t.Fatal("stopped receiver remains ready")
	}
}

func TestListenerFailureCannotStartHealthyObservation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	worker := &readinessWorker{started: make(chan struct{})}
	app := centerapp.New(listener.Addr().String(), http.NewServeMux(), worker)
	if err := app.Run(context.Background()); err == nil {
		t.Fatal("expected occupied listener error")
	}
	if worker.ready.Load() {
		t.Fatal("unavailable receiver is ready")
	}
	select {
	case <-worker.started:
		t.Fatal("worker started without receiver")
	default:
	}
}

func (f *fakeWorker) Run(ctx context.Context) error {
	<-ctx.Done()
	close(f.exited)
	return nil
}

func TestAppWaitsForWorkerShutdownBeforeReturning(t *testing.T) {
	worker := &fakeWorker{exited: make(chan struct{})}
	app := centerapp.New("127.0.0.1:0", http.NewServeMux(), worker)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}

	select {
	case <-worker.exited:
	case <-time.After(time.Second):
		t.Fatal("worker did not exit before Run() returned")
	}
}

type blockingWorker struct {
	exited  chan struct{}
	release <-chan struct{}
}

func (f *blockingWorker) Run(ctx context.Context) error {
	<-ctx.Done()
	<-f.release
	close(f.exited)
	return nil
}

func TestAppWaitsForMultipleWorkerShutdownBeforeReturning(t *testing.T) {
	release := make(chan struct{})
	workerA := &blockingWorker{exited: make(chan struct{}), release: release}
	workerB := &blockingWorker{exited: make(chan struct{}), release: release}
	app := centerapp.New("127.0.0.1:0", http.NewServeMux(), workerA, workerB)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		t.Fatalf("Run() returned before workers released: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	close(release)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after workers released")
	}

	select {
	case <-workerA.exited:
	default:
		t.Fatal("workerA did not exit before Run() returned")
	}

	select {
	case <-workerB.exited:
	default:
		t.Fatal("workerB did not exit before Run() returned")
	}
}

type workerFunc func(context.Context) error

func (worker workerFunc) Run(ctx context.Context) error { return worker(ctx) }

func TestAppWorkerFailureDrainsWorkersAndActiveRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	fail := make(chan struct{})
	workerCanceled := make(chan struct{})
	releaseWorker := make(chan struct{})
	releaseRequest := make(chan struct{})
	requestEntered := make(chan struct{})
	defer func() {
		select {
		case <-releaseWorker:
		default:
			close(releaseWorker)
		}
		select {
		case <-releaseRequest:
		default:
			close(releaseRequest)
		}
	}()
	wantErr := errors.New("worker failed")
	application := centerapp.New("127.0.0.1:0", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestEntered)
		<-releaseRequest
		w.WriteHeader(http.StatusNoContent)
	}), workerFunc(func(context.Context) error { <-fail; return wantErr }), workerFunc(func(ctx context.Context) error {
		<-ctx.Done()
		close(workerCanceled)
		<-releaseWorker
		return nil
	}))
	address := make(chan string, 1)
	application.ServerForTest().BaseContext = func(listener net.Listener) context.Context { address <- listener.Addr().String(); return ctx }
	done := make(chan error, 1)
	go func() { done <- application.Run(ctx) }()
	var endpoint string
	select {
	case endpoint = <-address:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	requestDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		response, err := client.Get("http://" + endpoint)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				err = fmt.Errorf("status = %d", response.StatusCode)
			}
		}
		requestDone <- err
	}()
	select {
	case <-requestEntered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	close(fail)
	select {
	case <-workerCanceled:
	case <-ctx.Done():
		t.Fatal("sibling worker was not canceled")
	}
	select {
	case err := <-done:
		t.Fatalf("returned before worker drain: %v", err)
	default:
	}
	close(releaseWorker)
	select {
	case err := <-done:
		t.Fatalf("returned before request drain: %v", err)
	default:
	}
	close(releaseRequest)
	select {
	case err := <-requestDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case err := <-done:
		if !errors.Is(err, wantErr) {
			t.Fatalf("Run() = %v, want worker error", err)
		}
	case <-ctx.Done():
		t.Fatal("application did not finish draining")
	}
}

func TestNewConfiguresHTTPServerTimeouts(t *testing.T) {
	app := centerapp.New("127.0.0.1:0", http.NewServeMux())
	server := app.ServerForTest()

	if server.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s, want 5s", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout != 30*time.Second {
		t.Fatalf("ReadTimeout = %s, want 30s", server.ReadTimeout)
	}
	if server.WriteTimeout != 30*time.Second {
		t.Fatalf("WriteTimeout = %s, want 30s", server.WriteTimeout)
	}
	if server.IdleTimeout != 60*time.Second {
		t.Fatalf("IdleTimeout = %s, want 60s", server.IdleTimeout)
	}
	if server.MaxHeaderBytes != 1<<20 {
		t.Fatalf("MaxHeaderBytes = %d, want 1MiB", server.MaxHeaderBytes)
	}
}
