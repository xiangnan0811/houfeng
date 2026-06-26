package handlers

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"houfeng/internal/center/auth"
)

// AuthService is the subset of auth.Service used by HTTP handlers (allows test stubs).
type AuthService interface {
	Login(ctx context.Context, username, password, userAgent, clientIP string) (auth.Session, error)
	Logout(ctx context.Context, sessionID string) error
	Touch(ctx context.Context, sessionID string) (auth.Session, error)
	UserBySession(ctx context.Context, sessionID string) (auth.User, error)
	ChangePassword(ctx context.Context, userID, currentSessionID, oldPassword, newPassword string) error
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

type LoginOptions struct {
	TrustedProxies []string
	RateLimit      LoginRateLimitOptions
	Now            func() time.Time
}

type LoginRateLimitOptions struct {
	MaxFailuresByUsername int
	MaxFailuresByIP       int
	MaxFailuresGlobal     int
	MaxTrackedKeys        int
	Window                time.Duration
	SweepInterval         time.Duration
}

type loginLimiter struct {
	mu            sync.Mutex
	now           func() time.Time
	window        time.Duration
	sweepInterval time.Duration
	nextSweep     time.Time
	maxUser       int
	maxIP         int
	maxGlobal     int
	maxKeys       int
	byUser        map[string][]time.Time
	byIP          map[string][]time.Time
	global        []time.Time
}

func newLoginLimiter(opts LoginRateLimitOptions, now func() time.Time) *loginLimiter {
	if now == nil {
		now = time.Now
	}
	if opts.Window <= 0 {
		opts.Window = 15 * time.Minute
	}
	if opts.MaxTrackedKeys <= 0 {
		opts.MaxTrackedKeys = 10000
	}
	if opts.SweepInterval <= 0 {
		opts.SweepInterval = opts.Window
	}
	return &loginLimiter{
		now:           now,
		window:        opts.Window,
		sweepInterval: opts.SweepInterval,
		maxUser:       opts.MaxFailuresByUsername,
		maxIP:         opts.MaxFailuresByIP,
		maxGlobal:     opts.MaxFailuresGlobal,
		maxKeys:       opts.MaxTrackedKeys,
		byUser:        make(map[string][]time.Time),
		byIP:          make(map[string][]time.Time),
	}
}

func defaultLoginRateLimitOptions() LoginRateLimitOptions {
	return LoginRateLimitOptions{
		MaxFailuresByUsername: 10,
		MaxFailuresByIP:       30,
		MaxFailuresGlobal:     300,
		MaxTrackedKeys:        10000,
		Window:                15 * time.Minute,
		SweepInterval:         time.Minute,
	}
}

func (l *loginLimiter) allow(username, clientIP string) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now().UTC()
	cutoff := now.Add(-l.window)
	l.sweepExpiredLocked(now, cutoff)
	userEvents := l.eventsForKey(l.byUser, username, cutoff)
	ipEvents := l.eventsForKey(l.byIP, clientIP, cutoff)
	l.global = pruneTimes(l.global, cutoff)
	if l.maxUser > 0 && len(userEvents) >= l.maxUser {
		return false
	}
	if l.maxIP > 0 && len(ipEvents) >= l.maxIP {
		return false
	}
	if l.maxGlobal > 0 && len(l.global) >= l.maxGlobal {
		return false
	}
	return true
}

func (l *loginLimiter) recordFailure(username, clientIP string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now().UTC()
	cutoff := now.Add(-l.window)
	l.sweepExpiredLocked(now, cutoff)
	if events, ok := l.trackableEventsForKey(l.byUser, username, cutoff); ok {
		l.byUser[username] = append(events, now)
	}
	if events, ok := l.trackableEventsForKey(l.byIP, clientIP, cutoff); ok {
		l.byIP[clientIP] = append(events, now)
	}
	l.global = append(pruneTimes(l.global, cutoff), now)
}

func (l *loginLimiter) recordSuccess(username, clientIP string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.byUser, username)
	delete(l.byIP, clientIP)
}

func (l *loginLimiter) eventsForKey(values map[string][]time.Time, key string, cutoff time.Time) []time.Time {
	events, ok := values[key]
	if !ok {
		return nil
	}
	events = pruneTimes(events, cutoff)
	if len(events) == 0 {
		delete(values, key)
		return nil
	}
	values[key] = events
	return events
}

func (l *loginLimiter) trackableEventsForKey(values map[string][]time.Time, key string, cutoff time.Time) ([]time.Time, bool) {
	events := l.eventsForKey(values, key, cutoff)
	if events != nil {
		return events, true
	}
	if l.maxKeys > 0 && len(values) >= l.maxKeys {
		return nil, false
	}
	return nil, true
}

func (l *loginLimiter) sweepExpiredLocked(now, cutoff time.Time) {
	if l.sweepInterval <= 0 {
		return
	}
	if !l.nextSweep.IsZero() && now.Before(l.nextSweep) {
		return
	}
	sweepTimeMap(l.byUser, cutoff)
	sweepTimeMap(l.byIP, cutoff)
	l.nextSweep = now.Add(l.sweepInterval)
}

func pruneTimes(values []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for ; i < len(values); i++ {
		if !values[i].Before(cutoff) {
			break
		}
	}
	if i == 0 {
		return values
	}
	return append(values[:0], values[i:]...)
}

func sweepTimeMap(values map[string][]time.Time, cutoff time.Time) {
	for key, events := range values {
		events = pruneTimes(events, cutoff)
		if len(events) == 0 {
			delete(values, key)
			continue
		}
		values[key] = events
	}
}

type trustedProxyResolver struct {
	trusted []*net.IPNet
}

func newTrustedProxyResolver(cidrs []string) trustedProxyResolver {
	resolver := trustedProxyResolver{}
	for _, raw := range cidrs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		_, network, err := net.ParseCIDR(raw)
		if err == nil {
			resolver.trusted = append(resolver.trusted, network)
		}
	}
	return resolver
}

// Login handles POST /api/auth/login.
func Login(svc AuthService) http.HandlerFunc {
	return LoginWithOptions(svc, LoginOptions{RateLimit: defaultLoginRateLimitOptions()})
}

func LoginWithOptions(svc AuthService, opts LoginOptions) http.HandlerFunc {
	resolver := newTrustedProxyResolver(opts.TrustedProxies)
	if opts.RateLimit == (LoginRateLimitOptions{}) {
		opts.RateLimit = defaultLoginRateLimitOptions()
	}
	limiter := newLoginLimiter(opts.RateLimit, opts.Now)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req loginRequest
		if err := decodeJSONLimited(w, r, &req, AuthJSONBodyLimit); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		ip := resolver.clientIP(r)
		username := strings.TrimSpace(req.Username)
		if !limiter.allow(username, ip) {
			writeError(w, http.StatusTooManyRequests, "too many login attempts")
			return
		}
		sess, err := svc.Login(r.Context(), req.Username, req.Password, r.UserAgent(), ip)
		if err != nil {
			limiter.recordFailure(username, ip)
			if errors.Is(err, auth.ErrInvalidCredentials) {
				writeError(w, http.StatusUnauthorized, "invalid username or password")
				return
			}
			writeError(w, http.StatusInternalServerError, "login failed")
			return
		}
		limiter.recordSuccess(username, ip)
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
		if err := decodeJSONLimited(w, r, &req, AuthJSONBodyLimit); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := svc.ChangePassword(r.Context(), u.UserID, id, req.OldPassword, req.NewPassword); err != nil {
			switch {
			case errors.Is(err, auth.ErrInvalidCredentials):
				writeError(w, http.StatusUnauthorized, "old password incorrect")
			case errors.Is(err, auth.ErrPasswordTooShort), errors.Is(err, auth.ErrPasswordTooLong), errors.Is(err, auth.ErrPasswordTooWeak):
				writeError(w, http.StatusBadRequest, "new password invalid")
			default:
				writeError(w, http.StatusInternalServerError, "change password failed")
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (resolver trustedProxyResolver) clientIP(r *http.Request) string {
	remoteHost := hostOnly(r.RemoteAddr)
	remoteIP := net.ParseIP(remoteHost)
	if remoteIP == nil {
		return remoteHost
	}
	if len(resolver.trusted) == 0 || !resolver.isTrusted(remoteIP) {
		return remoteHost
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return remoteHost
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(parts[i])
		ip := net.ParseIP(candidate)
		if ip == nil {
			continue
		}
		if !resolver.isTrusted(ip) {
			return ip.String()
		}
	}
	for _, part := range parts {
		candidate := strings.TrimSpace(part)
		if ip := net.ParseIP(candidate); ip != nil {
			return ip.String()
		}
	}
	return remoteHost
}

func hostOnly(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(addr)
}

func (resolver trustedProxyResolver) isTrusted(ip net.IP) bool {
	for _, network := range resolver.trusted {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
