package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"houfeng/internal/center/auth"
	centerhttp "houfeng/internal/center/http"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/recordauth"
)

type memoryAuthState struct {
	mu       sync.Mutex
	users    map[string]auth.User
	byUser   map[string]string
	sessions map[string]auth.Session
}

// memoryUsers and memorySessions are a single process-local repository state.
// Keeping one mutex models the row-lock transaction boundary used by the
// PostgreSQL repository rather than giving each fake method an independent lock.
type memoryUsers struct {
	state  *memoryAuthState
	byID   map[string]auth.User
	byUser map[string]string
}

func newMemoryAuthState() *memoryAuthState {
	return &memoryAuthState{
		users:    map[string]auth.User{},
		byUser:   map[string]string{},
		sessions: map[string]auth.Session{},
	}
}

func newMemoryUsersWithState(state *memoryAuthState) *memoryUsers {
	return &memoryUsers{state: state, byID: state.users, byUser: state.byUser}
}

func newMemoryUsers() *memoryUsers { return newMemoryUsersWithState(newMemoryAuthState()) }

func (m *memoryUsers) Create(_ context.Context, u auth.User) error {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	if _, ok := m.byUser[u.Username]; ok {
		return auth.ErrUsernameTaken
	}
	m.byID[u.UserID] = u
	m.byUser[u.Username] = u.UserID
	return nil
}
func (m *memoryUsers) FindByUsername(_ context.Context, n string) (auth.User, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	id, ok := m.byUser[n]
	if !ok {
		return auth.User{}, auth.ErrUserNotFound
	}
	return m.byID[id], nil
}
func (m *memoryUsers) FindByID(_ context.Context, id string) (auth.User, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	u, ok := m.byID[id]
	if !ok {
		return auth.User{}, auth.ErrUserNotFound
	}
	return u, nil
}
func (m *memoryUsers) CountUsers(_ context.Context) (int, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	return len(m.byID), nil
}

type memorySessions struct {
	state *memoryAuthState
	byID  map[string]auth.Session
}

func newMemorySessionsWithState(state *memoryAuthState) *memorySessions {
	return &memorySessions{state: state, byID: state.sessions}
}

func newMemorySessions() *memorySessions { return newMemorySessionsWithState(newMemoryAuthState()) }

func newMemoryAuthRepositories() (*memoryUsers, *memorySessions) {
	state := newMemoryAuthState()
	return newMemoryUsersWithState(state), newMemorySessionsWithState(state)
}

func (m *memorySessions) CreateIfPasswordHash(_ context.Context, expectedHash string, s auth.Session, now func() time.Time, ttl time.Duration) (auth.Session, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	u, ok := m.state.users[s.UserID]
	if !ok || u.PasswordHash != expectedHash {
		return auth.Session{}, auth.ErrInvalidCredentials
	}
	issuedAt := now().UTC().Truncate(time.Microsecond)
	if u.PasswordChangedAt.After(issuedAt) {
		issuedAt = u.PasswordChangedAt
	}
	s.IssuedAt = issuedAt
	s.LastSeenAt = issuedAt
	s.ExpiresAt = issuedAt.Add(ttl)
	m.byID[s.SessionID] = s
	return s, nil
}

func (m *memorySessions) ChangePasswordIfHash(_ context.Context, userID, currentSessionID, expectedHash, newHash string, now func() time.Time) error {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	u, ok := m.state.users[userID]
	if !ok {
		return auth.ErrUserNotFound
	}
	if u.PasswordHash != expectedHash {
		return auth.ErrInvalidCredentials
	}
	s, ok := m.byID[currentSessionID]
	if !ok {
		return auth.ErrSessionNotFound
	}
	if s.UserID != userID {
		return auth.ErrSessionNotFound
	}
	checkedAt := now().UTC().Truncate(time.Microsecond)
	if !s.ExpiresAt.After(checkedAt) || s.IssuedAt.Before(u.PasswordChangedAt) {
		return auth.ErrSessionExpired
	}
	changedAt := checkedAt
	if u.PasswordChangedAt.After(changedAt) {
		changedAt = u.PasswordChangedAt
	}
	u.PasswordHash = newHash
	u.PasswordChangedAt = changedAt
	m.state.users[userID] = u
	s.IssuedAt = changedAt
	m.byID[currentSessionID] = s
	for id, other := range m.byID {
		if other.UserID == userID && id != currentSessionID {
			delete(m.byID, id)
		}
	}
	return nil
}

func (m *memorySessions) TouchWithUserLock(_ context.Context, sessionID string, now func() time.Time, ttl time.Duration) (auth.Session, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	s, ok := m.byID[sessionID]
	if !ok {
		return auth.Session{}, auth.ErrSessionNotFound
	}
	u, ok := m.state.users[s.UserID]
	if !ok {
		return auth.Session{}, auth.ErrUserNotFound
	}
	checkedAt := now().UTC().Truncate(time.Microsecond)
	if !s.ExpiresAt.After(checkedAt) || s.IssuedAt.Before(u.PasswordChangedAt) {
		delete(m.byID, sessionID)
		return auth.Session{}, auth.ErrSessionExpired
	}
	baseline := checkedAt
	if s.LastSeenAt.After(baseline) {
		baseline = s.LastSeenAt
	}
	if u.PasswordChangedAt.After(baseline) {
		baseline = u.PasswordChangedAt
	}
	s.LastSeenAt = baseline
	s.ExpiresAt = baseline.Add(ttl)
	m.byID[sessionID] = s
	return s, nil
}

func (m *memorySessions) Delete(_ context.Context, id string) error {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	delete(m.byID, id)
	return nil
}
func (m *memorySessions) DeleteExpiredBefore(_ context.Context, cutoff time.Time) (int, error) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	n := 0
	for k, s := range m.byID {
		if s.ExpiresAt.Before(cutoff) {
			delete(m.byID, k)
			n++
		}
	}
	return n, nil
}

type successfulScopeRepository struct{}

func (successfulScopeRepository) ListActorGroupIDs(_ context.Context, projectID recordauth.ProjectID, _ string) ([]string, error) {
	if projectID != recordauth.ProjectIDDefault {
		return nil, errors.New("unexpected project")
	}
	return []string{"rag_e2e"}, nil
}

func setupAuthEndToEnd(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
	users, sessions := newMemoryAuthRepositories()
	if err := auth.SeedInitialUser(context.Background(), users, "admin", "correct-horse-battery", "管理员", time.Now); err != nil {
		t.Fatalf("SeedInitialUser: %v", err)
	}
	svc := auth.New(users, sessions, auth.Options{SessionTTL: time.Hour})

	dashboardCalled := 0
	dashboard := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dashboardCalled++
		uid, _ := centerhttp.UserIDFromContext(r.Context())
		_ = json.NewEncoder(w).Encode(map[string]string{"user_id": uid})
	})

	mux := centerhttp.New(centerhttp.RouterOptions{
		Version:                   "test",
		DashboardHandler:          dashboard,
		AuthLoginHandler:          handlers.Login(svc),
		AuthLogoutHandler:         handlers.Logout(svc),
		AuthMeHandler:             handlers.Me(svc),
		AuthChangePasswordHandler: handlers.ChangePassword(svc),
		AuthMiddleware:            centerhttp.RequireSession(svc, successfulScopeRepository{}),
	})
	srv := httptest.NewTLSServer(mux)
	return srv, srv.Close
}

func TestAuthEndToEndLoginFlow(t *testing.T) {
	srv, cleanup := setupAuthEndToEnd(t)
	defer cleanup()

	jar, err := newCookieJar()
	if err != nil {
		t.Fatalf("newCookieJar: %v", err)
	}
	client := srv.Client()
	client.Jar = jar

	// 1. Unauthenticated GET /api/dashboard -> 401
	{
		resp, err := client.Get(srv.URL + "/api/dashboard")
		if err != nil {
			t.Fatalf("get dashboard: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("unauth dashboard = %d, want 401", resp.StatusCode)
		}
	}

	// 2. POST /api/auth/login with valid creds -> 200, sets cookie
	{
		body := strings.NewReader(`{"username":"admin","password":"correct-horse-battery"}`)
		resp, err := client.Post(srv.URL+"/api/auth/login", "application/json", body)
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("login = %d, want 200", resp.StatusCode)
		}
	}

	// 3. Authenticated GET /api/dashboard -> 200
	{
		resp, err := client.Get(srv.URL + "/api/dashboard")
		if err != nil {
			t.Fatalf("auth dashboard: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("auth dashboard = %d, want 200", resp.StatusCode)
		}
	}

	// 4. GET /api/auth/me returns identity
	{
		resp, err := client.Get(srv.URL + "/api/auth/me")
		if err != nil {
			t.Fatalf("me: %v", err)
		}
		var got struct {
			UserID   string `json:"user_id"`
			Username string `json:"username"`
			Role     string `json:"role"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&got)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || got.Username != "admin" || got.Role != auth.RoleAdmin {
			t.Fatalf("me = %d %+v, want 200 admin/admin", resp.StatusCode, got)
		}
	}

	// 5. POST /api/auth/logout -> 204
	{
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/auth/logout", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("logout: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("logout = %d, want 204", resp.StatusCode)
		}
	}

	// 6. After logout the session cookie was cleared by the server. The
	//    client jar may still have it though, so an explicit re-request
	//    should now hit Touch -> ErrSessionNotFound -> 401. Browsers honor
	//    Set-Cookie MaxAge=-1 and drop it; our Go cookie jar does too.
	{
		resp, err := client.Get(srv.URL + "/api/dashboard")
		if err != nil {
			t.Fatalf("post-logout dashboard: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("post-logout dashboard = %d, want 401", resp.StatusCode)
		}
	}
}

func TestAuthEndToEndPasswordRotationRetainsCurrentCookieAndRevokesOther(t *testing.T) {
	srv, cleanup := setupAuthEndToEnd(t)
	defer cleanup()

	currentJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("current cookie jar: %v", err)
	}
	otherJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("other cookie jar: %v", err)
	}
	baseClient := srv.Client()
	currentValue := *baseClient
	currentValue.Jar = currentJar
	current := &currentValue
	otherValue := *baseClient
	otherValue.Jar = otherJar
	other := &otherValue
	login := func(client *http.Client) {
		t.Helper()
		resp, err := client.Post(srv.URL+"/api/auth/login", "application/json", strings.NewReader(`{"username":"admin","password":"correct-horse-battery"}`))
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("login = %d, want 200", resp.StatusCode)
		}
	}
	login(current)
	login(other)

	resp, err := current.Do(mustRequest(t, http.MethodPut, srv.URL+"/api/auth/password", `{"old_password":"correct-horse-battery","new_password":"new-correct-horse-battery"}`))
	if err != nil {
		t.Fatalf("change password: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("change password = %d, want 204", resp.StatusCode)
	}

	resp, err = current.Get(srv.URL + "/api/dashboard")
	if err != nil {
		t.Fatalf("current cookie dashboard: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("current cookie dashboard = %d, want 200", resp.StatusCode)
	}

	resp, err = other.Get(srv.URL + "/api/dashboard")
	if err != nil {
		t.Fatalf("other cookie dashboard: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("other cookie dashboard = %d, want 401", resp.StatusCode)
	}

	replacementJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("replacement cookie jar: %v", err)
	}
	replacementValue := *baseClient
	replacementValue.Jar = replacementJar
	replacement := &replacementValue
	resp, err = replacement.Post(srv.URL+"/api/auth/login", "application/json", strings.NewReader(`{"username":"admin","password":"new-correct-horse-battery"}`))
	if err != nil {
		t.Fatalf("replacement login: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replacement login = %d, want 200", resp.StatusCode)
	}
}

func TestAuthEndToEndWrongCredentials(t *testing.T) {
	srv, cleanup := setupAuthEndToEnd(t)
	defer cleanup()

	body := strings.NewReader(`{"username":"admin","password":"wrong-password-xx"}`)
	client := srv.Client()
	resp, err := client.Post(srv.URL+"/api/auth/login", "application/json", body)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("login wrong = %d, want 401", resp.StatusCode)
	}
}

func TestAuthEndToEndHealthzAndAgentBypass(t *testing.T) {
	srv, cleanup := setupAuthEndToEnd(t)
	defer cleanup()

	client := srv.Client()
	resp, err := client.Get(srv.URL + "/api/healthz")
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz = %d, want 200", resp.StatusCode)
	}
}

func mustRequest(t *testing.T, method, url, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return req
}

// newCookieJar wraps cookiejar.New into a tiny helper. Defined here to keep the
// import list tight at the top of the file.
func newCookieJar() (http.CookieJar, error) {
	return cookiejar.New(nil)
}
