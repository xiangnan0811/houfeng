package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"houfeng/internal/center/auth"
)

type stubAuthSvc struct {
	user auth.User
	err  error
}

func (s *stubAuthSvc) Login(_ context.Context, _, _, _, _ string) (auth.Session, error) {
	return auth.Session{}, nil
}
func (s *stubAuthSvc) Logout(_ context.Context, _ string) error { return nil }
func (s *stubAuthSvc) Touch(_ context.Context, _ string) (auth.Session, error) {
	return auth.Session{}, s.err
}
func (s *stubAuthSvc) UserBySession(_ context.Context, _ string) (auth.User, error) {
	return s.user, s.err
}
func (s *stubAuthSvc) ChangePassword(_ context.Context, _, _, _, _ string) error { return nil }

func TestRequireSessionAllowsAuthenticated(t *testing.T) {
	svc := &stubAuthSvc{user: auth.User{UserID: "u1"}}
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		uid, ok := UserIDFromContext(r.Context())
		if !ok || uid != "u1" {
			t.Fatalf("ctx user_id = %q ok=%v, want u1", uid, ok)
		}
		w.WriteHeader(http.StatusOK)
	})
	mw := RequireSession(svc)(inner)

	r := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "abc", Expires: time.Now().Add(time.Hour)})
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, r)

	if !called {
		t.Fatal("inner handler must be called")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestRequireSessionRejectsMissingCookie(t *testing.T) {
	svc := &stubAuthSvc{err: auth.ErrSessionNotFound}
	mw := RequireSession(svc)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner must not be called")
	}))
	r := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestRequireSessionRejectsExpired(t *testing.T) {
	svc := &stubAuthSvc{err: auth.ErrSessionExpired}
	mw := RequireSession(svc)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner must not be called")
	}))
	r := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "expired"})
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestUserIDFromContextEmpty(t *testing.T) {
	uid, ok := UserIDFromContext(context.Background())
	if ok || uid != "" {
		t.Fatalf("UserIDFromContext = %q ok=%v, want empty/false", uid, ok)
	}
}

func TestRequireSameOriginRejectsCrossSiteUnsafeMethod(t *testing.T) {
	mw := RequireSameOrigin("https://houfeng.example.com")
	innerCalled := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		innerCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))

	r := httptest.NewRequest(http.MethodPost, "https://houfeng.example.com/api/settings", nil)
	r.Header.Set("Origin", "https://evil.example.net")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if innerCalled {
		t.Fatal("inner handler was called for cross-site unsafe method")
	}
}

func TestRequireSameOriginAllowsSameOriginUnsafeMethod(t *testing.T) {
	mw := RequireSameOrigin("https://houfeng.example.com")
	innerCalled := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		innerCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))

	r := httptest.NewRequest(http.MethodPut, "https://houfeng.example.com/api/settings", nil)
	r.Header.Set("Origin", "https://houfeng.example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if !innerCalled {
		t.Fatal("inner handler was not called for same-origin unsafe method")
	}
}

func TestRequireSameOriginRejectsUnsafeMethodWithoutOriginOrReferer(t *testing.T) {
	mw := RequireSameOrigin("https://houfeng.example.com")
	handler := mw(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner handler must not be called")
	}))

	r := httptest.NewRequest(http.MethodDelete, "https://houfeng.example.com/api/settings", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

func TestRequireAllowedHostAllowsEmptyPublicBaseURL(t *testing.T) {
	innerCalled := false
	handler := RequireAllowedHost("")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		innerCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))

	r := httptest.NewRequest(http.MethodGet, "http://dev.local/api/dashboard", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if !innerCalled {
		t.Fatal("inner handler was not called")
	}
}

func TestRequireAllowedHostAllowsConfiguredHostWithPort(t *testing.T) {
	innerCalled := false
	handler := RequireAllowedHost("https://houfeng.example.com:8443")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		innerCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))

	r := httptest.NewRequest(http.MethodGet, "http://houfeng.example.com:8443/api/dashboard", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if !innerCalled {
		t.Fatal("inner handler was not called")
	}
}

func TestRequireAllowedHostRejectsMismatchedHost(t *testing.T) {
	innerCalled := false
	handler := RequireAllowedHost("https://houfeng.example.com")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		innerCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))

	r := httptest.NewRequest(http.MethodGet, "http://evil.example.net/api/dashboard", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if innerCalled {
		t.Fatal("inner handler was called for mismatched host")
	}
}

func TestSecurityHeadersSetsBaselineHeaders(t *testing.T) {
	handler := SecurityHeaders(true)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	r := httptest.NewRequest(http.MethodGet, "https://houfeng.example.com/api/dashboard", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	headers := w.Result().Header
	if headers.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", headers.Get("X-Content-Type-Options"))
	}
	if headers.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want DENY", headers.Get("X-Frame-Options"))
	}
	if headers.Get("Strict-Transport-Security") == "" {
		t.Fatal("Strict-Transport-Security missing")
	}
	const wantCSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; object-src 'none'; base-uri 'self'; form-action 'self'"
	if got := headers.Get("Content-Security-Policy"); got != wantCSP {
		t.Fatalf("Content-Security-Policy = %q, want %q", got, wantCSP)
	}
}
