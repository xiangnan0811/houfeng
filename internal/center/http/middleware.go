package http

import (
	"context"
	stdhttp "net/http"
	"net/url"
	"strings"

	"houfeng/internal/center/auth"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/http/sessionctx"
)

// UserIDFromContext returns the authenticated user ID stored by RequireSession.
func UserIDFromContext(ctx context.Context) (string, bool) {
	return sessionctx.UserIDFromContext(ctx)
}

// RequireSession returns middleware that requires a valid session cookie. On
// success it stamps the user_id into request context and forwards to next.
// On any failure it returns 401 with a JSON error body.
func RequireSession(svc handlers.AuthService) func(stdhttp.Handler) stdhttp.Handler {
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
			ctx := sessionctx.WithUserID(r.Context(), u.UserID)
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
			header.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; object-src 'none'; base-uri 'self'")
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
