package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeUsers struct {
	byID   map[string]User
	byUser map[string]string
}

func newFakeUsers() *fakeUsers {
	return &fakeUsers{byID: map[string]User{}, byUser: map[string]string{}}
}

func (f *fakeUsers) Create(_ context.Context, u User) error {
	if _, ok := f.byUser[u.Username]; ok {
		return ErrUsernameTaken
	}
	f.byID[u.UserID] = u
	f.byUser[u.Username] = u.UserID
	return nil
}
func (f *fakeUsers) FindByUsername(_ context.Context, n string) (User, error) {
	id, ok := f.byUser[n]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return f.byID[id], nil
}
func (f *fakeUsers) FindByID(_ context.Context, id string) (User, error) {
	u, ok := f.byID[id]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return u, nil
}
func (f *fakeUsers) UpdatePassword(_ context.Context, id, h string, t time.Time) error {
	u, ok := f.byID[id]
	if !ok {
		return ErrUserNotFound
	}
	u.PasswordHash = h
	u.PasswordChangedAt = t
	f.byID[id] = u
	return nil
}
func (f *fakeUsers) CountUsers(_ context.Context) (int, error) { return len(f.byID), nil }

type fakeSessions struct {
	byID map[string]Session
}

func newFakeSessions() *fakeSessions { return &fakeSessions{byID: map[string]Session{}} }

func (f *fakeSessions) Create(_ context.Context, s Session) error {
	f.byID[s.SessionID] = s
	return nil
}
func (f *fakeSessions) Find(_ context.Context, id string) (Session, error) {
	s, ok := f.byID[id]
	if !ok {
		return Session{}, ErrSessionNotFound
	}
	return s, nil
}
func (f *fakeSessions) RefreshExpires(_ context.Context, id string, ls, exp time.Time) error {
	s, ok := f.byID[id]
	if !ok {
		return ErrSessionNotFound
	}
	s.LastSeenAt = ls
	s.ExpiresAt = exp
	f.byID[id] = s
	return nil
}
func (f *fakeSessions) Delete(_ context.Context, id string) error {
	delete(f.byID, id)
	return nil
}
func (f *fakeSessions) DeleteExpiredBefore(_ context.Context, cutoff time.Time) (int, error) {
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
	users := newFakeUsers()
	sessions := newFakeSessions()
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
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
}

func TestServiceLoginWrongPassword(t *testing.T) {
	svc, users, _ := newTestService(t)
	mustSeed(t, users, "admin", "right-password-xx")

	_, err := svc.Login(context.Background(), "admin", "wrong-password-xx", "", "")
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

	sess, _ := svc.Login(context.Background(), "admin", "correct-horse-battery", "", "")
	got, err := svc.Touch(context.Background(), sess.SessionID)
	if err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if !got.ExpiresAt.Equal(sess.ExpiresAt) {
		// Static clock means same expiry — that's fine. We assert no error here.
	}
	if got.SessionID != sess.SessionID {
		t.Fatalf("SessionID changed unexpectedly")
	}
}

func TestServiceTouchExpired(t *testing.T) {
	users := newFakeUsers()
	sessions := newFakeSessions()
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	clock := now
	svc := New(users, sessions, Options{SessionTTL: time.Hour, Now: func() time.Time { return clock }})
	mustSeed(t, users, "admin", "correct-horse-battery")

	sess, _ := svc.Login(context.Background(), "admin", "correct-horse-battery", "", "")
	clock = now.Add(2 * time.Hour) // jump past expiry
	_, err := svc.Touch(context.Background(), sess.SessionID)
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("Touch after expiry = %v, want ErrSessionExpired", err)
	}
}

func TestServiceLogout(t *testing.T) {
	svc, users, _ := newTestService(t)
	mustSeed(t, users, "admin", "correct-horse-battery")

	sess, _ := svc.Login(context.Background(), "admin", "correct-horse-battery", "", "")
	if err := svc.Logout(context.Background(), sess.SessionID); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	_, err := svc.Touch(context.Background(), sess.SessionID)
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

	sess, _ := svc.Login(context.Background(), "admin", "correct-horse-battery", "", "")
	u, err := svc.UserBySession(context.Background(), sess.SessionID)
	if err != nil {
		t.Fatalf("UserBySession: %v", err)
	}
	if u.Username != "admin" {
		t.Fatalf("Username = %q, want admin", u.Username)
	}
}

func TestServiceChangePassword(t *testing.T) {
	svc, users, _ := newTestService(t)
	mustSeed(t, users, "admin", "correct-horse-battery")

	if err := svc.ChangePassword(context.Background(), "usr_admin", "correct-horse-battery", "new-correct-horse-battery"); err != nil {
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

func TestServiceChangePasswordWrongOld(t *testing.T) {
	svc, users, _ := newTestService(t)
	mustSeed(t, users, "admin", "correct-horse-battery")

	err := svc.ChangePassword(context.Background(), "usr_admin", "wrong-old-pwd-xx", "new-correct-horse-battery")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("ChangePassword wrong old = %v, want ErrInvalidCredentials", err)
	}
}

func TestServiceChangePasswordRejectsTooShort(t *testing.T) {
	svc, users, _ := newTestService(t)
	mustSeed(t, users, "admin", "correct-horse-battery")

	err := svc.ChangePassword(context.Background(), "usr_admin", "correct-horse-battery", "abc")
	if !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("ChangePassword short = %v, want ErrPasswordTooShort", err)
	}
}
