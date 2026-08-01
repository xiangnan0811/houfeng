package http

import (
	"context"
	_ "embed"
	stdhttp "net/http"
	"net/url"
	"strings"

	"houfeng/internal/center/auth"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/recordauth"
)

//go:embed csp-policy.txt
var contentSecurityPolicySource string

var contentSecurityPolicy = strings.TrimSpace(contentSecurityPolicySource)

// UserIDFromContext returns the authenticated user ID stored by RequireSession.
func UserIDFromContext(ctx context.Context) (string, bool) {
	return sessionctx.UserIDFromContext(ctx)
}

// ActorScopeFromContext returns the typed trusted actor stamped by
// RequireSession. Existing handlers can keep using UserIDFromContext while
// record callers migrate to the typed authorization boundary.
func ActorScopeFromContext(ctx context.Context) (recordauth.ActorScope, bool) {
	return sessionctx.ActorScopeFromContext(ctx)
}

// RequireSession returns middleware that requires a valid session cookie. On
// success it hydrates a typed actor only from server-side identity and
// persistent group membership, then also preserves the legacy user_id context
// value. Invalid authentication returns 401; authorization infrastructure
// failure returns a fixed opaque 503.
func RequireSession(svc handlers.AuthService, scopes recordauth.ScopeRepository) func(stdhttp.Handler) stdhttp.Handler {
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			id := auth.ReadSessionCookie(r)
			if id == "" {
				writeUnauthorized(w)
				return
			}
			u, err := svc.UserBySession(r.Context(), id)
			if err != nil {
				writeUnauthorized(w)
				return
			}
			if u.UserID == "" || u.Role != auth.RoleAdmin {
				writeUnauthorized(w)
				return
			}
			if scopes == nil {
				writeAuthorizationUnavailable(w)
				return
			}
			groupIDs, err := scopes.ListActorGroupIDs(r.Context(), recordauth.ProjectIDDefault, u.UserID)
			if err != nil {
				writeAuthorizationUnavailable(w)
				return
			}
			actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
				UserID:    u.UserID,
				Role:      recordauth.RoleProjectAdmin,
				ProjectID: recordauth.ProjectIDDefault,
				GroupIDs:  groupIDs,
			})
			if err != nil {
				writeAuthorizationUnavailable(w)
				return
			}
			ctx := sessionctx.WithActorScope(sessionctx.WithUserID(r.Context(), u.UserID), actor)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func NoAuthForTestOnly() func(stdhttp.Handler) stdhttp.Handler {
	return func(next stdhttp.Handler) stdhttp.Handler {
		return next
	}
}

func RequireSameOrigin(publicBaseURL string) func(stdhttp.Handler) stdhttp.Handler {
	configuredOrigin := originFromRawURL(publicBaseURL)
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if isSafeMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			allowed := sameOrigin(r, configuredOrigin)
			if !allowed {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(stdhttp.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"forbidden"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func RequireAllowedHost(publicBaseURL string) func(stdhttp.Handler) stdhttp.Handler {
	allowedHost := hostFromRawURL(publicBaseURL)
	return func(next stdhttp.Handler) stdhttp.Handler {
		if allowedHost == "" {
			return next
		}
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if !strings.EqualFold(strings.TrimSpace(r.Host), allowedHost) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(stdhttp.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid host"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func SecurityHeaders(enableHSTS bool) func(stdhttp.Handler) stdhttp.Handler {
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			header := w.Header()
			header.Set("X-Content-Type-Options", "nosniff")
			header.Set("X-Frame-Options", "DENY")
			header.Set("Referrer-Policy", "no-referrer")
			header.Set("Content-Security-Policy", contentSecurityPolicy)
			header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
			if enableHSTS {
				header.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isSafeMethod(method string) bool {
	switch method {
	case stdhttp.MethodGet, stdhttp.MethodHead, stdhttp.MethodOptions:
		return true
	default:
		return false
	}
}

func sameOrigin(r *stdhttp.Request, configuredOrigin string) bool {
	requestOrigin := originFromRequest(r)
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		return originMatches(origin, requestOrigin, configuredOrigin)
	}
	if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
		return originMatches(referer, requestOrigin, configuredOrigin)
	}
	return false
}

func originMatches(raw, requestOrigin, configuredOrigin string) bool {
	origin := originFromRawURL(raw)
	if origin == "" {
		return false
	}
	return origin == requestOrigin || (configuredOrigin != "" && origin == configuredOrigin)
}

func originFromRequest(r *stdhttp.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.URL.Scheme, "https") {
		scheme = "https"
	}
	if r.URL.Scheme == "http" || r.URL.Scheme == "https" {
		scheme = r.URL.Scheme
	}
	host := r.Host
	if host == "" {
		host = r.URL.Host
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + strings.ToLower(host)
}

func originFromRawURL(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if u.Scheme == "" || u.Host == "" {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	return scheme + "://" + strings.ToLower(u.Host)
}

func hostFromRawURL(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Host)
}

func writeUnauthorized(w stdhttp.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(stdhttp.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthenticated"}`))
}

func writeAuthorizationUnavailable(w stdhttp.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(stdhttp.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`{"error":"authorization unavailable"}`))
}
