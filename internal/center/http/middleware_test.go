package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/auth"
	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/recordauth"
)

const middlewareTestUserID = "usr_0123456789abcdef01234567"

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

type stubScopeRepository struct {
	groupIDs  []string
	err       error
	calls     int
	projectID recordauth.ProjectID
	userID    string
}

func (s *stubScopeRepository) ListActorGroupIDs(_ context.Context, projectID recordauth.ProjectID, userID string) ([]string, error) {
	s.calls++
	s.projectID = projectID
	s.userID = userID
	if s.err != nil {
		return nil, s.err
	}
	return append([]string(nil), s.groupIDs...), nil
}

func TestRequireSessionBuildsTrustedTypedActorAndIgnoresForgedHeaders(t *testing.T) {
	svc := &stubAuthSvc{user: auth.User{UserID: middlewareTestUserID, Role: auth.RoleAdmin}}
	scopes := &stubScopeRepository{groupIDs: []string{"rag_beta", "rag_alpha", "rag_beta"}}
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		uid, ok := UserIDFromContext(r.Context())
		if !ok || uid != middlewareTestUserID {
			t.Fatalf("ctx user_id = %q ok=%v, want %q", uid, ok, middlewareTestUserID)
		}
		actor, ok := sessionctx.ActorScopeFromContext(r.Context())
		if !ok {
			t.Fatal("typed actor missing from context")
		}
		if actor.UserID != middlewareTestUserID || actor.Role != recordauth.RoleProjectAdmin || actor.ProjectID != recordauth.ProjectIDDefault {
			t.Fatalf("typed actor = %#v, want trusted default-project admin", actor)
		}
		if want := []string{"rag_alpha", "rag_beta"}; !sameStringSlices(actor.GroupIDs, want) {
			t.Fatalf("actor GroupIDs = %#v, want %#v", actor.GroupIDs, want)
		}
		w.WriteHeader(http.StatusOK)
	})
	mw := RequireSession(svc, scopes)(inner)

	r := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "abc", Expires: time.Now().Add(time.Hour)})
	r.Header.Set("X-Project-ID", "other")
	r.Header.Set("X-Role", "viewer")
	r.Header.Set("X-Group-ID", "rag_forged")
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, r)

	if !called {
		t.Fatal("inner handler must be called")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if scopes.calls != 1 || scopes.projectID != recordauth.ProjectIDDefault || scopes.userID != middlewareTestUserID {
		t.Fatalf("scope lookup = calls=%d project=%q user=%q, want one default/%q lookup", scopes.calls, scopes.projectID, scopes.userID, middlewareTestUserID)
	}
}

func TestRequireSessionRejectsMissingCookie(t *testing.T) {
	svc := &stubAuthSvc{err: auth.ErrSessionNotFound}
	scopes := &stubScopeRepository{}
	mw := RequireSession(svc, scopes)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner must not be called")
	}))
	r := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if scopes.calls != 0 {
		t.Fatalf("scope lookup calls = %d, want 0", scopes.calls)
	}
}

func TestRequireSessionRejectsExpired(t *testing.T) {
	svc := &stubAuthSvc{err: auth.ErrSessionExpired}
	mw := RequireSession(svc, &stubScopeRepository{})(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
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

func TestRequireSessionRejectsScopeInfrastructureFailureWithOpaque503(t *testing.T) {
	internalFailure := errors.New("database connection details must not leak")
	svc := &stubAuthSvc{user: auth.User{UserID: middlewareTestUserID, Role: auth.RoleAdmin}}
	mw := RequireSession(svc, &stubScopeRepository{err: internalFailure})(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner must not be called")
	}))

	r := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "abc"})
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	if got := w.Body.String(); got != `{"error":"authorization unavailable"}` || strings.Contains(got, internalFailure.Error()) {
		t.Fatalf("opaque 503 body = %q, want fixed non-leaking response", got)
	}
}

func TestRequireSessionRejectsMalformedPersistedScopeWithOpaque503(t *testing.T) {
	svc := &stubAuthSvc{user: auth.User{UserID: middlewareTestUserID, Role: auth.RoleAdmin}}
	mw := RequireSession(svc, &stubScopeRepository{groupIDs: []string{"RAG_not-canonical"}})(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner must not be called")
	}))

	r := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "abc"})
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestRequireSessionTreatsMalformedAuthenticatedUserIDAsOpaqueScopeFailure(t *testing.T) {
	const malformedUserID = "usr_not-hex"
	scopes := &stubScopeRepository{}
	innerCalled := false
	mw := RequireSession(&stubAuthSvc{user: auth.User{UserID: malformedUserID, Role: auth.RoleAdmin}}, scopes)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		innerCalled = true
	}))

	r := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "abc"})
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	if innerCalled {
		t.Fatal("inner handler must not be called")
	}
	if scopes.calls != 1 || scopes.projectID != recordauth.ProjectIDDefault || scopes.userID != malformedUserID {
		t.Fatalf("scope lookup = calls=%d project=%q user=%q, want one default/%q lookup", scopes.calls, scopes.projectID, scopes.userID, malformedUserID)
	}
	if got := w.Body.String(); got != `{"error":"authorization unavailable"}` || strings.Contains(got, malformedUserID) {
		t.Fatalf("opaque 503 body = %q, want fixed non-leaking response", got)
	}
}

func TestRequireSessionRejectsEmptyOrUnknownAuthenticatedIdentity(t *testing.T) {
	tests := []struct {
		name string
		user auth.User
	}{
		{name: "empty user id", user: auth.User{Role: auth.RoleAdmin}},
		{name: "unknown role", user: auth.User{UserID: middlewareTestUserID, Role: "viewer"}},
		{name: "empty role", user: auth.User{UserID: middlewareTestUserID}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scopes := &stubScopeRepository{}
			mw := RequireSession(&stubAuthSvc{user: tt.user}, scopes)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
				t.Fatal("inner must not be called")
			}))
			r := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
			r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "abc"})
			w := httptest.NewRecorder()
			mw.ServeHTTP(w, r)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", w.Code)
			}
			if scopes.calls != 0 {
				t.Fatalf("scope lookup calls = %d, want 0", scopes.calls)
			}
		})
	}
}

func TestUserIDFromContextEmpty(t *testing.T) {
	uid, ok := UserIDFromContext(context.Background())
	if ok || uid != "" {
		t.Fatalf("UserIDFromContext = %q ok=%v, want empty/false", uid, ok)
	}
}

func TestActorScopeFromContextEmpty(t *testing.T) {
	actor, ok := sessionctx.ActorScopeFromContext(context.Background())
	if ok || actor.UserID != "" || actor.Role != "" || actor.ProjectID != "" || len(actor.GroupIDs) != 0 {
		t.Fatalf("ActorScopeFromContext = %#v ok=%v, want zero/false", actor, ok)
	}
}

func sameStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
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
