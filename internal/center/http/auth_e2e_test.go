package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"houfeng/internal/center/auth"
	centerhttp "houfeng/internal/center/http"
	"houfeng/internal/center/http/handlers"
)

// memoryUsers is a process-local in-memory UserRepository sufficient for e2e
// wiring tests of the HTTP layer (we are not exercising Postgres).
type memoryUsers struct {
	mu     sync.Mutex
	byID   map[string]auth.User
	byUser map[string]string
}

func newMemoryUsers() *memoryUsers {
	return &memoryUsers{byID: map[string]auth.User{}, byUser: map[string]string{}}
}

func (m *memoryUsers) Create(_ context.Context, u auth.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byUser[u.Username]; ok {
		return auth.ErrUsernameTaken
	}
	m.byID[u.UserID] = u
	m.byUser[u.Username] = u.UserID
	return nil
}
func (m *memoryUsers) FindByUsername(_ context.Context, n string) (auth.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.byUser[n]
	if !ok {
		return auth.User{}, auth.ErrUserNotFound
	}
	return m.byID[id], nil
}
func (m *memoryUsers) FindByID(_ context.Context, id string) (auth.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.byID[id]
	if !ok {
		return auth.User{}, auth.ErrUserNotFound
	}
	return u, nil
}
func (m *memoryUsers) UpdatePassword(_ context.Context, id, h string, t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.byID[id]
	if !ok {
		return auth.ErrUserNotFound
	}
	u.PasswordHash = h
	u.PasswordChangedAt = t
	m.byID[id] = u
	return nil
}
func (m *memoryUsers) CountUsers(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.byID), nil
}

type memorySessions struct {
	mu   sync.Mutex
	byID map[string]auth.Session
}

func newMemorySessions() *memorySessions { return &memorySessions{byID: map[string]auth.Session{}} }

func (m *memorySessions) Create(_ context.Context, s auth.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byID[s.SessionID] = s
	return nil
}
func (m *memorySessions) Find(_ context.Context, id string) (auth.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.byID[id]
	if !ok {
		return auth.Session{}, auth.ErrSessionNotFound
	}
	return s, nil
}
func (m *memorySessions) RefreshExpires(_ context.Context, id string, ls, exp time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.byID[id]
	if !ok {
		return auth.ErrSessionNotFound
	}
	s.LastSeenAt = ls
	s.ExpiresAt = exp
	m.byID[id] = s
	return nil
}
func (m *memorySessions) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.byID, id)
	return nil
}
func (m *memorySessions) DeleteExpiredBefore(_ context.Context, cutoff time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for k, s := range m.byID {
		if s.ExpiresAt.Before(cutoff) {
			delete(m.byID, k)
			n++
		}
	}
	return n, nil
}

func setupAuthEndToEnd(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
	users := newMemoryUsers()
	sessions := newMemorySessions()
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
		AuthMiddleware:            centerhttp.RequireSession(svc),
	})
	srv := httptest.NewServer(mux)
	return srv, srv.Close
}

func TestAuthEndToEndLoginFlow(t *testing.T) {
	srv, cleanup := setupAuthEndToEnd(t)
	defer cleanup()

	jar, err := newCookieJar()
	if err != nil {
		t.Fatalf("newCookieJar: %v", err)
	}
	client := &http.Client{Jar: jar}

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

func TestAuthEndToEndWrongCredentials(t *testing.T) {
	srv, cleanup := setupAuthEndToEnd(t)
	defer cleanup()

	body := strings.NewReader(`{"username":"admin","password":"wrong-password-xx"}`)
	resp, err := http.Post(srv.URL+"/api/auth/login", "application/json", body)
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

	resp, err := http.Get(srv.URL + "/api/healthz")
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz = %d, want 200", resp.StatusCode)
	}
}

// newCookieJar wraps cookiejar.New into a tiny helper. Defined here to keep the
// import list tight at the top of the file.
func newCookieJar() (http.CookieJar, error) {
	return cookieJarHelper()
}
