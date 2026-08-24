package deploy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordauthority"
)

func TestComposeAuthorityRuntimeHealthTracksOnlyVerifiedFreshMembership(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "records-authority")
	state, err := recordauthority.CreateComposeState(root)
	if err != nil {
		t.Fatalf("CreateComposeState() error = %v", err)
	}
	runtime, err := newComposeAuthorityRuntime(root, state)
	if err != nil {
		t.Fatalf("newComposeAuthorityRuntime() error = %v", err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	if runtime.healthyAt(now) {
		t.Fatal("authority is healthy before a verified database heartbeat")
	}

	wantExpiry := now.Add(90 * time.Second)
	heartbeats := 0
	err = runtime.renew(t.Context(), &pgxpool.Pool{}, now, composeAuthorityRuntimeDependencies{
		loadState: recordauthority.LoadComposeState,
		heartbeat: func(_ context.Context, _ *pgxpool.Pool, got recordauthority.VerifiedComposeState, issuedAt time.Time) (time.Time, error) {
			heartbeats++
			if got.DeploymentID != state.DeploymentID || issuedAt != now {
				t.Fatal("authority heartbeat did not use reverified state and supplied issue time")
			}
			return wantExpiry, nil
		},
	})
	if err != nil {
		t.Fatalf("Compose authority renewal error = %v", err)
	}
	if heartbeats != 1 || !runtime.healthyAt(now) || !runtime.healthyAt(wantExpiry.Add(-time.Nanosecond)) || runtime.healthyAt(wantExpiry) {
		t.Fatalf("authority health/heartbeat = %t/%d at bounded lease", runtime.healthyAt(now), heartbeats)
	}

	wantFailure := errors.New("database failure containing authority-secret")
	if err := runtime.renew(t.Context(), &pgxpool.Pool{}, now.Add(30*time.Second), composeAuthorityRuntimeDependencies{
		loadState: recordauthority.LoadComposeState,
		heartbeat: func(context.Context, *pgxpool.Pool, recordauthority.VerifiedComposeState, time.Time) (time.Time, error) {
			return time.Time{}, wantFailure
		},
	}); err == nil || errors.Is(err, wantFailure) {
		t.Fatalf("failed renewal error = %v, want redacted failure", err)
	}
	if !runtime.healthyAt(now.Add(89*time.Second)) || runtime.healthyAt(wantExpiry) {
		t.Fatal("failed renewal changed prior lease instead of expiring fail-closed")
	}
}

func TestComposeAuthorityHealthHandlerReflectsOnlyRuntimeLease(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "records-authority")
	state, err := recordauthority.CreateComposeState(root)
	if err != nil {
		t.Fatalf("CreateComposeState() error = %v", err)
	}
	runtime, err := newComposeAuthorityRuntime(root, state)
	if err != nil {
		t.Fatalf("newComposeAuthorityRuntime() error = %v", err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	handler := composeAuthorityHealthHandler(runtime, func() time.Time { return now })

	request := httptest.NewRequest(http.MethodGet, composeAuthorityHealthPath, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("pre-heartbeat health status = %d, want 503", response.Code)
	}

	err = runtime.renew(t.Context(), &pgxpool.Pool{}, now, composeAuthorityRuntimeDependencies{
		loadState: recordauthority.LoadComposeState,
		heartbeat: func(context.Context, *pgxpool.Pool, recordauthority.VerifiedComposeState, time.Time) (time.Time, error) {
			return now.Add(90 * time.Second), nil
		},
	})
	if err != nil {
		t.Fatalf("renew authority runtime: %v", err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("fresh health status = %d, want 204", response.Code)
	}

	for _, badRequest := range []*http.Request{
		httptest.NewRequest(http.MethodPost, composeAuthorityHealthPath, nil),
		httptest.NewRequest(http.MethodGet, "/", nil),
	} {
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, badRequest)
		if response.Code == http.StatusNoContent {
			t.Fatalf("health handler accepted %s %s", badRequest.Method, badRequest.URL.Path)
		}
	}
}

func TestRunComposeAuthorityUsesOnlyVerifiedStateCredentialAndStopsCleanly(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "records-authority")
	state, err := recordauthority.CreateComposeState(root)
	if err != nil {
		t.Fatalf("CreateComposeState() error = %v", err)
	}
	pool := &pgxpool.Pool{}
	ctx, cancel := context.WithCancel(t.Context())
	opened := 0
	closed := 0
	heartbeats := 0
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for authority test: %v", err)
	}
	err = runComposeAuthorityWithDependencies(ctx, root, listener.Addr().String(), composeAuthorityServiceDependencies{
		loadState: recordauthority.LoadComposeState,
		openPostgres: func(_ context.Context, endpoint composePostgresEndpoint) (*pgxpool.Pool, error) {
			opened++
			if endpoint.Host != composeDatabaseHost || endpoint.Port != composeDatabasePort || endpoint.Database != composeDatabaseName ||
				endpoint.Role != composeAuthorityRole || endpoint.Password != state.DatabasePassword() {
				t.Fatalf("authority PostgreSQL endpoint = %#v, want fixed role with state-only credential", endpoint)
			}
			return pool, nil
		},
		closePostgres: func(got *pgxpool.Pool) {
			if got != pool {
				t.Fatal("authority closed unknown PostgreSQL pool")
			}
			closed++
		},
		listen: func(network, address string) (net.Listener, error) {
			if network != "tcp" || address != listener.Addr().String() {
				t.Fatalf("authority listener = %q/%q, want configured loopback", network, address)
			}
			return listener, nil
		},
		now: func() time.Time { return time.Unix(1_800_000_000, 0).UTC() },
		heartbeat: func(context.Context, *pgxpool.Pool, recordauthority.VerifiedComposeState, time.Time) (time.Time, error) {
			heartbeats++
			cancel()
			return time.Unix(1_800_000_090, 0).UTC(), nil
		},
		newTicker: func(time.Duration) composeAuthorityTicker { return neverComposeAuthorityTicker{} },
	})
	if err != nil {
		t.Fatalf("runComposeAuthorityWithDependencies() error = %v", err)
	}
	if opened != 1 || closed != 1 || heartbeats != 1 {
		t.Fatalf("authority lifecycle opened=%d closed=%d heartbeats=%d, want 1/1/1", opened, closed, heartbeats)
	}
}

type neverComposeAuthorityTicker struct{}

func (neverComposeAuthorityTicker) C() <-chan time.Time { return make(chan time.Time) }
func (neverComposeAuthorityTicker) Stop()               {}

func TestComposeAuthorityRuntimeRejectsStateDriftBeforeHeartbeat(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "records-authority")
	state, err := recordauthority.CreateComposeState(root)
	if err != nil {
		t.Fatalf("CreateComposeState() error = %v", err)
	}
	runtime, err := newComposeAuthorityRuntime(root, state)
	if err != nil {
		t.Fatalf("newComposeAuthorityRuntime() error = %v", err)
	}
	deploymentIDPath := filepath.Join(root, "public", "deployment-id")
	if err := os.WriteFile(deploymentIDPath, []byte("dp-corrupt\n"), 0o644); err != nil {
		t.Fatalf("corrupt deployment identity: %v", err)
	}
	heartbeats := 0
	err = runtime.renew(t.Context(), &pgxpool.Pool{}, time.Unix(1_800_000_000, 0).UTC(), composeAuthorityRuntimeDependencies{
		loadState: recordauthority.LoadComposeState,
		heartbeat: func(context.Context, *pgxpool.Pool, recordauthority.VerifiedComposeState, time.Time) (time.Time, error) {
			heartbeats++
			return time.Time{}, nil
		},
	})
	if err == nil || heartbeats != 0 {
		t.Fatalf("state drift result = error %v/heartbeats %d, want fail before database mutation", err, heartbeats)
	}
}

func TestComposeAuthorityRuntimeUsesFixedInternalTopology(t *testing.T) {
	t.Parallel()

	if composeAuthorityRefreshInterval != 30*time.Second {
		t.Fatalf("authority refresh interval = %s, want 30s", composeAuthorityRefreshInterval)
	}
	if composeAuthorityHealthAddress != "127.0.0.1:16002" || composeAuthorityHealthPath != "/healthz" {
		t.Fatalf("authority health endpoint = %q%q, want loopback-only fixed endpoint", composeAuthorityHealthAddress, composeAuthorityHealthPath)
	}
}
