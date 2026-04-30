package http

import (
	"context"
	stdhttp "net/http"

	"houfeng/internal/center/auth"
	"houfeng/internal/center/http/handlers"
)

type ctxKey int

const (
	ctxKeyUserID ctxKey = iota
)

// UserIDFromContext returns the authenticated user ID stored by RequireSession.
func UserIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxKeyUserID).(string)
	return v, ok
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
			ctx := context.WithValue(r.Context(), ctxKeyUserID, u.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeUnauthorized(w stdhttp.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(stdhttp.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthenticated"}`))
}
