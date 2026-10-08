package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type fakeAuthState struct {
	mu       sync.Mutex
	users    map[string]User
	byUser   map[string]string
	sessions map[string]Session
}

type fakeUsers struct {
	state             *fakeAuthState
	byID              map[string]User
	byUser            map[string]string
	findByUsernameErr error
}

func newFakeAuthState() *fakeAuthState {
	return &fakeAuthState{
		users:    map[string]User{},
		byUser:   map[string]string{},
		sessions: map[string]Session{},
	}
}

func newFakeUsersWithState(state *fakeAuthState) *fakeUsers {
	return &fakeUsers{state: state, byID: state.users, byUser: state.byUser}
}

func newFakeUsers() *fakeUsers { return newFakeUsersWithState(newFakeAuthState()) }

func (f *fakeUsers) Create(_ context.Context, u User) error {
	f.state.mu.Lock()
	defer f.state.mu.Unlock()
	if _, ok := f.byUser[u.Username]; ok {
		return ErrUsernameTaken
	}
	f.byID[u.UserID] = u
	f.byUser[u.Username] = u.UserID
	return nil
}
func (f *fakeUsers) FindByUsername(_ context.Context, n string) (User, error) {
	f.state.mu.Lock()
	defer f.state.mu.Unlock()
	if f.findByUsernameErr != nil {
		return User{}, f.findByUsernameErr
	}
	id, ok := f.byUser[n]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return f.byID[id], nil
}
func (f *fakeUsers) FindByID(_ context.Context, id string) (User, error) {
	f.state.mu.Lock()
	defer f.state.mu.Unlock()
	u, ok := f.byID[id]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return u, nil
}
func (f *fakeUsers) CountUsers(_ context.Context) (int, error) {
	f.state.mu.Lock()
	defer f.state.mu.Unlock()
	return len(f.byID), nil
}

type fakeSessions struct {
	state *fakeAuthState
	byID  map[string]Session
}

func newFakeSessionsWithState(state *fakeAuthState) *fakeSessions {
	return &fakeSessions{state: state, byID: state.sessions}
}

func newFakeSessions() *fakeSessions { return newFakeSessionsWithState(newFakeAuthState()) }

func newFakeRepositories() (*fakeUsers, *fakeSessions) {
	state := newFakeAuthState()
	return newFakeUsersWithState(state), newFakeSessionsWithState(state)
}

func fakeSessionTimes() (time.Time, time.Time) {
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	return now, now.Add(time.Hour)
}

func (f *fakeSessions) CreateIfPasswordHash(_ context.Context, expectedHash string, s Session, now func() time.Time, ttl time.Duration) (Session, error) {
	f.state.mu.Lock()
	defer f.state.mu.Unlock()
	u, ok := f.state.users[s.UserID]
	if !ok || u.PasswordHash != expectedHash || u.DisabledAt != nil {
		return Session{}, ErrInvalidCredentials
	}
	s.ManagementCapabilities = u.ManagementCapabilities()
	issuedAt := now().UTC().Truncate(time.Microsecond)
	if u.PasswordChangedAt.After(issuedAt) {
		issuedAt = u.PasswordChangedAt
	}
	s.IssuedAt = issuedAt
	s.LastSeenAt = issuedAt
	s.ExpiresAt = issuedAt.Add(ttl)
	f.byID[s.SessionID] = s
	return s, nil
}

func (f *fakeSessions) ChangePasswordIfHash(_ context.Context, userID, currentSessionID, expectedHash, newHash string, now func() time.Time) error {
	f.state.mu.Lock()
	defer f.state.mu.Unlock()
	u, ok := f.state.users[userID]
	if !ok {
		return ErrUserNotFound
	}
	if u.DisabledAt != nil {
		return ErrSessionNotFound
	}
	if u.PasswordHash != expectedHash {
		return ErrInvalidCredentials
	}
	s, ok := f.byID[currentSessionID]
	if !ok {
		return ErrSessionNotFound
	}
	if s.UserID != userID {
		return ErrSessionNotFound
	}
	checkedAt := now().UTC().Truncate(time.Microsecond)
	if !s.ExpiresAt.After(checkedAt) || s.IssuedAt.Before(u.PasswordChangedAt) {
		return ErrSessionExpired
	}
	changedAt := checkedAt
	if u.PasswordChangedAt.After(changedAt) {
		changedAt = u.PasswordChangedAt
	}
	u.PasswordHash = newHash
	u.PasswordChangedAt = changedAt
	f.state.users[userID] = u
	s.IssuedAt = changedAt
	f.byID[currentSessionID] = s
	for id, other := range f.byID {
		if other.UserID == userID && id != currentSessionID {
			delete(f.byID, id)
		}
	}
	return nil
}

func (f *fakeSessions) TouchWithUserLock(_ context.Context, sessionID string, now func() time.Time, ttl time.Duration) (Session, error) {
	f.state.mu.Lock()
	defer f.state.mu.Unlock()
	s, ok := f.byID[sessionID]
	if !ok {
		return Session{}, ErrSessionNotFound
	}
	u, ok := f.state.users[s.UserID]
	if !ok {
		return Session{}, ErrUserNotFound
	}
	if u.DisabledAt != nil {
		delete(f.byID, sessionID)
		return Session{}, ErrSessionExpired
	}
	checkedAt := now().UTC().Truncate(time.Microsecond)
	if !s.ExpiresAt.After(checkedAt) || s.IssuedAt.Before(u.PasswordChangedAt) {
		delete(f.byID, sessionID)
		return Session{}, ErrSessionExpired
	}
	baseline := checkedAt
	if s.LastSeenAt.After(baseline) {
		baseline = s.LastSeenAt
	}
	if u.PasswordChangedAt.After(baseline) {
		baseline = u.PasswordChangedAt
	}
	s.ManagementCapabilities = u.ManagementCapabilities()
	s.LastSeenAt = baseline
	s.ExpiresAt = baseline.Add(ttl)
	f.byID[sessionID] = s
	return s, nil
}

func (f *fakeSessions) ValidateSession(_ context.Context, sessionID string, now func() time.Time) error {
	f.state.mu.Lock()
	defer f.state.mu.Unlock()
	s, ok := f.byID[sessionID]
	if !ok {
		return ErrSessionNotFound
	}
	u, ok := f.state.users[s.UserID]
	if !ok {
		return ErrUserNotFound
	}
	if u.DisabledAt != nil {
		return ErrSessionExpired
	}
	checkedAt := now().UTC().Truncate(time.Microsecond)
	if !s.ExpiresAt.After(checkedAt) || (!s.IssuedAt.IsZero() && s.IssuedAt.Before(u.PasswordChangedAt)) {
		return ErrSessionExpired
	}
	return nil
}

func (f *fakeSessions) Delete(_ context.Context, id string) error {
	f.state.mu.Lock()
	defer f.state.mu.Unlock()
	delete(f.byID, id)
	return nil
}
func (f *fakeSessions) DeleteExpiredBefore(_ context.Context, cutoff time.Time) (int, error) {
	f.state.mu.Lock()
	defer f.state.mu.Unlock()
	n := 0
	for k, s := range f.byID {
		if s.ExpiresAt.Before(cutoff) {
			delete(f.byID, k)
			n++
		}
	}
	return n, nil
}

func newTestService(t *testing.T) (*Service, *fakeUsers, *fakeSessions) {
	t.Helper()
	users, sessions := newFakeRepositories()
	now, _ := fakeSessionTimes()
	svc := New(users, sessions, Options{
		SessionTTL: time.Hour,
		Now:        func() time.Time { return now },
	})
	return svc, users, sessions
}

func mustSeed(t *testing.T, users *fakeUsers, username, password string) User {
	t.Helper()
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	u := User{
		UserID:       "usr_" + username,
		Username:     username,
		PasswordHash: hash,
		Role:         RoleAdmin,
	}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return u
}

func addFakeSession(sessions *fakeSessions, id, userID string, issuedAt, expiresAt time.Time) {
	sessions.byID[id] = Session{SessionID: id, UserID: userID, IssuedAt: issuedAt, LastSeenAt: issuedAt, ExpiresAt: expiresAt}
}

func TestServiceLoginSuccess(t *testing.T) {
	svc, users, _ := newTestService(t)
	mustSeed(t, users, "admin", "correct-horse-battery")

	sess, err := svc.Login(context.Background(), "admin", "correct-horse-battery", "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if sess.UserID != "usr_admin" {
		t.Fatalf("UserID = %q, want usr_admin", sess.UserID)
	}
	if sess.SessionID == "" {
		t.Fatal("SessionID empty")
	}
	if sess.UserAgent != "ua" || sess.ClientIP != "1.2.3.4" {
		t.Fatalf("metadata not stored: ua=%q ip=%q", sess.UserAgent, sess.ClientIP)
	}
	if sess.IssuedAt.IsZero() || sess.ExpiresAt.IsZero() {
		t.Fatalf("repository did not populate timestamps: %+v", sess)
	}
}
func TestServiceLoginDisabledUserCannotCreateSessionAndExistingSessionIsRejected(t *testing.T) {
	svc, users, sessions := newTestService(t)
	user := mustSeed(t, users, "admin", "correct-horse-battery")
	user.IsSupervisor = true
	users.byID[user.UserID] = user

	session, err := svc.Login(context.Background(), "admin", "correct-horse-battery", "", "")
	if err != nil {
		t.Fatalf("initial Login: %v", err)
	}
	if !session.ManagementCapabilities.Access {
		t.Fatalf("management_capabilities = %+v, want access=true", session.ManagementCapabilities)
	}

	disabledAt := time.Date(2026, 4, 29, 12, 1, 0, 0, time.UTC)
	user.DisabledAt = &disabledAt
	users.byID[user.UserID] = user
	if _, err := svc.Login(context.Background(), "admin", "correct-horse-battery", "", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("disabled Login = %v, want ErrInvalidCredentials", err)
	}
	if _, err := svc.Touch(context.Background(), session.SessionID); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("disabled Touch = %v, want ErrSessionExpired", err)
	}
	if _, ok := sessions.byID[session.SessionID]; ok {
		t.Fatal("disabled session remained persisted")
	}
	if err := svc.ValidateSession(context.Background(), session.SessionID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("ValidateSession after disabled Touch = %v, want ErrSessionNotFound", err)
	}
}

func TestServiceLoginWrongPassword(t *testing.T) {
	svc, users, _ := newTestService(t)
	mustSeed(t, users, "admin", "right-credential-2026!")

	_, err := svc.Login(context.Background(), "admin", "wrong-credential-2026!", "", "")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login wrong = %v, want ErrInvalidCredentials", err)
	}
}

func TestServiceLoginUnknownUser(t *testing.T) {
	svc, _, _ := newTestService(t)
	_, err := svc.Login(context.Background(), "ghost", "any-password-xx", "", "")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login unknown = %v, want ErrInvalidCredentials", err)
	}
}

func TestServiceLoginPropagatesUserRepositoryError(t *testing.T) {
	svc, users, _ := newTestService(t)
	users.findByUsernameErr = errors.New("query user: scan password_changed_at")

	_, err := svc.Login(context.Background(), "admin", "correct-horse-battery", "", "")
	if err == nil {
		t.Fatal("Login = nil, want repository error")
	}
	if errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login error = %v, must not be ErrInvalidCredentials", err)
	}
	if !strings.Contains(err.Error(), "find user by username") {
		t.Fatalf("Login error = %v, want find user context", err)
	}
}

func TestServiceLoginEmptyUsername(t *testing.T) {
	svc, _, _ := newTestService(t)
	_, err := svc.Login(context.Background(), "", "any-password-xx", "", "")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login empty username = %v, want ErrInvalidCredentials", err)
	}
}

func TestServiceTouchExtendsExpiry(t *testing.T) {
	svc, users, _ := newTestService(t)
	mustSeed(t, users, "admin", "correct-horse-battery")

	sess, err := svc.Login(context.Background(), "admin", "correct-horse-battery", "", "")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	got, err := svc.Touch(context.Background(), sess.SessionID)
	if err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if !got.ExpiresAt.Equal(sess.ExpiresAt) {
		t.Fatalf("static clock changed expiry: got %s want %s", got.ExpiresAt, sess.ExpiresAt)
	}
	if got.SessionID != sess.SessionID {
		t.Fatalf("SessionID changed unexpectedly")
	}
}

func TestServiceValidateSessionIsReadOnlyAndUsesCurrentAuthority(t *testing.T) {
	users, sessions := newFakeRepositories()
	now, _ := fakeSessionTimes()
	clock := now
	svc := New(users, sessions, Options{SessionTTL: time.Hour, Now: func() time.Time { return clock }})
	mustSeed(t, users, "admin", "correct-horse-battery")

	session, err := svc.Login(context.Background(), "admin", "correct-horse-battery", "", "")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	before := sessions.byID[session.SessionID]
	if err := svc.ValidateSession(context.Background(), session.SessionID); err != nil {
		t.Fatalf("ValidateSession(valid) = %v", err)
	}
	after := sessions.byID[session.SessionID]
	if after.LastSeenAt != before.LastSeenAt || after.ExpiresAt != before.ExpiresAt {
		t.Fatalf("ValidateSession changed session timestamps: before=%+v after=%+v", before, after)
	}

	clock = session.ExpiresAt
	if err := svc.ValidateSession(context.Background(), session.SessionID); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("ValidateSession(expired) = %v, want ErrSessionExpired", err)
	}
	if _, ok := sessions.byID[session.SessionID]; !ok {
		t.Fatal("ValidateSession removed expired session; it must be side-effect free")
	}

	if err := svc.Logout(context.Background(), session.SessionID); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if err := svc.ValidateSession(context.Background(), session.SessionID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("ValidateSession(revoked) = %v, want ErrSessionNotFound", err)
	}
	if err := svc.ValidateSession(context.Background(), ""); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("ValidateSession(empty) = %v, want ErrSessionNotFound", err)
	}
}

func TestServiceTouchRejectsSessionIssuedBeforePasswordChange(t *testing.T) {
	users, sessions := newFakeRepositories()
	now, _ := fakeSessionTimes()
	svc := New(users, sessions, Options{SessionTTL: time.Hour, Now: func() time.Time { return now }})
	user := mustSeed(t, users, "admin", "correct-horse-battery")
	user.PasswordChangedAt = now.Add(10 * time.Minute)
	users.byID[user.UserID] = user
	addFakeSession(sessions, "old-session", user.UserID, now, now.Add(time.Hour))

	_, err := svc.Touch(context.Background(), "old-session")
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("Touch old password session = %v, want ErrSessionExpired", err)
	}
	if _, ok := sessions.byID["old-session"]; ok {
		t.Fatal("old password session was not deleted")
	}
}

func TestServiceTouchExpired(t *testing.T) {
	users, sessions := newFakeRepositories()
	now, _ := fakeSessionTimes()
	clock := now
	svc := New(users, sessions, Options{SessionTTL: time.Hour, Now: func() time.Time { return clock }})
	mustSeed(t, users, "admin", "correct-horse-battery")

	sess, err := svc.Login(context.Background(), "admin", "correct-horse-battery", "", "")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	clock = sess.ExpiresAt
	_, err = svc.Touch(context.Background(), sess.SessionID)
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("Touch at expiry = %v, want ErrSessionExpired", err)
	}
	if _, ok := sessions.byID[sess.SessionID]; ok {
		t.Fatal("expired session was not deleted")
	}
}

func TestServiceLogout(t *testing.T) {
	svc, users, _ := newTestService(t)
	mustSeed(t, users, "admin", "correct-horse-battery")

	sess, err := svc.Login(context.Background(), "admin", "correct-horse-battery", "", "")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if err := svc.Logout(context.Background(), sess.SessionID); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	_, err = svc.Touch(context.Background(), sess.SessionID)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("Touch after logout = %v, want ErrSessionNotFound", err)
	}
}

func TestServiceLogoutEmptyIsNoop(t *testing.T) {
	svc, _, _ := newTestService(t)
	if err := svc.Logout(context.Background(), ""); err != nil {
		t.Fatalf("Logout empty = %v, want nil", err)
	}
}

func TestServiceUserBySessionReturnsUser(t *testing.T) {
	svc, users, _ := newTestService(t)
	mustSeed(t, users, "admin", "correct-horse-battery")

	sess, err := svc.Login(context.Background(), "admin", "correct-horse-battery", "", "")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	u, err := svc.UserBySession(context.Background(), sess.SessionID)
	if err != nil {
		t.Fatalf("UserBySession: %v", err)
	}
	if u.Username != "admin" {
		t.Fatalf("Username = %q, want admin", u.Username)
	}
}

func TestServiceChangePassword(t *testing.T) {
	users, sessions := newFakeRepositories()
	now, later := fakeSessionTimes()
	svc := New(users, sessions, Options{SessionTTL: time.Hour, Now: func() time.Time { return now }})
	mustSeed(t, users, "admin", "correct-horse-battery")
	addFakeSession(sessions, "current-session", "usr_admin", now, later)
	addFakeSession(sessions, "other-session", "usr_admin", now, later)

	if err := svc.ChangePassword(context.Background(), "usr_admin", "current-session", "correct-horse-battery", "new-correct-horse-battery"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	_, err := svc.Login(context.Background(), "admin", "correct-horse-battery", "", "")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login old = %v, want ErrInvalidCredentials", err)
	}
	if _, err := svc.Login(context.Background(), "admin", "new-correct-horse-battery", "", ""); err != nil {
		t.Fatalf("Login new: %v", err)
	}
}

func TestServiceChangePasswordUsesConfiguredBcryptCost(t *testing.T) {
	users, sessions := newFakeRepositories()
	now, later := fakeSessionTimes()
	svc := New(users, sessions, Options{
		SessionTTL:         time.Hour,
		Now:                func() time.Time { return now },
		PasswordBcryptCost: bcrypt.MinCost,
	})
	mustSeed(t, users, "admin", "correct-horse-battery")
	addFakeSession(sessions, "current-session", "usr_admin", now, later)

	if err := svc.ChangePassword(context.Background(), "usr_admin", "current-session", "correct-horse-battery", "new-correct-horse-battery"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	user, err := users.FindByID(context.Background(), "usr_admin")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	got, err := bcrypt.Cost([]byte(user.PasswordHash))
	if err != nil {
		t.Fatalf("bcrypt.Cost: %v", err)
	}
	if got != bcrypt.MinCost {
		t.Fatalf("changed password bcrypt cost = %d, want %d", got, bcrypt.MinCost)
	}
}

func TestServiceChangePasswordWrongOld(t *testing.T) {
	svc, users, _ := newTestService(t)
	mustSeed(t, users, "admin", "correct-horse-battery")

	err := svc.ChangePassword(context.Background(), "usr_admin", "current-session", "wrong-old-pwd-xx", "new-correct-horse-battery")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("ChangePassword wrong old = %v, want ErrInvalidCredentials", err)
	}
}

func TestServiceChangePasswordRejectsTooShort(t *testing.T) {
	users, sessions := newFakeRepositories()
	now, later := fakeSessionTimes()
	svc := New(users, sessions, Options{SessionTTL: time.Hour, Now: func() time.Time { return now }})
	mustSeed(t, users, "admin", "correct-horse-battery")
	addFakeSession(sessions, "current-session", "usr_admin", now, later)

	err := svc.ChangePassword(context.Background(), "usr_admin", "current-session", "correct-horse-battery", "abc")
	if !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("ChangePassword short = %v, want ErrPasswordTooShort", err)
	}
}

func TestServiceChangePasswordRequiresLiveCurrentSession(t *testing.T) {
	svc, users, _ := newTestService(t)
	mustSeed(t, users, "admin", "correct-horse-battery")

	err := svc.ChangePassword(context.Background(), "usr_admin", "missing-session", "correct-horse-battery", "new-correct-horse-battery")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("ChangePassword missing session = %v, want ErrSessionNotFound", err)
	}
	user, err := users.FindByID(context.Background(), "usr_admin")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if VerifyPassword(user.PasswordHash, "correct-horse-battery") != nil {
		t.Fatal("password changed without a live current session")
	}
}

func TestServiceChangePasswordDeletesOtherSessions(t *testing.T) {
	users, sessions := newFakeRepositories()
	now, later := fakeSessionTimes()
	svc := New(users, sessions, Options{SessionTTL: time.Hour, Now: func() time.Time { return now }})
	mustSeed(t, users, "admin", "correct-horse-battery")
	addFakeSession(sessions, "current-session", "usr_admin", now, later)
	addFakeSession(sessions, "other-session", "usr_admin", now, later)
	addFakeSession(sessions, "other-user-session", "usr_other", now, later)

	if err := svc.ChangePassword(context.Background(), "usr_admin", "current-session", "correct-horse-battery", "new-correct-horse-battery"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if _, ok := sessions.byID["current-session"]; !ok {
		t.Fatal("current session was deleted")
	}
	if _, ok := sessions.byID["other-session"]; ok {
		t.Fatal("other session for same user was not deleted")
	}
	if _, ok := sessions.byID["other-user-session"]; !ok {
		t.Fatal("other user's session was deleted")
	}
	user, err := users.FindByID(context.Background(), "usr_admin")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if user.PasswordChangedAt != now {
		t.Fatalf("PasswordChangedAt = %s, want %s", user.PasswordChangedAt, now)
	}
	if sessions.byID["current-session"].IssuedAt != now {
		t.Fatalf("current session issued_at = %s, want %s", sessions.byID["current-session"].IssuedAt, now)
	}
}
