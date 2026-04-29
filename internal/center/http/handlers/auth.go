package handlers

import (
	"context"
	"errors"
	"net/http"

	"houfeng/internal/center/auth"
)

// AuthService is the subset of auth.Service used by HTTP handlers (allows test stubs).
type AuthService interface {
	Login(ctx context.Context, username, password, userAgent, clientIP string) (auth.Session, error)
	Logout(ctx context.Context, sessionID string) error
	Touch(ctx context.Context, sessionID string) (auth.Session, error)
	UserBySession(ctx context.Context, sessionID string) (auth.User, error)
	ChangePassword(ctx context.Context, userID, oldPassword, newPassword string) error
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type meResponse struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	DisplayName string `json:"display_name"`
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// Login handles POST /api/auth/login.
func Login(svc AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req loginRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		sess, err := svc.Login(r.Context(), req.Username, req.Password, r.UserAgent(), clientIP(r))
		if err != nil {
			if errors.Is(err, auth.ErrInvalidCredentials) {
				writeError(w, http.StatusUnauthorized, "invalid username or password")
				return
			}
			writeError(w, http.StatusInternalServerError, "login failed")
			return
		}
		auth.SetSessionCookie(w, sess.SessionID, sess.ExpiresAt)
		writeJSON(w, http.StatusOK, meResponse{UserID: sess.UserID})
	}
}

// Logout handles POST /api/auth/logout.
func Logout(svc AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if id := auth.ReadSessionCookie(r); id != "" {
			_ = svc.Logout(r.Context(), id)
		}
		auth.ClearSessionCookie(w)
		w.WriteHeader(http.StatusNoContent)
	}
}

// Me handles GET /api/auth/me.
func Me(svc AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		id := auth.ReadSessionCookie(r)
		if id == "" {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return
		}
		u, err := svc.UserBySession(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return
		}
		writeJSON(w, http.StatusOK, meResponse{
			UserID:      u.UserID,
			Username:    u.Username,
			Role:        u.Role,
			DisplayName: u.DisplayName,
		})
	}
}

// ChangePassword handles PUT /api/auth/password.
func ChangePassword(svc AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		id := auth.ReadSessionCookie(r)
		if id == "" {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return
		}
		u, err := svc.UserBySession(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return
		}
		var req changePasswordRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := svc.ChangePassword(r.Context(), u.UserID, req.OldPassword, req.NewPassword); err != nil {
			switch {
			case errors.Is(err, auth.ErrInvalidCredentials):
				writeError(w, http.StatusUnauthorized, "old password incorrect")
			case errors.Is(err, auth.ErrPasswordTooShort), errors.Is(err, auth.ErrPasswordTooLong):
				writeError(w, http.StatusBadRequest, "new password invalid")
			default:
				writeError(w, http.StatusInternalServerError, "change password failed")
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	return r.RemoteAddr
}
