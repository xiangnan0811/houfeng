package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/auth"
)

type stubAuth struct {
	loginErr  error
	loginSess auth.Session
	logoutErr error
	touchErr  error
	touchUser auth.User
	chgErr    error
	gotUser   string
	gotPass   string
	gotIP     string
}

func (s *stubAuth) Login(_ context.Context, username, password, _, clientIP string) (auth.Session, error) {
	s.gotUser, s.gotPass = username, password
	s.gotIP = clientIP
	return s.loginSess, s.loginErr
}
func (s *stubAuth) Logout(_ context.Context, _ string) error { return s.logoutErr }
func (s *stubAuth) Touch(_ context.Context, _ string) (auth.Session, error) {
	return auth.Session{}, s.touchErr
}
func (s *stubAuth) UserBySession(_ context.Context, _ string) (auth.User, error) {
	return s.touchUser, s.touchErr
}
func (s *stubAuth) ChangePassword(_ context.Context, _, _, _, _ string) error { return s.chgErr }

// ---- Login ---------------------------------------------------------------

func TestLoginHandlerSuccess(t *testing.T) {
	svc := &stubAuth{
		loginSess: auth.Session{SessionID: "abc", UserID: "u1", ExpiresAt: time.Now().Add(time.Hour)},
	}
	h := Login(svc)
	body := strings.NewReader(`{"username":"admin","password":"correct-horse"}`)
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != auth.SessionCookieName || cookies[0].Value != "abc" {
		t.Fatalf("cookie = %+v, want %s=abc", cookies, auth.SessionCookieName)
	}
	var resp meResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.UserID != "u1" {
		t.Fatalf("user_id = %v, want u1", resp.UserID)
	}
	if svc.gotUser != "admin" || svc.gotPass != "correct-horse" {
		t.Fatalf("forwarded creds = %q/%q", svc.gotUser, svc.gotPass)
	}
}

func TestLoginHandlerInvalidCredentials(t *testing.T) {
	svc := &stubAuth{loginErr: auth.ErrInvalidCredentials}
	h := Login(svc)
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"x","password":"y"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("must not set cookie on failure")
	}
}

func TestLoginHandlerInternalFailure(t *testing.T) {
	svc := &stubAuth{loginErr: errors.New("find user by username: query user")}
	h := Login(svc)
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"correct-horse"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "invalid username or password") {
		t.Fatalf("body = %q, must not present system failure as invalid credentials", w.Body.String())
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("must not set cookie on failure")
	}
}

func TestLoginHandlerRejectNonPost(t *testing.T) {
	svc := &stubAuth{}
	h := Login(svc)
	r := httptest.NewRequest(http.MethodGet, "/api/auth/login", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", w.Code)
	}
}

func TestLoginHandlerRejectMalformedBody(t *testing.T) {
	svc := &stubAuth{}
	h := Login(svc)
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{not json`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestLoginHandlerUsesRemoteAddrWhenProxyIsUntrusted(t *testing.T) {
	svc := &stubAuth{
		loginSess: auth.Session{SessionID: "abc", UserID: "u1", ExpiresAt: time.Now().Add(time.Hour)},
	}
	h := Login(svc)
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"correct-horse"}`))
	r.RemoteAddr = "198.51.100.10:12345"
	r.Header.Set("X-Forwarded-For", "203.0.113.77")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if svc.gotIP != "198.51.100.10" {
		t.Fatalf("client IP = %q, want remote address host", svc.gotIP)
	}
}

func TestLoginHandlerUsesForwardedForFromTrustedProxy(t *testing.T) {
	svc := &stubAuth{
		loginSess: auth.Session{SessionID: "abc", UserID: "u1", ExpiresAt: time.Now().Add(time.Hour)},
	}
	h := LoginWithOptions(svc, LoginOptions{
		TrustedProxies: []string{"10.0.0.0/8"},
	})
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"correct-horse"}`))
	r.RemoteAddr = "10.1.2.3:12345"
	r.Header.Set("X-Forwarded-For", "203.0.113.77, 10.1.2.3")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if svc.gotIP != "203.0.113.77" {
		t.Fatalf("client IP = %q, want trusted forwarded client", svc.gotIP)
	}
}

func TestLoginHandlerRateLimitsFailedAttempts(t *testing.T) {
	svc := &stubAuth{loginErr: auth.ErrInvalidCredentials}
	h := LoginWithOptions(svc, LoginOptions{
		RateLimit: LoginRateLimitOptions{
			MaxFailuresByUsername: 2,
			Window:                time.Minute,
		},
	})

	for i := 0; i < 2; i++ {
		r := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"wrong"}`))
		r.RemoteAddr = "198.51.100.10:12345"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", i+1, w.Code)
		}
	}

	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"wrong"}`))
	r.RemoteAddr = "198.51.100.10:12345"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("limited attempt status = %d, want 429", w.Code)
	}
}

func TestLoginLimiterCapsUsernameKeys(t *testing.T) {
	now := time.Date(2026, time.June, 26, 8, 0, 0, 0, time.UTC)
	limiter := newLoginLimiter(LoginRateLimitOptions{
		MaxTrackedKeys: 3,
		Window:         time.Minute,
	}, func() time.Time { return now })

	for i := 0; i < 10; i++ {
		limiter.recordFailure("user-"+string(rune('a'+i)), "198.51.100.10")
	}

	if got := len(limiter.byUser); got > 3 {
		t.Fatalf("tracked username keys = %d, want <= 3", got)
	}
}

func TestLoginLimiterSweepsExpiredKeys(t *testing.T) {
	now := time.Date(2026, time.June, 26, 8, 0, 0, 0, time.UTC)
	limiter := newLoginLimiter(LoginRateLimitOptions{
		MaxTrackedKeys: 10,
		SweepInterval:  time.Minute,
		Window:         time.Minute,
	}, func() time.Time { return now })

	limiter.recordFailure("expired-user", "198.51.100.10")
	now = now.Add(2 * time.Minute)
	limiter.recordFailure("active-user", "198.51.100.11")

	if _, ok := limiter.byUser["expired-user"]; ok {
		t.Fatalf("expired username key was not swept: %#v", limiter.byUser)
	}
	if _, ok := limiter.byIP["198.51.100.10"]; ok {
		t.Fatalf("expired IP key was not swept: %#v", limiter.byIP)
	}
	if _, ok := limiter.byUser["active-user"]; !ok {
		t.Fatalf("active username key missing after sweep: %#v", limiter.byUser)
	}
}

// ---- Logout --------------------------------------------------------------

func TestLogoutHandlerClearsCookie(t *testing.T) {
	svc := &stubAuth{}
	h := Logout(svc)
	r := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "abc"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Value != "" || cookies[0].MaxAge != -1 {
		t.Fatalf("cookie = %+v, want cleared", cookies)
	}
}

func TestLogoutHandlerNoCookieStillNoContent(t *testing.T) {
	svc := &stubAuth{}
	h := Logout(svc)
	r := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
}

func TestLogoutHandlerRejectNonPost(t *testing.T) {
	svc := &stubAuth{}
	h := Logout(svc)
	r := httptest.NewRequest(http.MethodGet, "/api/auth/logout", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", w.Code)
	}
}

// ---- Me ------------------------------------------------------------------

func TestMeHandlerSuccess(t *testing.T) {
	svc := &stubAuth{
		touchUser: auth.User{UserID: "u1", Username: "admin", Role: auth.RoleAdmin, DisplayName: "管理员"},
	}
	h := Me(svc)
	r := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "abc"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp meResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.UserID != "u1" || resp.Username != "admin" || resp.Role != auth.RoleAdmin {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestMeHandlerUnauthenticatedNoCookie(t *testing.T) {
	svc := &stubAuth{}
	h := Me(svc)
	r := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestMeHandlerUnauthenticatedExpired(t *testing.T) {
	svc := &stubAuth{touchErr: auth.ErrSessionExpired}
	h := Me(svc)
	r := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "abc"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

// ---- ChangePassword -------------------------------------------------------

func TestChangePasswordSuccess(t *testing.T) {
	svc := &stubAuth{
		touchUser: auth.User{UserID: "u1", Username: "admin", Role: auth.RoleAdmin},
	}
	h := ChangePassword(svc)
	body := strings.NewReader(`{"old_password":"correct-horse-battery","new_password":"new-correct-horse-battery"}`)
	r := httptest.NewRequest(http.MethodPut, "/api/auth/password", body)
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "abc"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
}

func TestChangePasswordWrongOld(t *testing.T) {
	svc := &stubAuth{
		touchUser: auth.User{UserID: "u1", Username: "admin", Role: auth.RoleAdmin},
		chgErr:    auth.ErrInvalidCredentials,
	}
	h := ChangePassword(svc)
	body := strings.NewReader(`{"old_password":"wrong","new_password":"new-correct-horse-battery"}`)
	r := httptest.NewRequest(http.MethodPut, "/api/auth/password", body)
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "abc"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestChangePasswordTooShort(t *testing.T) {
	svc := &stubAuth{
		touchUser: auth.User{UserID: "u1", Username: "admin", Role: auth.RoleAdmin},
		chgErr:    auth.ErrPasswordTooShort,
	}
	h := ChangePassword(svc)
	body := strings.NewReader(`{"old_password":"correct-horse-battery","new_password":"abc"}`)
	r := httptest.NewRequest(http.MethodPut, "/api/auth/password", body)
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "abc"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestChangePasswordTooWeak(t *testing.T) {
	svc := &stubAuth{
		touchUser: auth.User{UserID: "u1", Username: "admin", Role: auth.RoleAdmin},
		chgErr:    auth.ErrPasswordTooWeak,
	}
	h := ChangePassword(svc)
	body := strings.NewReader(`{"old_password":"correct-horse-battery","new_password":"password123"}`)
	r := httptest.NewRequest(http.MethodPut, "/api/auth/password", body)
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "abc"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestChangePasswordUnauthenticated(t *testing.T) {
	svc := &stubAuth{}
	h := ChangePassword(svc)
	r := httptest.NewRequest(http.MethodPut, "/api/auth/password", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}
