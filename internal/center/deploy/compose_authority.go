package deploy

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordauthority"
)

const (
	composeAuthorityRefreshInterval = 30 * time.Second
	composeAuthorityHealthAddress   = "127.0.0.1:16002"
	composeAuthorityHealthPath      = "/healthz"
)

var (
	ErrComposeAuthorityLoadState = errors.New("load Compose Records authority state failed")
	ErrComposeAuthorityOpenDB    = errors.New("open Compose Records authority PostgreSQL connection failed")
	ErrComposeAuthorityListen    = errors.New("start Compose Records authority health listener failed")
	ErrComposeAuthorityHeartbeat = errors.New("renew Compose Records authority membership failed")
	ErrComposeAuthorityServe     = errors.New("serve Compose Records authority health endpoint failed")
)

type composeAuthorityRuntimeDependencies struct {
	loadState func(string) (recordauthority.VerifiedComposeState, error)
	heartbeat func(context.Context, *pgxpool.Pool, recordauthority.VerifiedComposeState, time.Time) (time.Time, error)
}

type composeAuthorityRuntime struct {
	root     string
	baseline recordauthority.VerifiedComposeState

	mu        sync.RWMutex
	expiresAt time.Time
}

type composeAuthorityTicker interface {
	C() <-chan time.Time
	Stop()
}

type systemComposeAuthorityTicker struct {
	ticker *time.Ticker
}

func (ticker systemComposeAuthorityTicker) C() <-chan time.Time { return ticker.ticker.C }
func (ticker systemComposeAuthorityTicker) Stop()               { ticker.ticker.Stop() }

type composeAuthorityServiceDependencies struct {
	loadState     func(string) (recordauthority.VerifiedComposeState, error)
	openPostgres  func(context.Context, composePostgresEndpoint) (*pgxpool.Pool, error)
	closePostgres func(*pgxpool.Pool)
	listen        func(string, string) (net.Listener, error)
	now           func() time.Time
	heartbeat     func(context.Context, *pgxpool.Pool, recordauthority.VerifiedComposeState, time.Time) (time.Time, error)
	newTicker     func(time.Duration) composeAuthorityTicker
}

// RunComposeAuthority runs the fixed single-host authority. It owns only the
// constrained heartbeat credential recovered from durable signed state.
func RunComposeAuthority(ctx context.Context) error {
	return runComposeAuthorityWithDependencies(ctx, composeAuthorityStateRoot, composeAuthorityHealthAddress, composeAuthorityServiceDependencies{
		loadState:    recordauthority.LoadComposeState,
		openPostgres: openComposePostgres,
		closePostgres: func(pool *pgxpool.Pool) {
			pool.Close()
		},
		listen:    net.Listen,
		now:       func() time.Time { return time.Now().UTC() },
		heartbeat: heartbeatComposeAuthorityAt,
		newTicker: func(interval time.Duration) composeAuthorityTicker {
			return systemComposeAuthorityTicker{ticker: time.NewTicker(interval)}
		},
	})
}

func runComposeAuthorityWithDependencies(
	ctx context.Context,
	stateRoot string,
	healthAddress string,
	deps composeAuthorityServiceDependencies,
) error {
	if ctx == nil || !validComposeAuthorityStateRoot(stateRoot) || !validComposeAuthorityHealthAddress(healthAddress) ||
		deps.loadState == nil || deps.openPostgres == nil || deps.closePostgres == nil || deps.listen == nil ||
		deps.now == nil || deps.heartbeat == nil || deps.newTicker == nil {
		return ErrComposeAuthorityLoadState
	}
	state, err := deps.loadState(stateRoot)
	if err != nil {
		return ErrComposeAuthorityLoadState
	}
	runtime, err := newComposeAuthorityRuntime(stateRoot, state)
	if err != nil {
		return ErrComposeAuthorityLoadState
	}
	pool, err := deps.openPostgres(ctx, composePostgresEndpoint{
		Host:     composeDatabaseHost,
		Port:     composeDatabasePort,
		Database: composeDatabaseName,
		Role:     composeAuthorityRole,
		Password: state.DatabasePassword(),
	})
	if pool != nil && err != nil {
		deps.closePostgres(pool)
	}
	if err != nil || pool == nil {
		return ErrComposeAuthorityOpenDB
	}
	defer deps.closePostgres(pool)

	listener, err := deps.listen("tcp", healthAddress)
	if err != nil || listener == nil {
		if listener != nil {
			_ = listener.Close()
		}
		return ErrComposeAuthorityListen
	}
	defer listener.Close()
	server := &http.Server{
		Handler:           composeAuthorityHealthHandler(runtime, deps.now),
		ReadHeaderTimeout: 2 * time.Second,
		IdleTimeout:       5 * time.Second,
	}
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.Serve(listener)
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	runtimeDeps := composeAuthorityRuntimeDependencies{
		loadState: deps.loadState,
		heartbeat: deps.heartbeat,
	}
	if err := runtime.renew(ctx, pool, deps.now(), runtimeDeps); err != nil {
		return ErrComposeAuthorityHeartbeat
	}
	ticker := deps.newTicker(composeAuthorityRefreshInterval)
	if ticker == nil {
		return ErrComposeAuthorityHeartbeat
	}
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C():
			if err := runtime.renew(ctx, pool, deps.now(), runtimeDeps); err != nil {
				return ErrComposeAuthorityHeartbeat
			}
		case serveErr := <-serveResult:
			if errors.Is(serveErr, http.ErrServerClosed) && ctx.Err() != nil {
				return nil
			}
			return ErrComposeAuthorityServe
		}
	}
}

func validComposeAuthorityHealthAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	return err == nil && host == "127.0.0.1" && port != "" && port != "0"
}

func composeAuthorityHealthHandler(runtime *composeAuthorityRuntime, now func() time.Time) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != composeAuthorityHealthPath {
			http.NotFound(response, request)
			return
		}
		if request.Method != http.MethodGet {
			response.Header().Set("Allow", http.MethodGet)
			response.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		response.Header().Set("Cache-Control", "no-store")
		if runtime == nil || now == nil || !runtime.healthyAt(now()) {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	})
}

func newComposeAuthorityRuntime(root string, baseline recordauthority.VerifiedComposeState) (*composeAuthorityRuntime, error) {
	canonical, err := recordauthority.LoadComposeState(root)
	if err != nil || !sameVerifiedComposeAuthorityState(canonical, baseline) {
		return nil, errors.New("Compose Records authority startup state is invalid")
	}
	return &composeAuthorityRuntime{root: root, baseline: canonical}, nil
}

func (runtime *composeAuthorityRuntime) renew(
	ctx context.Context,
	pool *pgxpool.Pool,
	issuedAt time.Time,
	deps composeAuthorityRuntimeDependencies,
) error {
	if runtime == nil || pool == nil || deps.loadState == nil || deps.heartbeat == nil {
		return errors.New("Compose Records authority renewal dependencies are invalid")
	}
	state, err := deps.loadState(runtime.root)
	if err != nil || !sameVerifiedComposeAuthorityState(state, runtime.baseline) {
		return errors.New("Compose Records authority durable state verification failed")
	}
	_, wantExpiry, err := recordauthority.MarshalMembershipHeartbeatCommandV1(state, issuedAt)
	if err != nil {
		return errors.New("Compose Records authority heartbeat command is invalid")
	}
	gotExpiry, err := deps.heartbeat(ctx, pool, state, issuedAt)
	if err != nil {
		return errors.New("Compose Records authority database heartbeat failed")
	}
	if !gotExpiry.UTC().Equal(wantExpiry) {
		return errors.New("Compose Records authority database heartbeat receipt is invalid")
	}
	runtime.mu.Lock()
	runtime.expiresAt = wantExpiry
	runtime.mu.Unlock()
	return nil
}

func (runtime *composeAuthorityRuntime) healthyAt(now time.Time) bool {
	if runtime == nil {
		return false
	}
	runtime.mu.RLock()
	expiresAt := runtime.expiresAt
	runtime.mu.RUnlock()
	return !expiresAt.IsZero() && now.Before(expiresAt)
}

func sameVerifiedComposeAuthorityState(left, right recordauthority.VerifiedComposeState) bool {
	if left.DeploymentID != right.DeploymentID || left.DatabasePassword() != right.DatabasePassword() ||
		len(left.Memberships) != len(right.Memberships) {
		return false
	}
	for index := range left.Memberships {
		if left.Memberships[index] != right.Memberships[index] {
			return false
		}
	}
	leftCommand, leftErr := left.ActivationCommand.MarshalBinary()
	rightCommand, rightErr := right.ActivationCommand.MarshalBinary()
	return leftErr == nil && rightErr == nil && bytes.Equal(leftCommand, rightCommand)
}
