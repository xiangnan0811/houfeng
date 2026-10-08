package store_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/auth"
	centerhttp "houfeng/internal/center/http"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/runtimefacts"
	"houfeng/internal/center/store"
	storemigrate "houfeng/internal/center/store/migrate"
	"houfeng/internal/center/syncing"
)

const (
	runtimeStreamAuthPassword     = "correct-horse-battery"
	runtimeStreamAuthNewPassword  = "Rotated-Credential-2026!"
	runtimeStreamAuthHMACKey      = "0123456789abcdef0123456789abcdef"
	runtimeStreamAuthUserID       = "usr_0123456789abcdef01234567"
	runtimeStreamAuthUsername     = "runtime_stream_admin"
	runtimeStreamAuthMonitoringID = "mi_0123456789abcdef"
	runtimeStreamAuthFingerprint  = "fp-runtime-stream-auth"
	runtimeStreamAuthSessionProbe = "/api/runtime-auth-probe"
)

type runtimeStreamAuthClock struct {
	mu      sync.Mutex
	current time.Time
}

func newRuntimeStreamAuthClock(value time.Time) *runtimeStreamAuthClock {
	return &runtimeStreamAuthClock{current: value.UTC().Truncate(time.Microsecond)}
}

func (c *runtimeStreamAuthClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

func (c *runtimeStreamAuthClock) Set(value time.Time) {
	c.mu.Lock()
	c.current = value.UTC().Truncate(time.Microsecond)
	c.mu.Unlock()
}

func (c *runtimeStreamAuthClock) Advance(delta time.Duration) {
	c.mu.Lock()
	c.current = c.current.Add(delta)
	c.mu.Unlock()
}

type runtimeStreamAuthFixture struct {
	pool       *pgxpool.Pool
	clock      *runtimeStreamAuthClock
	service    *auth.Service
	scope      *store.PostgresRecordAuthorizationRepository
	monitoring *store.PostgresMonitoringInstanceRepository
	hub        *runtimeStreamAuthHub
}

func newRuntimeStreamAuthFixture(t *testing.T) *runtimeStreamAuthFixture {
	t.Helper()
	ctx := context.Background()
	pool := openRuntimeStreamAuthPostgresSchema(t, ctx)
	base := time.Now().UTC().Truncate(time.Microsecond)
	clock := newRuntimeStreamAuthClock(base)
	hash, err := auth.HashPassword(runtimeStreamAuthPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into users (user_id, username, password_hash, display_name, role, created_at, password_changed_at)
		values ($1, $2, $3, $4, $5, $6, $6)`, runtimeStreamAuthUserID, runtimeStreamAuthUsername, hash, "Runtime stream admin", auth.RoleAdmin, base); err != nil {
		t.Fatalf("insert runtime stream user: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into vps_assets (vps_id, display_name, lifecycle_status)
		values ($1, $2, 'active')`, "vps_"+runtimeStreamAuthMonitoringID, "Runtime stream auth VPS"); err != nil {
		t.Fatalf("insert runtime stream auth VPS: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into monitoring_instances (
			monitoring_instance_id, vps_id, display_name, "group", region, city, provider,
			lifecycle_status, monitoring_status, binding_status, binding_fingerprint,
			binding_epoch_started_at, archived_at
		) values ($1, $2, $1, 'test', 'test-region', 'test-city', 'test-provider',
			'已接入', '启用', '已绑定', $3, $4, null)`,
		runtimeStreamAuthMonitoringID, "vps_"+runtimeStreamAuthMonitoringID, runtimeStreamAuthFingerprint, base.Add(-2*time.Hour)); err != nil {
		t.Fatalf("insert runtime stream monitoring instance: %v", err)
	}
	sessions, err := store.NewPostgresSessionRepository(pool, []byte(runtimeStreamAuthHMACKey))
	if err != nil {
		t.Fatalf("NewPostgresSessionRepository: %v", err)
	}
	users := store.NewPostgresUserRepository(pool)
	service := auth.New(users, sessions, auth.Options{SessionTTL: time.Hour, Now: clock.Now})
	return &runtimeStreamAuthFixture{
		pool:       pool,
		clock:      clock,
		service:    service,
		scope:      store.NewPostgresRecordAuthorizationRepository(pool),
		monitoring: store.NewPostgresMonitoringInstanceRepository(pool),
		hub:        &runtimeStreamAuthHub{StreamHub: runtimefacts.NewStreamHub(), subscribed: make(chan string, 32), subscriptionEvents: make(chan runtimeStreamAuthSubscriptionEvent, 64)},
	}
}

func openRuntimeStreamAuthPostgresSchema(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("HOUFENG_POSTGRES_INTEGRATION=1 is required for runtime stream auth PostgreSQL integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("HOUFENG_DATABASE_URL is required for runtime stream auth PostgreSQL integration tests")
	}
	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse HOUFENG_DATABASE_URL: %v", err)
	}
	databaseName := fmt.Sprintf("houfeng_runtime_stream_auth_%d_%d", time.Now().UnixNano(), os.Getpid())
	if !regexp.MustCompile(`^[a-z_][a-z0-9_]*$`).MatchString(databaseName) {
		t.Fatalf("unsafe generated database name %q", databaseName)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatalf("open postgres admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)
	quotedDatabase := `"` + strings.ReplaceAll(databaseName, `"`, `""`) + `"`
	if _, err := adminPool.Exec(ctx, `create database `+quotedDatabase); err != nil {
		t.Fatalf("create temporary postgres database %q: %v", databaseName, err)
	}
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := adminPool.Exec(dropCtx, `drop database if exists `+quotedDatabase+` with (force)`); err != nil {
			t.Errorf("drop temporary postgres database %q: %v", databaseName, err)
		}
	})
	testConfig := adminConfig.Copy()
	testConfig.ConnConfig.Database = databaseName
	testPool, err := pgxpool.NewWithConfig(ctx, testConfig)
	if err != nil {
		t.Fatalf("open temporary postgres database %q: %v", databaseName, err)
	}
	t.Cleanup(testPool.Close)
	if err := storemigrate.Apply(ctx, testPool); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return testPool
}

type runtimeStreamAuthSubscriptionEvent struct {
	monitoringInstanceID string
	subscription         runtimefacts.HostSampleSubscription
}

type runtimeStreamAuthHub struct {
	*runtimefacts.StreamHub
	subscribed         chan string
	subscriptionEvents chan runtimeStreamAuthSubscriptionEvent
}

func (h *runtimeStreamAuthHub) SubscribeHostSamples(monitoringInstanceID string) runtimefacts.HostSampleSubscription {
	subscription := h.StreamHub.SubscribeHostSamples(monitoringInstanceID)
	select {
	case h.subscribed <- monitoringInstanceID:
	default:
	}
	if h.subscriptionEvents != nil {
		h.subscriptionEvents <- runtimeStreamAuthSubscriptionEvent{
			monitoringInstanceID: monitoringInstanceID,
			subscription:         subscription,
		}
	}
	return subscription
}

type runtimeStreamAuthClosedHub struct{}

func (runtimeStreamAuthClosedHub) SubscribeHostSamples(string) runtimefacts.HostSampleSubscription {
	messages := make(chan runtimefacts.HostSampleStreamMessage)
	close(messages)
	return runtimefacts.HostSampleSubscription{Messages: messages}
}

type runtimeStreamAuthValidator struct {
	service *auth.Service
	calls   chan struct{}
	mu      sync.RWMutex
	failure error
}

func (v *runtimeStreamAuthValidator) ValidateSession(ctx context.Context, sessionID string) error {
	if v.calls != nil {
		select {
		case v.calls <- struct{}{}:
		default:
		}
	}
	v.mu.RLock()
	failure := v.failure
	v.mu.RUnlock()
	if failure != nil {
		return failure
	}
	return v.service.ValidateSession(ctx, sessionID)
}

func (v *runtimeStreamAuthValidator) SetFailure(err error) {
	v.mu.Lock()
	v.failure = err
	v.mu.Unlock()
}

type runtimeStreamAuthValidationEvent struct {
	sequence int
	phase    string
	err      error
}

type runtimeStreamAuthSlowValidator struct {
	service         *auth.Service
	events          chan runtimeStreamAuthValidationEvent
	releasePeriodic chan struct{}
	releaseResult   chan struct{}
	mu              sync.Mutex
	sequence        int
}

func (v *runtimeStreamAuthSlowValidator) ValidateSession(ctx context.Context, sessionID string) error {
	v.mu.Lock()
	v.sequence++
	sequence := v.sequence
	v.mu.Unlock()
	select {
	case v.events <- runtimeStreamAuthValidationEvent{sequence: sequence, phase: "entry"}:
	case <-ctx.Done():
		return ctx.Err()
	}
	if sequence == 3 {
		select {
		case <-v.releasePeriodic:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	err := v.service.ValidateSession(ctx, sessionID)
	select {
	case v.events <- runtimeStreamAuthValidationEvent{sequence: sequence, phase: "result", err: err}:
	case <-ctx.Done():
		return ctx.Err()
	}
	if sequence == 3 {
		select {
		case <-v.releaseResult:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}

type runtimeStreamAuthSubscriber interface {
	SubscribeHostSamples(string) runtimefacts.HostSampleSubscription
}

func newRuntimeStreamAuthServer(t *testing.T, fixture *runtimeStreamAuthFixture, validator interface {
	ValidateSession(context.Context, string) error
}) *httptest.Server {
	return newRuntimeStreamAuthServerWithHub(t, fixture, validator, fixture.hub)
}

func newRuntimeStreamAuthServerWithHub(t *testing.T, fixture *runtimeStreamAuthFixture, validator interface {
	ValidateSession(context.Context, string) error
}, hub runtimeStreamAuthSubscriber) *httptest.Server {
	return newRuntimeStreamAuthServerWithHubDone(t, fixture, validator, hub, nil)
}

func newRuntimeStreamAuthServerWithHubDone(t *testing.T, fixture *runtimeStreamAuthFixture, validator interface {
	ValidateSession(context.Context, string) error
}, hub runtimeStreamAuthSubscriber, streamDone chan struct{}) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(runtimeStreamAuthMux(t, fixture, validator, hub, streamDone))
}

func runtimeStreamAuthMux(t *testing.T, fixture *runtimeStreamAuthFixture, validator interface {
	ValidateSession(context.Context, string) error
}, hub runtimeStreamAuthSubscriber, streamDone chan struct{}) http.Handler {
	t.Helper()
	runtimeHandler := handlers.MonitoringInstanceRuntimeStream(fixture.monitoring, hub, validator)
	streamHandler := runtimeHandler
	if streamDone != nil {
		var doneOnce sync.Once
		streamHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer doneOnce.Do(func() { close(streamDone) })
			runtimeHandler.ServeHTTP(w, r)
		})
	}
	authMiddleware := centerhttp.RequireSession(fixture.service, fixture.scope)
	router := centerhttp.New(centerhttp.RouterOptions{
		Version:                                "test",
		AuthLoginHandler:                       handlers.Login(fixture.service),
		AuthLogoutHandler:                      handlers.Logout(fixture.service),
		AuthMeHandler:                          handlers.Me(fixture.service, handlers.RuntimeCapabilities{}),
		AuthChangePasswordHandler:              handlers.ChangePassword(fixture.service),
		AuthMiddleware:                         authMiddleware,
		MonitoringInstanceRuntimeStreamHandler: streamHandler,
	})
	mux := http.NewServeMux()
	mux.Handle(runtimeStreamAuthSessionProbe, authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	mux.Handle("/", router)
	return mux
}

// The gate is a test-only transport barrier: it proves the handler reached a
// real WebSocket transport write and lets the watcher close that transport.
// It is not a production backpressure hook or a kernel socket-buffer claim.
type runtimeStreamAuthWriteGateListener struct {
	net.Listener
	accepted chan *runtimeStreamAuthWriteGateConn
}

func (l *runtimeStreamAuthWriteGateListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	gated := &runtimeStreamAuthWriteGateConn{
		Conn:         conn,
		writeEntered: make(chan struct{}),
		released:     make(chan struct{}),
	}
	l.accepted <- gated
	return gated, nil
}

type runtimeStreamAuthWriteGateConn struct {
	net.Conn
	mu           sync.Mutex
	armed        bool
	blocked      bool
	writeEntered chan struct{}
	released     chan struct{}
	enterOnce    sync.Once
	closeOnce    sync.Once
	closeErr     error
}

func (c *runtimeStreamAuthWriteGateConn) Arm() {
	c.mu.Lock()
	c.armed = true
	c.mu.Unlock()
}

func (c *runtimeStreamAuthWriteGateConn) Write(payload []byte) (int, error) {
	c.mu.Lock()
	block := c.armed && !c.blocked
	if block {
		c.blocked = true
	}
	c.mu.Unlock()
	if block {
		c.enterOnce.Do(func() { close(c.writeEntered) })
		<-c.released
	}
	return c.Conn.Write(payload)
}

func (c *runtimeStreamAuthWriteGateConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.released)
		c.closeErr = c.Conn.Close()
	})
	return c.closeErr
}

type runtimeStreamAuthGatedServer struct {
	URL    string
	client *http.Client
	server *http.Server
}

func (s *runtimeStreamAuthGatedServer) Close() error {
	return s.server.Close()
}

func newRuntimeStreamAuthGatedServer(t *testing.T, fixture *runtimeStreamAuthFixture, validator interface {
	ValidateSession(context.Context, string) error
}, hub runtimeStreamAuthSubscriber, streamDone chan struct{}) (*runtimeStreamAuthGatedServer, *runtimeStreamAuthWriteGateListener) {
	t.Helper()
	rawListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for gated runtime stream: %v", err)
	}
	listener := &runtimeStreamAuthWriteGateListener{
		Listener: rawListener,
		accepted: make(chan *runtimeStreamAuthWriteGateConn, 4),
	}
	server := &http.Server{Handler: runtimeStreamAuthMux(t, fixture, validator, hub, streamDone)}
	go func() {
		_ = server.Serve(listener)
	}()
	return &runtimeStreamAuthGatedServer{
		URL:    "http://" + listener.Addr().String(),
		client: &http.Client{},
		server: server,
	}, listener
}

func runtimeStreamAuthHTTPClient(server *httptest.Server) *http.Client {
	client := *server.Client()
	return &client
}

func runtimeStreamAuthHTTPClientWithJar(server *httptest.Server, jar http.CookieJar) *http.Client {
	client := *server.Client()
	client.Jar = jar
	return &client
}

func runtimeStreamAuthLogin(t *testing.T, client *http.Client, serverURL, password string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"username": runtimeStreamAuthUsername, "password": password})
	if err != nil {
		t.Fatalf("marshal login: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, serverURL+"/api/auth/login", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("new login request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("login request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	for _, cookie := range resp.Cookies() {
		if cookie.Name == auth.SessionCookieName && cookie.Value != "" {
			return cookie.Value
		}
	}
	t.Fatal("login response did not set session cookie")
	return ""
}

func runtimeStreamAuthProtectedStatus(t *testing.T, client *http.Client, serverURL string) int {
	t.Helper()
	resp, err := client.Get(serverURL + runtimeStreamAuthSessionProbe)
	if err != nil {
		t.Fatalf("protected request: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func runtimeStreamAuthChangePassword(t *testing.T, client *http.Client, serverURL string) int {
	t.Helper()
	body := strings.NewReader(`{"old_password":"` + runtimeStreamAuthPassword + `","new_password":"` + runtimeStreamAuthNewPassword + `"}`)
	req, err := http.NewRequest(http.MethodPut, serverURL+"/api/auth/password", body)
	if err != nil {
		t.Fatalf("new change password request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("change password request: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func runtimeStreamAuthLogout(t *testing.T, client *http.Client, serverURL string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, serverURL+"/api/auth/logout", nil)
	if err != nil {
		t.Fatalf("new logout request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("logout request: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func dialRuntimeStreamAuth(t *testing.T, server *httptest.Server, sessionID string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	return dialRuntimeStreamAuthURL(t, server.URL, runtimeStreamAuthHTTPClient(server), sessionID)
}

func dialRuntimeStreamAuthURL(t *testing.T, serverURL string, client *http.Client, sessionID string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	origin := serverURL
	scheme := "ws"
	if strings.HasPrefix(serverURL, "https://") {
		scheme = "wss"
		serverURL = strings.TrimPrefix(serverURL, "https")
	} else {
		serverURL = strings.TrimPrefix(serverURL, "http")
	}
	wsURL := scheme + serverURL + "/api/monitoring-instances/" + runtimeStreamAuthMonitoringID + "/runtime-stream"
	return websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPClient: client,
		HTTPHeader: http.Header{
			"Origin": []string{origin},
			"Cookie": []string{auth.SessionCookieName + "=" + sessionID},
		},
	})
}

func waitRuntimeStreamAuthSubscription(t *testing.T, fixture *runtimeStreamAuthFixture, ctx context.Context) {
	t.Helper()
	_ = waitRuntimeStreamAuthTrackedSubscription(t, fixture, ctx)
}

func waitRuntimeStreamAuthTrackedSubscription(t *testing.T, fixture *runtimeStreamAuthFixture, ctx context.Context) runtimefacts.HostSampleSubscription {
	t.Helper()
	select {
	case got := <-fixture.hub.subscribed:
		if got != runtimeStreamAuthMonitoringID {
			t.Fatalf("subscription monitoring instance = %q, want %q", got, runtimeStreamAuthMonitoringID)
		}
	case <-ctx.Done():
		t.Fatalf("waiting for runtime stream subscription: %v", ctx.Err())
	}
	select {
	case event := <-fixture.hub.subscriptionEvents:
		if event.monitoringInstanceID != runtimeStreamAuthMonitoringID {
			t.Fatalf("tracked subscription monitoring instance = %q, want %q", event.monitoringInstanceID, runtimeStreamAuthMonitoringID)
		}
		return event.subscription
	case <-ctx.Done():
		t.Fatalf("waiting for tracked runtime stream subscription: %v", ctx.Err())
	}
	return runtimefacts.HostSampleSubscription{}
}

func waitRuntimeStreamAuthValidation(t *testing.T, calls <-chan struct{}, ctx context.Context, count int) {
	t.Helper()
	for range count {
		select {
		case <-calls:
		case <-ctx.Done():
			t.Fatalf("waiting for %d session validation calls: %v", count, ctx.Err())
		}
	}
}
func waitRuntimeStreamAuthValidationEvent(t *testing.T, events <-chan runtimeStreamAuthValidationEvent, ctx context.Context, wantSequence int, wantPhase string) runtimeStreamAuthValidationEvent {
	t.Helper()
	select {
	case event := <-events:
		if event.sequence != wantSequence || event.phase != wantPhase {
			t.Fatalf("runtime stream validation event = %#v, want sequence=%d phase=%q", event, wantSequence, wantPhase)
		}
		return event
	case <-ctx.Done():
		t.Fatalf("waiting for runtime stream validation event sequence %d phase %q: %v", wantSequence, wantPhase, ctx.Err())
	}
	return runtimeStreamAuthValidationEvent{}
}

func assertNoRuntimeStreamAuthValidationEvents(t *testing.T, events <-chan runtimeStreamAuthValidationEvent, duration time.Duration) {
	t.Helper()
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case event := <-events:
		t.Fatalf("runtime stream validated after handler completion: sequence=%d", event.sequence)
	case <-timer.C:
	}
}

func drainRuntimeStreamAuthValidations(calls <-chan struct{}) {
	for {
		select {
		case <-calls:
		default:
			return
		}
	}
}

func publishRuntimeStreamAuthSample(t *testing.T, fixture *runtimeStreamAuthFixture, ctx context.Context, batchID string) {
	t.Helper()
	receivedAt := time.Now().UTC().Add(-time.Minute)
	if err := fixture.hub.AfterSuccessfulSync(ctx, syncing.Batch{
		MonitoringInstanceID: runtimeStreamAuthMonitoringID,
		Observations: observations.BatchWrite{HostSamples: []observations.HostSampleWrite{{
			MonitoringInstanceID: runtimeStreamAuthMonitoringID,
			ObservedAt:           receivedAt,
			ReceivedAt:           receivedAt,
			AgentVersion:         "agent/runtime-auth",
			Fingerprint:          runtimeStreamAuthFingerprint,
			SyncBatchID:          batchID,
			CPUUsagePct:          42,
		}}},
	}, syncing.Result{Disposition: syncing.ResultDispositionRecorded, AcceptedAt: receivedAt}); err != nil {
		t.Fatalf("publish runtime stream sample: %v", err)
	}
}

func readRuntimeStreamAuthMessage(t *testing.T, conn *websocket.Conn, timeout time.Duration) runtimefacts.HostSampleStreamMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var message runtimefacts.HostSampleStreamMessage
	if err := wsjson.Read(ctx, conn, &message); err != nil {
		t.Fatalf("read runtime stream sample: %v", err)
	}
	return message
}
func waitRuntimeStreamAuthWriteGate(t *testing.T, listener *runtimeStreamAuthWriteGateListener, ctx context.Context) *runtimeStreamAuthWriteGateConn {
	t.Helper()
	select {
	case conn := <-listener.accepted:
		return conn
	case <-ctx.Done():
		t.Fatalf("waiting for gated runtime stream connection: %v", ctx.Err())
	}
	return nil
}

func waitRuntimeStreamAuthClosedSince(t *testing.T, conn *websocket.Conn, started time.Time, timeout time.Duration) {
	t.Helper()
	remaining := time.Until(started.Add(timeout))
	if remaining <= 0 {
		t.Fatalf("runtime stream close deadline of %s elapsed before wait", timeout)
	}
	result := make(chan error, 1)
	go func() {
		var message runtimefacts.HostSampleStreamMessage
		result <- wsjson.Read(context.Background(), conn, &message)
	}()
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("runtime stream delivered a message after authority failure")
		}
	case <-timer.C:
		_ = conn.Close(websocket.StatusNormalClosure, "test timeout")
		t.Fatalf("runtime stream did not close within %s of authority invalidation", timeout)
	}
}

func waitRuntimeStreamAuthClosed(t *testing.T, conn *websocket.Conn, timeout time.Duration) {
	t.Helper()
	result := make(chan error, 1)
	go func() {
		var message runtimefacts.HostSampleStreamMessage
		result <- wsjson.Read(context.Background(), conn, &message)
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("runtime stream delivered a message after authority failure")
		}
	case <-timer.C:
		_ = conn.Close(websocket.StatusNormalClosure, "test timeout")
		t.Fatalf("runtime stream did not close within %s", timeout)
	}
}

func assertNoRuntimeStreamAuthValidation(t *testing.T, calls <-chan struct{}, duration time.Duration) {
	t.Helper()
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-calls:
		t.Fatal("runtime stream validated a client after it disconnected")
	case <-timer.C:
	}
}

func runtimeStreamAuthSessionHash(sessionID string) string {
	mac := hmac.New(sha256.New, []byte(runtimeStreamAuthHMACKey))
	_, _ = mac.Write([]byte(sessionID))
	return hex.EncodeToString(mac.Sum(nil))
}

type runtimeStreamAuthSessionRow struct {
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

func readRuntimeStreamAuthSessionRow(ctx context.Context, pool *pgxpool.Pool, sessionID string) (runtimeStreamAuthSessionRow, bool, error) {
	var row runtimeStreamAuthSessionRow
	err := pool.QueryRow(ctx, `select last_seen_at, expires_at from sessions where session_id_hash = $1`, runtimeStreamAuthSessionHash(sessionID)).Scan(&row.LastSeenAt, &row.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return runtimeStreamAuthSessionRow{}, false, nil
	}
	if err != nil {
		return runtimeStreamAuthSessionRow{}, false, err
	}
	return row, true, nil
}

func TestPostgresIntegrationRuntimeStreamSessionRevocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	fixture := newRuntimeStreamAuthFixture(t)
	validator := &runtimeStreamAuthValidator{service: fixture.service, calls: make(chan struct{}, 128)}
	server := newRuntimeStreamAuthServer(t, fixture, validator)
	defer server.Close()

	jarA, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar A: %v", err)
	}
	clientA := runtimeStreamAuthHTTPClientWithJar(server, jarA)
	jarB, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar B: %v", err)
	}
	clientB := runtimeStreamAuthHTTPClientWithJar(server, jarB)

	cookieA := runtimeStreamAuthLogin(t, clientA, server.URL, runtimeStreamAuthPassword)
	cookieB := runtimeStreamAuthLogin(t, clientB, server.URL, runtimeStreamAuthPassword)
	connA, response, err := dialRuntimeStreamAuth(t, server, cookieA)
	if err != nil {
		t.Fatalf("dial A: %v", err)
	}
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if connA == nil {
		t.Fatal("A websocket connection is nil")
	}
	defer connA.Close(websocket.StatusNormalClosure, "")
	waitRuntimeStreamAuthSubscription(t, fixture, ctx)
	connB, response, err := dialRuntimeStreamAuth(t, server, cookieB)
	if err != nil {
		t.Fatalf("dial B: %v", err)
	}
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if connB == nil {
		t.Fatal("B websocket connection is nil")
	}
	defer connB.Close(websocket.StatusNormalClosure, "")
	waitRuntimeStreamAuthSubscription(t, fixture, ctx)
	waitRuntimeStreamAuthValidation(t, validator.calls, ctx, 2)

	beforeB, present, err := readRuntimeStreamAuthSessionRow(ctx, fixture.pool, cookieB)
	if err != nil || !present {
		t.Fatalf("read B session before passive checks: present=%v err=%v", present, err)
	}
	publishRuntimeStreamAuthSample(t, fixture, ctx, "runtime-auth-initial")
	if message := readRuntimeStreamAuthMessage(t, connA, 3*time.Second); message.Sample.SyncBatchID != "runtime-auth-initial" {
		t.Fatalf("A initial sample = %q, want runtime-auth-initial", message.Sample.SyncBatchID)
	}
	if message := readRuntimeStreamAuthMessage(t, connB, 3*time.Second); message.Sample.SyncBatchID != "runtime-auth-initial" {
		t.Fatalf("B initial sample = %q, want runtime-auth-initial", message.Sample.SyncBatchID)
	}
	drainRuntimeStreamAuthValidations(validator.calls)
	waitRuntimeStreamAuthValidation(t, validator.calls, ctx, 2)
	afterB, present, err := readRuntimeStreamAuthSessionRow(ctx, fixture.pool, cookieB)
	if err != nil || !present {
		t.Fatalf("read B session after passive checks: present=%v err=%v", present, err)
	}
	if !afterB.LastSeenAt.Equal(beforeB.LastSeenAt) || !afterB.ExpiresAt.Equal(beforeB.ExpiresAt) {
		t.Fatalf("passive stream validation renewed B: before=%+v after=%+v", beforeB, afterB)
	}

	if status := runtimeStreamAuthChangePassword(t, clientA, server.URL); status != http.StatusNoContent {
		t.Fatalf("A change password status = %d, want 204", status)
	}
	if status := runtimeStreamAuthProtectedStatus(t, clientB, server.URL); status != http.StatusUnauthorized {
		t.Fatalf("B protected request after password change = %d, want 401", status)
	}
	if _, present, err := readRuntimeStreamAuthSessionRow(ctx, fixture.pool, cookieB); err != nil || present {
		t.Fatalf("B session after password change: present=%v err=%v, want revoked", present, err)
	}
	drainRuntimeStreamAuthValidations(validator.calls)
	publishRuntimeStreamAuthSample(t, fixture, ctx, "runtime-auth-revoked")
	if message := readRuntimeStreamAuthMessage(t, connA, 3*time.Second); message.Sample.SyncBatchID != "runtime-auth-revoked" {
		t.Fatalf("A sample after B revocation = %q, want runtime-auth-revoked", message.Sample.SyncBatchID)
	}
	waitRuntimeStreamAuthClosed(t, connB, 3*time.Second)
	if err := connA.Close(websocket.StatusNormalClosure, "test complete"); err != nil {
		t.Fatalf("close A websocket: %v", err)
	}

	jarC, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar C: %v", err)
	}
	clientC := runtimeStreamAuthHTTPClientWithJar(server, jarC)
	cookieC := runtimeStreamAuthLogin(t, clientC, server.URL, runtimeStreamAuthNewPassword)
	connC, response, err := dialRuntimeStreamAuth(t, server, cookieC)
	if err != nil {
		t.Fatalf("dial C: %v", err)
	}
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if connC == nil {
		t.Fatal("C websocket connection is nil")
	}
	defer connC.Close(websocket.StatusNormalClosure, "")
	waitRuntimeStreamAuthSubscription(t, fixture, ctx)
	waitRuntimeStreamAuthValidation(t, validator.calls, ctx, 1)
	if status := runtimeStreamAuthLogout(t, clientC, server.URL); status != http.StatusNoContent {
		t.Fatalf("C logout status = %d, want 204", status)
	}
	if status := runtimeStreamAuthProtectedStatus(t, clientC, server.URL); status != http.StatusUnauthorized {
		t.Fatalf("C protected request after logout = %d, want 401", status)
	}
	if _, present, err := readRuntimeStreamAuthSessionRow(ctx, fixture.pool, cookieC); err != nil || present {
		t.Fatalf("C session after logout: present=%v err=%v, want revoked", present, err)
	}
	drainRuntimeStreamAuthValidations(validator.calls)
	publishRuntimeStreamAuthSample(t, fixture, ctx, "runtime-auth-logout")
	waitRuntimeStreamAuthClosed(t, connC, 3*time.Second)

	jarD, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar D: %v", err)
	}
	clientD := runtimeStreamAuthHTTPClientWithJar(server, jarD)
	cookieD := runtimeStreamAuthLogin(t, clientD, server.URL, runtimeStreamAuthNewPassword)
	connD, response, err := dialRuntimeStreamAuth(t, server, cookieD)
	if err != nil {
		t.Fatalf("dial D: %v", err)
	}
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if connD == nil {
		t.Fatal("D websocket connection is nil")
	}
	defer connD.Close(websocket.StatusNormalClosure, "")
	waitRuntimeStreamAuthSubscription(t, fixture, ctx)
	waitRuntimeStreamAuthValidation(t, validator.calls, ctx, 1)
	beforeD, present, err := readRuntimeStreamAuthSessionRow(ctx, fixture.pool, cookieD)
	if err != nil || !present {
		t.Fatalf("read D session before renewal: present=%v err=%v", present, err)
	}
	fixture.clock.Advance(30 * time.Minute)
	if status := runtimeStreamAuthProtectedStatus(t, clientD, server.URL); status != http.StatusNoContent {
		t.Fatalf("D renewal protected status = %d, want 204", status)
	}
	afterD, present, err := readRuntimeStreamAuthSessionRow(ctx, fixture.pool, cookieD)
	if err != nil || !present {
		t.Fatalf("read D session after renewal: present=%v err=%v", present, err)
	}
	if !afterD.ExpiresAt.After(beforeD.ExpiresAt) || !afterD.LastSeenAt.After(beforeD.LastSeenAt) {
		t.Fatalf("D HTTP Touch did not renew session: before=%+v after=%+v", beforeD, afterD)
	}
	drainRuntimeStreamAuthValidations(validator.calls)
	fixture.clock.Set(beforeD.ExpiresAt.Add(time.Minute))
	waitRuntimeStreamAuthValidation(t, validator.calls, ctx, 1)
	publishRuntimeStreamAuthSample(t, fixture, ctx, "runtime-auth-renewed")
	if message := readRuntimeStreamAuthMessage(t, connD, 3*time.Second); message.Sample.SyncBatchID != "runtime-auth-renewed" {
		t.Fatalf("D sample after original expiry = %q, want runtime-auth-renewed", message.Sample.SyncBatchID)
	}
	if err := connD.Close(websocket.StatusNormalClosure, "test complete"); err != nil {
		t.Fatalf("close D websocket: %v", err)
	}

	jarE, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar E: %v", err)
	}
	clientE := runtimeStreamAuthHTTPClientWithJar(server, jarE)
	cookieE := runtimeStreamAuthLogin(t, clientE, server.URL, runtimeStreamAuthNewPassword)
	connE, response, err := dialRuntimeStreamAuth(t, server, cookieE)
	if err != nil {
		t.Fatalf("dial E: %v", err)
	}
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if connE == nil {
		t.Fatal("E websocket connection is nil")
	}
	defer connE.Close(websocket.StatusNormalClosure, "")
	waitRuntimeStreamAuthSubscription(t, fixture, ctx)
	waitRuntimeStreamAuthValidation(t, validator.calls, ctx, 1)
	sessionE, present, err := readRuntimeStreamAuthSessionRow(ctx, fixture.pool, cookieE)
	if err != nil || !present {
		t.Fatalf("read E session before expiry: present=%v err=%v", present, err)
	}
	drainRuntimeStreamAuthValidations(validator.calls)
	expiryTriggeredAt := time.Now()
	fixture.clock.Set(sessionE.ExpiresAt.Add(time.Microsecond))
	waitRuntimeStreamAuthValidation(t, validator.calls, ctx, 1)
	waitRuntimeStreamAuthClosedSince(t, connE, expiryTriggeredAt, 10*time.Second)
	if status := runtimeStreamAuthProtectedStatus(t, clientE, server.URL); status != http.StatusUnauthorized {
		t.Fatalf("E protected request after natural expiry = %d, want 401", status)
	}

	validator.SetFailure(errors.New("runtime stream authority database unavailable"))
	jarF, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar F: %v", err)
	}
	clientF := runtimeStreamAuthHTTPClientWithJar(server, jarF)
	cookieF := runtimeStreamAuthLogin(t, clientF, server.URL, runtimeStreamAuthNewPassword)
	connF, response, err := dialRuntimeStreamAuth(t, server, cookieF)
	if connF != nil {
		_ = connF.Close(websocket.StatusNormalClosure, "unexpected success")
		t.Fatal("validator failure handshake unexpectedly succeeded")
	}
	if response == nil || response.StatusCode != http.StatusInternalServerError {
		if response == nil {
			t.Fatalf("validator failure handshake response = nil, err=%v", err)
		}
		t.Fatalf("validator failure handshake status = %d, want 500", response.StatusCode)
	}
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	drainRuntimeStreamAuthValidations(validator.calls)
	validator.SetFailure(nil)
	connF, response, err = dialRuntimeStreamAuth(t, server, cookieF)
	if err != nil {
		t.Fatalf("dial F after validator recovery: %v", err)
	}
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if connF == nil {
		t.Fatal("F websocket connection is nil after validator recovery")
	}
	defer connF.Close(websocket.StatusNormalClosure, "")
	waitRuntimeStreamAuthSubscription(t, fixture, ctx)
	waitRuntimeStreamAuthValidation(t, validator.calls, ctx, 1)
	validator.SetFailure(errors.New("runtime stream authority query failed"))
	drainRuntimeStreamAuthValidations(validator.calls)
	publishRuntimeStreamAuthSample(t, fixture, ctx, "runtime-auth-validator-failure")
	waitRuntimeStreamAuthClosed(t, connF, 3*time.Second)
	validator.SetFailure(nil)

	slowJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar slow-write: %v", err)
	}
	slowClient := runtimeStreamAuthHTTPClientWithJar(server, slowJar)
	slowCookie := runtimeStreamAuthLogin(t, slowClient, server.URL, runtimeStreamAuthNewPassword)
	slowEvents := make(chan runtimeStreamAuthValidationEvent, 16)
	slowValidator := &runtimeStreamAuthSlowValidator{
		service:         fixture.service,
		events:          slowEvents,
		releasePeriodic: make(chan struct{}),
		releaseResult:   make(chan struct{}),
	}
	slowDone := make(chan struct{})
	slowServer, gateListener := newRuntimeStreamAuthGatedServer(t, fixture, slowValidator, fixture.hub, slowDone)
	defer slowServer.Close()
	connSlow, response, err := dialRuntimeStreamAuthURL(t, slowServer.URL, slowServer.client, slowCookie)
	if err != nil {
		t.Fatalf("dial slow-write stream: %v", err)
	}
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if connSlow == nil {
		t.Fatal("slow-write websocket connection is nil")
	}
	defer connSlow.Close(websocket.StatusNormalClosure, "")
	gateConn := waitRuntimeStreamAuthWriteGate(t, gateListener, ctx)
	slowSubscription := waitRuntimeStreamAuthTrackedSubscription(t, fixture, ctx)
	waitRuntimeStreamAuthValidationEvent(t, slowEvents, ctx, 1, "entry")
	waitRuntimeStreamAuthValidationEvent(t, slowEvents, ctx, 1, "result")
	gateConn.Arm()
	timer := time.NewTimer(2 * time.Second)
	select {
	case <-timer.C:
	case <-ctx.Done():
		timer.Stop()
		t.Fatalf("waiting to align slow-write stream before watcher tick: %v", ctx.Err())
	}
	publishRuntimeStreamAuthSample(t, fixture, ctx, "runtime-auth-slow-write")
	waitRuntimeStreamAuthValidationEvent(t, slowEvents, ctx, 2, "entry")
	waitRuntimeStreamAuthValidationEvent(t, slowEvents, ctx, 2, "result")
	select {
	case <-gateConn.writeEntered:
	case <-ctx.Done():
		t.Fatalf("waiting for blocked runtime stream write: %v", ctx.Err())
	}
	waitRuntimeStreamAuthValidationEvent(t, slowEvents, ctx, 3, "entry")
	select {
	case <-gateConn.released:
		t.Fatal("runtime stream write released before periodic authority result")
	default:
	}
	if status := runtimeStreamAuthLogout(t, slowClient, server.URL); status != http.StatusNoContent {
		t.Fatalf("slow-write session logout status = %d, want 204", status)
	}
	if _, present, err := readRuntimeStreamAuthSessionRow(ctx, fixture.pool, slowCookie); err != nil || present {
		t.Fatalf("slow-write session after logout: present=%v err=%v, want revoked", present, err)
	}
	select {
	case <-gateConn.released:
		t.Fatal("runtime stream write released before periodic authority result after revoke")
	default:
	}
	close(slowValidator.releasePeriodic)
	periodicResult := waitRuntimeStreamAuthValidationEvent(t, slowEvents, ctx, 3, "result")
	if !errors.Is(periodicResult.err, auth.ErrSessionNotFound) {
		t.Fatalf("slow-write periodic validation error = %v, want %v", periodicResult.err, auth.ErrSessionNotFound)
	}
	select {
	case <-gateConn.released:
		t.Fatal("runtime stream write released before periodic authority result was observed")
	default:
	}
	close(slowValidator.releaseResult)
	select {
	case <-gateConn.released:
	case <-ctx.Done():
		t.Fatalf("waiting for forced transport close after periodic authority rejection: %v", ctx.Err())
	}
	select {
	case <-slowDone:
	case <-ctx.Done():
		t.Fatalf("waiting for slow-write handler completion: %v", ctx.Err())
	}
	publishRuntimeStreamAuthSample(t, fixture, ctx, "runtime-auth-slow-write-after-cleanup")
	select {
	case message := <-slowSubscription.Messages:
		t.Fatalf("removed slow-write subscription received %#v", message)
	default:
	}
	assertNoRuntimeStreamAuthValidationEvents(t, slowEvents, 5*time.Second+500*time.Millisecond)

	gDone := make(chan struct{})
	gServer := newRuntimeStreamAuthServerWithHubDone(t, fixture, validator, fixture.hub, gDone)
	defer gServer.Close()
	connG, response, err := dialRuntimeStreamAuth(t, gServer, cookieF)
	if err != nil {
		t.Fatalf("dial G: %v", err)
	}
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if connG == nil {
		t.Fatal("G websocket connection is nil")
	}
	gSubscription := waitRuntimeStreamAuthTrackedSubscription(t, fixture, ctx)
	waitRuntimeStreamAuthValidation(t, validator.calls, ctx, 1)
	drainRuntimeStreamAuthValidations(validator.calls)
	if err := connG.Close(websocket.StatusNormalClosure, "client disconnect"); err != nil {
		t.Fatalf("close G websocket: %v", err)
	}
	select {
	case <-gDone:
	case <-ctx.Done():
		t.Fatalf("waiting for disconnected runtime stream handler completion: %v", ctx.Err())
	}
	publishRuntimeStreamAuthSample(t, fixture, ctx, "runtime-auth-client-disconnect")
	select {
	case message := <-gSubscription.Messages:
		t.Fatalf("removed disconnected subscription received %#v", message)
	default:
	}
	assertNoRuntimeStreamAuthValidation(t, validator.calls, 5*time.Second+500*time.Millisecond)

	closedServer := newRuntimeStreamAuthServerWithHub(t, fixture, validator, runtimeStreamAuthClosedHub{})
	defer closedServer.Close()
	connH, response, err := dialRuntimeStreamAuth(t, closedServer, cookieF)
	if err != nil {
		t.Fatalf("dial H: %v", err)
	}
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if connH == nil {
		t.Fatal("H websocket connection is nil")
	}
	defer connH.Close(websocket.StatusNormalClosure, "")
	waitRuntimeStreamAuthValidation(t, validator.calls, ctx, 1)
	waitRuntimeStreamAuthClosed(t, connH, 3*time.Second)
}
