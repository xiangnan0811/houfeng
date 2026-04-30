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
func (s *stubAuthSvc) ChangePassword(_ context.Context, _, _, _ string) error { return nil }

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
