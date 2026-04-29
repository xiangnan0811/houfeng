# Plan 1 · 后端 auth (V1.x) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add username/password login + session-cookie authentication to the houfeng-center Go service so that all user-facing API routes require an authenticated session, while leaving agent endpoints (`/api/agent/*`) and `/api/healthz` open.

**Architecture:** Single new package `internal/center/auth/` owns password hashing (bcrypt), session ID generation, cookie helpers, the auth service, and initial-user seeding. Two new repository files in `internal/center/store/` (`users.go`, `sessions.go`). Four new HTTP handlers in `internal/center/http/handlers/auth.go`. One new middleware in the `internal/center/http/` package wraps protected routes inside `router.New`. A small `SessionCleanupWorker` (mirrors `retention.Worker`) sweeps expired sessions hourly. `cmd/houfeng-center/bootstrap.go` wires it all together; `db/migrations/0010_add_users_and_sessions.sql` provides the schema.

**Tech Stack:** Go 1.26.2, `net/http` (existing pattern, no chi), `github.com/jackc/pgx/v5`, raw SQL migrations, `golang.org/x/crypto/bcrypt`, `crypto/rand` for session IDs. Tests follow the existing patterns in `internal/center/store/*_test.go` (Postgres-backed) and `internal/center/http/handlers/*_test.go` (httptest).

**Out of scope:** Frontend integration (Plan 2), CSRF tokens, multi-device session listing, password reset emails, OAuth/SSO, password complexity rules beyond `len ≥ 8`.

---

## File Structure

### New files
```
db/migrations/0010_add_users_and_sessions.sql
internal/center/auth/types.go            # User, Session, errors, repository interfaces
internal/center/auth/password.go         # bcrypt wrappers
internal/center/auth/password_test.go
internal/center/auth/session_id.go       # crypto/rand 256-bit hex
internal/center/auth/session_id_test.go
internal/center/auth/cookie.go           # SetSessionCookie / ClearSessionCookie / ReadSessionCookie
internal/center/auth/cookie_test.go
internal/center/auth/service.go          # Service: Login / Logout / Touch / UserBySession / ChangePassword
internal/center/auth/service_test.go
internal/center/auth/seed.go             # SeedInitialUser (env-driven)
internal/center/auth/seed_test.go
internal/center/auth/cleanup.go          # SessionCleanupWorker (Worker interface impl)
internal/center/auth/cleanup_test.go
internal/center/store/users.go           # PostgresUserRepository
internal/center/store/users_test.go
internal/center/store/sessions.go        # PostgresSessionRepository
internal/center/store/sessions_test.go
internal/center/http/middleware.go       # RequireSession(svc) func wraps http.Handler
internal/center/http/middleware_test.go
internal/center/http/handlers/auth.go    # Login / Logout / Me / ChangePassword
internal/center/http/handlers/auth_test.go
```

### Modified files
```
go.mod / go.sum                                                  # add golang.org/x/crypto
internal/center/config/config.go                                 # InitialUsername / InitialPassword / SessionTTL
internal/center/config/config_test.go                            # cover new vars
internal/center/http/router.go                                   # RouterOptions + auth wiring
internal/center/http/router_test.go                              # auth route tests
cmd/houfeng-center/bootstrap.go                                  # construct repos / service / seed / middleware / handlers
cmd/houfeng-center/bootstrap_test.go                             # cover new wiring
.env.example                                                     # HOUFENG_INITIAL_USERNAME / _PASSWORD / _SESSION_TTL
docs/deploy/local-and-systemd.md                                 # document new env vars + seed behavior
```

---

### Task 1: Add `golang.org/x/crypto` dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Verify it's missing**

Run: `grep -c '"golang.org/x/crypto"' go.sum || true`
Expected: empty / 0 (the bcrypt subpath isn't pulled yet).

- [ ] **Step 2: Add bcrypt**

Run: `go get golang.org/x/crypto/bcrypt@latest`
Expected: `go.mod` and `go.sum` updated; output mentions a version like `golang.org/x/crypto vX.Y.Z`.

- [ ] **Step 3: Tidy**

Run: `go mod tidy`
Expected: clean exit; no new deps removed accidentally.

- [ ] **Step 4: Sanity build**

Run: `go build ./...`
Expected: green.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum
git commit -m "Add golang.org/x/crypto/bcrypt for auth password hashing"
```

---

### Task 2: Migration `0010_add_users_and_sessions.sql`

**Files:**
- Create: `db/migrations/0010_add_users_and_sessions.sql`

- [ ] **Step 1: Write the migration**

Create `db/migrations/0010_add_users_and_sessions.sql`:

```sql
create table users (
  user_id              text primary key,
  username             text not null unique,
  password_hash        text not null,
  display_name         text not null default '',
  role                 text not null default 'admin',
  created_at           timestamptz not null default now(),
  password_changed_at  timestamptz not null default now()
);

create table sessions (
  session_id    text primary key,
  user_id       text not null references users(user_id) on delete cascade,
  issued_at     timestamptz not null default now(),
  last_seen_at  timestamptz not null default now(),
  expires_at    timestamptz not null,
  user_agent    text not null default '',
  client_ip     text not null default ''
);

create index sessions_user_idx on sessions(user_id);
create index sessions_expires_idx on sessions(expires_at);
```

- [ ] **Step 2: Verify it loads via the embed list**

Run: `go test ./internal/center/store/migrate/... -run TestEmbed -v` (use the existing migrate tests; if no such name, run the full migrate test file: `go test ./internal/center/store/migrate -v`).
Expected: passes — embedded list now includes `0010_*`.

- [ ] **Step 3: Apply against a local Postgres (manual sanity)**

Optional manual check; skip if no Postgres handy. The migrate tests above already exercise apply.

- [ ] **Step 4: Commit**

```bash
git add db/migrations/0010_add_users_and_sessions.sql
git commit -m "Add users and sessions schema migration"
```

---

### Task 3: Domain types and repository interfaces (`auth/types.go`)

**Files:**
- Create: `internal/center/auth/types.go`

- [ ] **Step 1: Write the file**

```go
package auth

import (
	"context"
	"errors"
	"time"
)

const (
	RoleAdmin = "admin"

	SessionCookieName = "houfeng_session"

	DefaultSessionTTL  = 7 * 24 * time.Hour
	MinPasswordLength  = 8
	MaxPasswordLength  = 256
	MinUsernameLength  = 1
	MaxUsernameLength  = 64
)

var (
	ErrUserNotFound        = errors.New("user not found")
	ErrUsernameTaken       = errors.New("username already taken")
	ErrInvalidCredentials  = errors.New("invalid username or password")
	ErrSessionNotFound     = errors.New("session not found")
	ErrSessionExpired      = errors.New("session expired")
	ErrPasswordTooShort    = errors.New("password too short")
	ErrPasswordTooLong     = errors.New("password too long")
	ErrUsernameInvalid     = errors.New("username invalid")
	ErrPasswordHashInvalid = errors.New("password hash invalid")
)

type User struct {
	UserID             string
	Username           string
	PasswordHash       string
	DisplayName        string
	Role               string
	CreatedAt          time.Time
	PasswordChangedAt  time.Time
}

type Session struct {
	SessionID  string
	UserID     string
	IssuedAt   time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	UserAgent  string
	ClientIP   string
}

type UserRepository interface {
	Create(ctx context.Context, u User) error
	FindByUsername(ctx context.Context, username string) (User, error)
	FindByID(ctx context.Context, userID string) (User, error)
	UpdatePassword(ctx context.Context, userID, newHash string, changedAt time.Time) error
	CountUsers(ctx context.Context) (int, error)
}

type SessionRepository interface {
	Create(ctx context.Context, s Session) error
	Find(ctx context.Context, sessionID string) (Session, error)
	RefreshExpires(ctx context.Context, sessionID string, lastSeenAt, expiresAt time.Time) error
	Delete(ctx context.Context, sessionID string) error
	DeleteExpiredBefore(ctx context.Context, cutoff time.Time) (int, error)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/center/auth/...`
Expected: green.

- [ ] **Step 3: Commit**

```bash
git add internal/center/auth/types.go
git commit -m "Add auth domain types and repository interfaces"
```

---

### Task 4: Password hashing (`auth/password.go`)

**Files:**
- Create: `internal/center/auth/password.go`
- Test: `internal/center/auth/password_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/center/auth/password_test.go`:

```go
package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestHashPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "" {
		t.Fatal("hash must not be empty")
	}
	if strings.HasPrefix(hash, "correct") {
		t.Fatal("hash must not contain plaintext")
	}

	if err := VerifyPassword(hash, "correct-horse-battery-staple"); err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
}

func TestVerifyPasswordWrong(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	err = VerifyPassword(hash, "wrong-password")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("VerifyPassword wrong = %v, want ErrInvalidCredentials", err)
	}
}

func TestHashPasswordRejectsShort(t *testing.T) {
	_, err := HashPassword("short")
	if !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("HashPassword short = %v, want ErrPasswordTooShort", err)
	}
}

func TestHashPasswordRejectsLong(t *testing.T) {
	_, err := HashPassword(strings.Repeat("a", MaxPasswordLength+1))
	if !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("HashPassword long = %v, want ErrPasswordTooLong", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/center/auth -run TestHashPassword -v`
Expected: FAIL — `undefined: HashPassword`.

- [ ] **Step 3: Write the minimal implementation**

Create `internal/center/auth/password.go`:

```go
package auth

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = bcrypt.DefaultCost

func HashPassword(plain string) (string, error) {
	if len(plain) < MinPasswordLength {
		return "", ErrPasswordTooShort
	}
	if len(plain) > MaxPasswordLength {
		return "", ErrPasswordTooLong
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("bcrypt generate: %w", err)
	}
	return string(hashed), nil
}

func VerifyPassword(hashed, plain string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return ErrInvalidCredentials
	}
	if errors.Is(err, bcrypt.ErrHashTooShort) || err != nil && len(hashed) < 4 {
		return ErrPasswordHashInvalid
	}
	if err != nil {
		return fmt.Errorf("bcrypt compare: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/center/auth -run TestHashPassword -v && go test ./internal/center/auth -run TestVerifyPassword -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/center/auth/password.go internal/center/auth/password_test.go
git commit -m "Add bcrypt password hashing helpers"
```

---

### Task 5: Session ID generator (`auth/session_id.go`)

**Files:**
- Create: `internal/center/auth/session_id.go`
- Test: `internal/center/auth/session_id_test.go`

- [ ] **Step 1: Write the failing test**

```go
package auth

import (
	"testing"
)

func TestNewSessionIDLengthAndCharset(t *testing.T) {
	id, err := NewSessionID()
	if err != nil {
		t.Fatalf("NewSessionID: %v", err)
	}
	if len(id) != 64 {
		t.Fatalf("len(id) = %d, want 64", len(id))
	}
	for _, r := range id {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !isHex {
			t.Fatalf("non-hex character %q in %q", r, id)
		}
	}
}

func TestNewSessionIDUnique(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 256; i++ {
		id, err := NewSessionID()
		if err != nil {
			t.Fatalf("NewSessionID: %v", err)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate session id %q", id)
		}
		seen[id] = struct{}{}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/center/auth -run TestNewSessionID -v`
Expected: FAIL — undefined.

- [ ] **Step 3: Write the implementation**

```go
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func NewSessionID() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/center/auth -run TestNewSessionID -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/center/auth/session_id.go internal/center/auth/session_id_test.go
git commit -m "Add cryptographically random session ID generator"
```

---

### Task 6: Cookie helpers (`auth/cookie.go`)

**Files:**
- Create: `internal/center/auth/cookie.go`
- Test: `internal/center/auth/cookie_test.go`

- [ ] **Step 1: Write the failing test**

```go
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSetAndReadSessionCookie(t *testing.T) {
	w := httptest.NewRecorder()
	expires := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	SetSessionCookie(w, "abc123", expires)

	resp := w.Result()
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("len(cookies) = %d, want 1", len(cookies))
	}
	c := cookies[0]
	if c.Name != SessionCookieName {
		t.Errorf("name = %q, want %q", c.Name, SessionCookieName)
	}
	if c.Value != "abc123" {
		t.Errorf("value = %q, want abc123", c.Value)
	}
	if !c.HttpOnly {
		t.Error("cookie must be HttpOnly")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("sameSite = %v, want Lax", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("path = %q, want /", c.Path)
	}
}

func TestReadSessionCookieEmptyWhenAbsent(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	got := ReadSessionCookie(r)
	if got != "" {
		t.Errorf("ReadSessionCookie absent = %q, want empty", got)
	}
}

func TestClearSessionCookie(t *testing.T) {
	w := httptest.NewRecorder()
	ClearSessionCookie(w)
	cookie := w.Result().Cookies()[0]
	if cookie.Value != "" {
		t.Errorf("value = %q, want empty", cookie.Value)
	}
	if cookie.MaxAge != -1 {
		t.Errorf("MaxAge = %d, want -1", cookie.MaxAge)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/center/auth -run "TestSetAndReadSessionCookie|TestReadSessionCookieEmptyWhenAbsent|TestClearSessionCookie" -v`
Expected: FAIL.

- [ ] **Step 3: Write the implementation**

```go
package auth

import (
	"net/http"
	"time"
)

func SetSessionCookie(w http.ResponseWriter, sessionID string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    sessionID,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   false,
	})
}

func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func ReadSessionCookie(r *http.Request) string {
	c, err := r.Cookie(SessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}
```

> Note: `Secure: false` is appropriate for V1.x — HttpOnly + SameSite=Lax + reverse-proxy HTTPS termination is the security model; per-cookie `Secure` adds defense-in-depth that's unnecessary at this scope and was deliberately dropped to keep the API surface minimal. Document in deploy guide (Task 21) that the proxy must terminate HTTPS.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/center/auth -run "Cookie" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/center/auth/cookie.go internal/center/auth/cookie_test.go
git commit -m "Add session cookie helpers"
```

---

### Task 7: Users repository (`store/users.go`)

**Files:**
- Create: `internal/center/store/users.go`
- Test: `internal/center/store/users_test.go`

Reference pattern: `internal/center/store/nodes.go` and `internal/center/store/nodes_test.go` (Postgres-backed test harness; honors the same `HOUFENG_TEST_DATABASE_URL` env or skips if unset).

- [ ] **Step 1: Write the failing test**

Read `nodes_test.go` to copy the boilerplate that opens a test pool and applies migrations. Then add `users_test.go`:

```go
package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/auth"
)

func TestPostgresUserRepositoryCreateAndFind(t *testing.T) {
	ctx := context.Background()
	pool := requireTestPool(t) // existing helper used by nodes_test.go
	repo := NewPostgresUserRepository(pool)

	u := auth.User{
		UserID:            "usr_" + tinyULID(t),
		Username:          "admin-" + tinyULID(t),
		PasswordHash:      "$2y$10$abcdefghijklmnopqrstuv",
		DisplayName:       "Admin",
		Role:              auth.RoleAdmin,
		CreatedAt:         time.Now().UTC().Truncate(time.Microsecond),
		PasswordChangedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindByUsername(ctx, u.Username)
	if err != nil {
		t.Fatalf("FindByUsername: %v", err)
	}
	if got.UserID != u.UserID || got.PasswordHash != u.PasswordHash || got.Role != u.Role {
		t.Fatalf("FindByUsername mismatch: got %+v want %+v", got, u)
	}

	got2, err := repo.FindByID(ctx, u.UserID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got2.Username != u.Username {
		t.Fatalf("FindByID username = %q, want %q", got2.Username, u.Username)
	}
}

func TestPostgresUserRepositoryUpdatePassword(t *testing.T) {
	ctx := context.Background()
	pool := requireTestPool(t)
	repo := NewPostgresUserRepository(pool)

	u := auth.User{
		UserID:            "usr_" + tinyULID(t),
		Username:          "admin-" + tinyULID(t),
		PasswordHash:      "old",
		Role:              auth.RoleAdmin,
		CreatedAt:         time.Now().UTC().Truncate(time.Microsecond),
		PasswordChangedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}

	newAt := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	if err := repo.UpdatePassword(ctx, u.UserID, "new", newAt); err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}
	got, _ := repo.FindByID(ctx, u.UserID)
	if got.PasswordHash != "new" {
		t.Fatalf("PasswordHash = %q, want new", got.PasswordHash)
	}
	if !got.PasswordChangedAt.Equal(newAt) {
		t.Fatalf("PasswordChangedAt = %v, want %v", got.PasswordChangedAt, newAt)
	}
}

func TestPostgresUserRepositoryFindByUsernameMissing(t *testing.T) {
	ctx := context.Background()
	pool := requireTestPool(t)
	repo := NewPostgresUserRepository(pool)
	_, err := repo.FindByUsername(ctx, "absent-"+tinyULID(t))
	if !errors.Is(err, auth.ErrUserNotFound) {
		t.Fatalf("FindByUsername missing = %v, want ErrUserNotFound", err)
	}
}

func TestPostgresUserRepositoryDuplicateUsername(t *testing.T) {
	ctx := context.Background()
	pool := requireTestPool(t)
	repo := NewPostgresUserRepository(pool)
	username := "dup-" + tinyULID(t)
	a := auth.User{UserID: "usr_a_" + tinyULID(t), Username: username, PasswordHash: "x", Role: auth.RoleAdmin, CreatedAt: time.Now().UTC()}
	b := auth.User{UserID: "usr_b_" + tinyULID(t), Username: username, PasswordHash: "y", Role: auth.RoleAdmin, CreatedAt: time.Now().UTC()}
	if err := repo.Create(ctx, a); err != nil {
		t.Fatalf("Create a: %v", err)
	}
	err := repo.Create(ctx, b)
	if !errors.Is(err, auth.ErrUsernameTaken) {
		t.Fatalf("Create dup = %v, want ErrUsernameTaken", err)
	}
}

func TestPostgresUserRepositoryCount(t *testing.T) {
	ctx := context.Background()
	pool := requireTestPool(t)
	repo := NewPostgresUserRepository(pool)
	got, err := repo.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if got < 0 {
		t.Fatalf("CountUsers = %d, want non-negative", got)
	}
}
```

> If `requireTestPool` and `tinyULID` helpers don't exist in store/ tests verbatim, copy the pool-bootstrapping snippet at the top of `nodes_test.go` and use any available unique-id helper (or write `tinyULID(t *testing.T) string` returning `fmt.Sprintf("%d", t.Now().UnixNano())`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/center/store -run TestPostgresUserRepository -v`
Expected: FAIL — `NewPostgresUserRepository` undefined.

- [ ] **Step 3: Write the implementation**

```go
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/auth"
)

type PostgresUserRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresUserRepository(pool *pgxpool.Pool) *PostgresUserRepository {
	return &PostgresUserRepository{pool: pool}
}

func (r *PostgresUserRepository) Create(ctx context.Context, u auth.User) error {
	_, err := r.pool.Exec(ctx, `
		insert into users (user_id, username, password_hash, display_name, role, created_at, password_changed_at)
		values ($1, $2, $3, $4, $5, $6, $7)`,
		u.UserID, u.Username, u.PasswordHash, u.DisplayName, u.Role, u.CreatedAt, u.PasswordChangedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return auth.ErrUsernameTaken
		}
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

func (r *PostgresUserRepository) FindByUsername(ctx context.Context, username string) (auth.User, error) {
	return r.queryOne(ctx, `
		select user_id, username, password_hash, display_name, role, created_at, password_changed_at
		from users where username = $1`, username)
}

func (r *PostgresUserRepository) FindByID(ctx context.Context, userID string) (auth.User, error) {
	return r.queryOne(ctx, `
		select user_id, username, password_hash, display_name, role, created_at, password_changed_at
		from users where user_id = $1`, userID)
}

func (r *PostgresUserRepository) queryOne(ctx context.Context, sql, arg string) (auth.User, error) {
	var u auth.User
	err := r.pool.QueryRow(ctx, sql, arg).Scan(
		&u.UserID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Role, &u.CreatedAt, &u.PasswordChangedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.User{}, auth.ErrUserNotFound
	}
	if err != nil {
		return auth.User{}, fmt.Errorf("query user: %w", err)
	}
	return u, nil
}

func (r *PostgresUserRepository) UpdatePassword(ctx context.Context, userID, newHash string, changedAt time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		update users set password_hash = $2, password_changed_at = $3 where user_id = $1`,
		userID, newHash, changedAt,
	)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrUserNotFound
	}
	return nil
}

func (r *PostgresUserRepository) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `select count(*) from users`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/center/store -run TestPostgresUserRepository -v`
Expected: PASS (skipped if `HOUFENG_TEST_DATABASE_URL` is unset, like other store tests).

- [ ] **Step 5: Commit**

```bash
git add internal/center/store/users.go internal/center/store/users_test.go
git commit -m "Add Postgres users repository"
```

---

### Task 8: Sessions repository (`store/sessions.go`)

**Files:**
- Create: `internal/center/store/sessions.go`
- Test: `internal/center/store/sessions_test.go`

- [ ] **Step 1: Write the failing test**

```go
package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/auth"
)

func TestPostgresSessionRepositoryCRUD(t *testing.T) {
	ctx := context.Background()
	pool := requireTestPool(t)
	users := NewPostgresUserRepository(pool)
	repo := NewPostgresSessionRepository(pool)

	u := auth.User{UserID: "usr_" + tinyULID(t), Username: "ses-" + tinyULID(t), PasswordHash: "x", Role: auth.RoleAdmin, CreatedAt: time.Now().UTC()}
	if err := users.Create(ctx, u); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	s := auth.Session{
		SessionID:  "sess_" + tinyULID(t),
		UserID:     u.UserID,
		IssuedAt:   now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(time.Hour),
		UserAgent:  "Chrome/test",
		ClientIP:   "127.0.0.1",
	}
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create session: %v", err)
	}

	got, err := repo.Find(ctx, s.SessionID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.UserID != u.UserID {
		t.Fatalf("UserID = %q, want %q", got.UserID, u.UserID)
	}

	newSeen := now.Add(30 * time.Minute)
	newExp := now.Add(2 * time.Hour)
	if err := repo.RefreshExpires(ctx, s.SessionID, newSeen, newExp); err != nil {
		t.Fatalf("RefreshExpires: %v", err)
	}
	got2, _ := repo.Find(ctx, s.SessionID)
	if !got2.ExpiresAt.Equal(newExp) {
		t.Fatalf("ExpiresAt = %v, want %v", got2.ExpiresAt, newExp)
	}

	if err := repo.Delete(ctx, s.SessionID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Find(ctx, s.SessionID); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("Find after delete = %v, want ErrSessionNotFound", err)
	}
}

func TestPostgresSessionRepositoryDeleteExpired(t *testing.T) {
	ctx := context.Background()
	pool := requireTestPool(t)
	users := NewPostgresUserRepository(pool)
	repo := NewPostgresSessionRepository(pool)

	u := auth.User{UserID: "usr_" + tinyULID(t), Username: "exp-" + tinyULID(t), PasswordHash: "x", Role: auth.RoleAdmin, CreatedAt: time.Now().UTC()}
	if err := users.Create(ctx, u); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	old := time.Now().UTC().Add(-2 * time.Hour)
	for i := 0; i < 3; i++ {
		_ = repo.Create(ctx, auth.Session{
			SessionID:  "sess_old_" + tinyULID(t),
			UserID:     u.UserID,
			IssuedAt:   old,
			LastSeenAt: old,
			ExpiresAt:  old.Add(time.Minute),
		})
	}
	cutoff := time.Now().UTC()
	n, err := repo.DeleteExpiredBefore(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteExpiredBefore: %v", err)
	}
	if n < 3 {
		t.Fatalf("deleted = %d, want >= 3", n)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/center/store -run TestPostgresSessionRepository -v`
Expected: FAIL.

- [ ] **Step 3: Write the implementation**

```go
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/auth"
)

type PostgresSessionRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresSessionRepository(pool *pgxpool.Pool) *PostgresSessionRepository {
	return &PostgresSessionRepository{pool: pool}
}

func (r *PostgresSessionRepository) Create(ctx context.Context, s auth.Session) error {
	_, err := r.pool.Exec(ctx, `
		insert into sessions (session_id, user_id, issued_at, last_seen_at, expires_at, user_agent, client_ip)
		values ($1, $2, $3, $4, $5, $6, $7)`,
		s.SessionID, s.UserID, s.IssuedAt, s.LastSeenAt, s.ExpiresAt, s.UserAgent, s.ClientIP,
	)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (r *PostgresSessionRepository) Find(ctx context.Context, sessionID string) (auth.Session, error) {
	var s auth.Session
	err := r.pool.QueryRow(ctx, `
		select session_id, user_id, issued_at, last_seen_at, expires_at, user_agent, client_ip
		from sessions where session_id = $1`, sessionID,
	).Scan(&s.SessionID, &s.UserID, &s.IssuedAt, &s.LastSeenAt, &s.ExpiresAt, &s.UserAgent, &s.ClientIP)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Session{}, auth.ErrSessionNotFound
	}
	if err != nil {
		return auth.Session{}, fmt.Errorf("query session: %w", err)
	}
	return s, nil
}

func (r *PostgresSessionRepository) RefreshExpires(ctx context.Context, sessionID string, lastSeenAt, expiresAt time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		update sessions set last_seen_at = $2, expires_at = $3 where session_id = $1`,
		sessionID, lastSeenAt, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("refresh session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrSessionNotFound
	}
	return nil
}

func (r *PostgresSessionRepository) Delete(ctx context.Context, sessionID string) error {
	_, err := r.pool.Exec(ctx, `delete from sessions where session_id = $1`, sessionID)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (r *PostgresSessionRepository) DeleteExpiredBefore(ctx context.Context, cutoff time.Time) (int, error) {
	tag, err := r.pool.Exec(ctx, `delete from sessions where expires_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete expired: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/center/store -run TestPostgresSessionRepository -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/center/store/sessions.go internal/center/store/sessions_test.go
git commit -m "Add Postgres sessions repository"
```

---

### Task 9: Auth service (`auth/service.go`)

**Files:**
- Create: `internal/center/auth/service.go`
- Test: `internal/center/auth/service_test.go`

The service uses in-memory fakes implementing the repository interfaces; no Postgres needed.

- [ ] **Step 1: Write the failing test**

```go
package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeUsers struct {
	byID    map[string]User
	byUser  map[string]string // username -> userID
}

func newFakeUsers() *fakeUsers { return &fakeUsers{byID: map[string]User{}, byUser: map[string]string{}} }

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

func TestServiceLoginSuccess(t *testing.T) {
	svc, users, _ := newTestService(t)

	hash, _ := HashPassword("correct-horse-battery-staple")
	_ = users.Create(context.Background(), User{
		UserID:       "u1",
		Username:     "admin",
		PasswordHash: hash,
		Role:         RoleAdmin,
	})

	sess, err := svc.Login(context.Background(), "admin", "correct-horse-battery-staple", "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if sess.UserID != "u1" {
		t.Fatalf("UserID = %q, want u1", sess.UserID)
	}
	if sess.SessionID == "" {
		t.Fatal("SessionID empty")
	}
}

func TestServiceLoginWrongPassword(t *testing.T) {
	svc, users, _ := newTestService(t)
	hash, _ := HashPassword("right-password-xx")
	_ = users.Create(context.Background(), User{UserID: "u1", Username: "admin", PasswordHash: hash, Role: RoleAdmin})

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

func TestServiceTouchExtendsExpiry(t *testing.T) {
	svc, users, _ := newTestService(t)
	hash, _ := HashPassword("correct-horse-battery-staple")
	_ = users.Create(context.Background(), User{UserID: "u1", Username: "admin", PasswordHash: hash, Role: RoleAdmin})
	sess, _ := svc.Login(context.Background(), "admin", "correct-horse-battery-staple", "", "")

	got, err := svc.Touch(context.Background(), sess.SessionID)
	if err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if !got.ExpiresAt.After(sess.ExpiresAt.Add(-time.Second)) {
		t.Fatalf("Touch did not extend expiry: %v vs %v", got.ExpiresAt, sess.ExpiresAt)
	}
}

func TestServiceTouchExpired(t *testing.T) {
	users := newFakeUsers()
	sessions := newFakeSessions()
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	clock := &now
	svc := New(users, sessions, Options{SessionTTL: time.Hour, Now: func() time.Time { return *clock }})
	hash, _ := HashPassword("correct-horse-battery-staple")
	_ = users.Create(context.Background(), User{UserID: "u1", Username: "admin", PasswordHash: hash, Role: RoleAdmin})
	sess, _ := svc.Login(context.Background(), "admin", "correct-horse-battery-staple", "", "")

	*clock = now.Add(2 * time.Hour) // jump past expiry
	_, err := svc.Touch(context.Background(), sess.SessionID)
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("Touch after expiry = %v, want ErrSessionExpired", err)
	}
}

func TestServiceLogout(t *testing.T) {
	svc, users, _ := newTestService(t)
	hash, _ := HashPassword("correct-horse-battery-staple")
	_ = users.Create(context.Background(), User{UserID: "u1", Username: "admin", PasswordHash: hash, Role: RoleAdmin})
	sess, _ := svc.Login(context.Background(), "admin", "correct-horse-battery-staple", "", "")

	if err := svc.Logout(context.Background(), sess.SessionID); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	_, err := svc.Touch(context.Background(), sess.SessionID)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("Touch after logout = %v, want ErrSessionNotFound", err)
	}
}

func TestServiceChangePassword(t *testing.T) {
	svc, users, _ := newTestService(t)
	hash, _ := HashPassword("correct-horse-battery-staple")
	_ = users.Create(context.Background(), User{UserID: "u1", Username: "admin", PasswordHash: hash, Role: RoleAdmin})

	if err := svc.ChangePassword(context.Background(), "u1", "correct-horse-battery-staple", "new-correct-horse-battery"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	_, err := svc.Login(context.Background(), "admin", "correct-horse-battery-staple", "", "")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login old = %v, want ErrInvalidCredentials", err)
	}
	if _, err := svc.Login(context.Background(), "admin", "new-correct-horse-battery", "", ""); err != nil {
		t.Fatalf("Login new: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/center/auth -run TestService -v`
Expected: FAIL.

- [ ] **Step 3: Write the implementation**

```go
package auth

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Options struct {
	SessionTTL time.Duration
	Now        func() time.Time
}

type Service struct {
	users    UserRepository
	sessions SessionRepository
	now      func() time.Time
	ttl      time.Duration
}

func New(users UserRepository, sessions SessionRepository, opts Options) *Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	ttl := opts.SessionTTL
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	return &Service{users: users, sessions: sessions, now: now, ttl: ttl}
}

func (s *Service) Login(ctx context.Context, username, password, userAgent, clientIP string) (Session, error) {
	username = strings.TrimSpace(username)
	if len(username) < MinUsernameLength || len(username) > MaxUsernameLength {
		return Session{}, ErrInvalidCredentials
	}
	u, err := s.users.FindByUsername(ctx, username)
	if err != nil {
		// Constant-time-ish: do a dummy hash compare to avoid leaking user existence.
		_ = VerifyPassword("$2y$10$invalidinvalidinvalidinvalidinvalidinvalidinvalidinvalid", password)
		return Session{}, ErrInvalidCredentials
	}
	if err := VerifyPassword(u.PasswordHash, password); err != nil {
		return Session{}, ErrInvalidCredentials
	}

	id, err := NewSessionID()
	if err != nil {
		return Session{}, fmt.Errorf("new session id: %w", err)
	}
	now := s.now().UTC()
	sess := Session{
		SessionID:  id,
		UserID:     u.UserID,
		IssuedAt:   now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(s.ttl),
		UserAgent:  userAgent,
		ClientIP:   clientIP,
	}
	if err := s.sessions.Create(ctx, sess); err != nil {
		return Session{}, err
	}
	return sess, nil
}

func (s *Service) Logout(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	return s.sessions.Delete(ctx, sessionID)
}

// Touch validates a session, extends its expiry, and returns the refreshed Session.
func (s *Service) Touch(ctx context.Context, sessionID string) (Session, error) {
	sess, err := s.sessions.Find(ctx, sessionID)
	if err != nil {
		return Session{}, err
	}
	now := s.now().UTC()
	if sess.ExpiresAt.Before(now) {
		_ = s.sessions.Delete(ctx, sessionID)
		return Session{}, ErrSessionExpired
	}
	newExp := now.Add(s.ttl)
	if err := s.sessions.RefreshExpires(ctx, sessionID, now, newExp); err != nil {
		return Session{}, err
	}
	sess.LastSeenAt = now
	sess.ExpiresAt = newExp
	return sess, nil
}

func (s *Service) UserBySession(ctx context.Context, sessionID string) (User, error) {
	sess, err := s.Touch(ctx, sessionID)
	if err != nil {
		return User{}, err
	}
	return s.users.FindByID(ctx, sess.UserID)
}

func (s *Service) ChangePassword(ctx context.Context, userID, oldPassword, newPassword string) error {
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if err := VerifyPassword(u.PasswordHash, oldPassword); err != nil {
		return ErrInvalidCredentials
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	return s.users.UpdatePassword(ctx, userID, hash, s.now().UTC())
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/center/auth -run TestService -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/center/auth/service.go internal/center/auth/service_test.go
git commit -m "Add auth service with login/touch/logout/change-password"
```

---

### Task 10: Login handler (`handlers/auth.go` — Login)

**Files:**
- Create: `internal/center/http/handlers/auth.go` (Login only this task; subsequent handlers added in 11-13)
- Test: `internal/center/http/handlers/auth_test.go`

- [ ] **Step 1: Write the failing test**

```go
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/auth"
)

type stubAuth struct {
	loginErr   error
	loginSess  auth.Session
	loginUser  auth.User
	logoutErr  error
	touchErr   error
	touchSess  auth.Session
	touchUser  auth.User
	chgErr     error
	gotUser    string
	gotPass    string
}

func (s *stubAuth) Login(_ context.Context, username, password, _, _ string) (auth.Session, error) {
	s.gotUser, s.gotPass = username, password
	return s.loginSess, s.loginErr
}
func (s *stubAuth) Logout(_ context.Context, _ string) error                 { return s.logoutErr }
func (s *stubAuth) Touch(_ context.Context, _ string) (auth.Session, error)  { return s.touchSess, s.touchErr }
func (s *stubAuth) UserBySession(_ context.Context, _ string) (auth.User, error) {
	return s.touchUser, s.touchErr
}
func (s *stubAuth) ChangePassword(_ context.Context, _, _, _ string) error { return s.chgErr }

func TestLoginHandlerSuccess(t *testing.T) {
	svc := &stubAuth{
		loginSess: auth.Session{SessionID: "abc", UserID: "u1", ExpiresAt: time.Now().Add(time.Hour)},
	}
	h := Login(svc)

	body := strings.NewReader(`{"username":"admin","password":"correct-horse"}`)
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != auth.SessionCookieName || cookies[0].Value != "abc" {
		t.Fatalf("cookie = %+v, want %s=abc", cookies, auth.SessionCookieName)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["user_id"] != "u1" {
		t.Fatalf("user_id = %v, want u1", resp["user_id"])
	}
	if svc.gotUser != "admin" || svc.gotPass != "correct-horse" {
		t.Fatalf("forwarded creds = %q/%q", svc.gotUser, svc.gotPass)
	}
}

func TestLoginHandlerInvalidCredentials(t *testing.T) {
	svc := &stubAuth{loginErr: auth.ErrInvalidCredentials}
	h := Login(svc)
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"x","password":"y"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("must not set cookie on failure")
	}
}

func TestLoginHandlerRejectNonPost(t *testing.T) {
	svc := &stubAuth{}
	h := Login(svc)
	r := httptest.NewRequest(http.MethodGet, "/api/auth/login", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", w.Code)
	}
}

func TestLoginHandlerRejectMalformedBody(t *testing.T) {
	svc := &stubAuth{}
	h := Login(svc)
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{not json`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/center/http/handlers -run TestLoginHandler -v`
Expected: FAIL.

- [ ] **Step 3: Write the implementation**

Create `internal/center/http/handlers/auth.go`:

```go
package handlers

import (
	"context"
	"errors"
	"net/http"

	"houfeng/internal/center/auth"
)

// AuthService is the subset of auth.Service used by handlers (allows test stubs).
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

type loginResponse struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	DisplayName string `json:"display_name"`
}

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
		// Optionally re-fetch user; here we pass back what's known on the session.
		writeJSON(w, http.StatusOK, loginResponse{UserID: sess.UserID})
	}
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	return r.RemoteAddr
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/center/http/handlers -run TestLoginHandler -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/center/http/handlers/auth.go internal/center/http/handlers/auth_test.go
git commit -m "Add auth login handler"
```

---

### Task 11: Logout handler

**Files:**
- Modify: `internal/center/http/handlers/auth.go`
- Modify: `internal/center/http/handlers/auth_test.go`

- [ ] **Step 1: Write the failing test (append to auth_test.go)**

```go
func TestLogoutHandlerClearsCookie(t *testing.T) {
	svc := &stubAuth{}
	h := Logout(svc)

	r := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "abc"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Value != "" || cookies[0].MaxAge != -1 {
		t.Fatalf("cookie = %+v, want cleared", cookies)
	}
}

func TestLogoutHandlerNoCookieStillNoContent(t *testing.T) {
	svc := &stubAuth{}
	h := Logout(svc)
	r := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/center/http/handlers -run TestLogoutHandler -v`
Expected: FAIL.

- [ ] **Step 3: Append implementation to auth.go**

```go
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
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/center/http/handlers -run TestLogoutHandler -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/center/http/handlers/auth.go internal/center/http/handlers/auth_test.go
git commit -m "Add auth logout handler"
```

---

### Task 12: Me handler (returns current user info)

**Files:**
- Modify: `internal/center/http/handlers/auth.go`
- Modify: `internal/center/http/handlers/auth_test.go`

- [ ] **Step 1: Write the failing test (append)**

```go
func TestMeHandlerSuccess(t *testing.T) {
	svc := &stubAuth{
		touchSess: auth.Session{SessionID: "abc", UserID: "u1", ExpiresAt: time.Now().Add(time.Hour)},
		touchUser: auth.User{UserID: "u1", Username: "admin", Role: auth.RoleAdmin, DisplayName: "管理员"},
	}
	h := Me(svc)

	r := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "abc"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp loginResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.UserID != "u1" || resp.Username != "admin" || resp.Role != auth.RoleAdmin {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestMeHandlerUnauthenticated(t *testing.T) {
	svc := &stubAuth{touchErr: auth.ErrSessionNotFound}
	h := Me(svc)
	r := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/center/http/handlers -run TestMeHandler -v`
Expected: FAIL.

- [ ] **Step 3: Append implementation to auth.go**

```go
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
		writeJSON(w, http.StatusOK, loginResponse{
			UserID:      u.UserID,
			Username:    u.Username,
			Role:        u.Role,
			DisplayName: u.DisplayName,
		})
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/center/http/handlers -run TestMeHandler -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/center/http/handlers/auth.go internal/center/http/handlers/auth_test.go
git commit -m "Add auth /me handler"
```

---

### Task 13: ChangePassword handler

**Files:**
- Modify: `internal/center/http/handlers/auth.go`
- Modify: `internal/center/http/handlers/auth_test.go`

- [ ] **Step 1: Write the failing test (append)**

```go
func TestChangePasswordSuccess(t *testing.T) {
	svc := &stubAuth{
		touchSess: auth.Session{SessionID: "abc", UserID: "u1", ExpiresAt: time.Now().Add(time.Hour)},
		touchUser: auth.User{UserID: "u1", Username: "admin", Role: auth.RoleAdmin},
	}
	h := ChangePassword(svc)

	body := strings.NewReader(`{"old_password":"correct-horse-battery","new_password":"new-correct-horse-battery"}`)
	r := httptest.NewRequest(http.MethodPut, "/api/auth/password", body)
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "abc"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
}

func TestChangePasswordWrongOld(t *testing.T) {
	svc := &stubAuth{
		touchSess: auth.Session{SessionID: "abc", UserID: "u1", ExpiresAt: time.Now().Add(time.Hour)},
		touchUser: auth.User{UserID: "u1", Username: "admin", Role: auth.RoleAdmin},
		chgErr:    auth.ErrInvalidCredentials,
	}
	h := ChangePassword(svc)
	body := strings.NewReader(`{"old_password":"wrong","new_password":"new-correct-horse-battery"}`)
	r := httptest.NewRequest(http.MethodPut, "/api/auth/password", body)
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "abc"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestChangePasswordTooShort(t *testing.T) {
	svc := &stubAuth{
		touchSess: auth.Session{SessionID: "abc", UserID: "u1", ExpiresAt: time.Now().Add(time.Hour)},
		touchUser: auth.User{UserID: "u1", Username: "admin", Role: auth.RoleAdmin},
		chgErr:    auth.ErrPasswordTooShort,
	}
	h := ChangePassword(svc)
	body := strings.NewReader(`{"old_password":"correct-horse-battery","new_password":"abc"}`)
	r := httptest.NewRequest(http.MethodPut, "/api/auth/password", body)
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "abc"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/center/http/handlers -run TestChangePassword -v`
Expected: FAIL.

- [ ] **Step 3: Append implementation to auth.go**

```go
type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

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
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/center/http/handlers -run TestChangePassword -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/center/http/handlers/auth.go internal/center/http/handlers/auth_test.go
git commit -m "Add auth change-password handler"
```

---

### Task 14: RequireSession middleware (`http/middleware.go`)

**Files:**
- Create: `internal/center/http/middleware.go`
- Test: `internal/center/http/middleware_test.go`

- [ ] **Step 1: Write the failing test**

```go
package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"houfeng/internal/center/auth"
)

type stubMW struct {
	sess auth.Session
	user auth.User
	err  error
}

func (s *stubMW) Login(_ context.Context, _, _, _, _ string) (auth.Session, error) { return s.sess, nil }
func (s *stubMW) Logout(_ context.Context, _ string) error                          { return nil }
func (s *stubMW) Touch(_ context.Context, _ string) (auth.Session, error)           { return s.sess, s.err }
func (s *stubMW) UserBySession(_ context.Context, _ string) (auth.User, error)      { return s.user, s.err }
func (s *stubMW) ChangePassword(_ context.Context, _, _, _ string) error            { return nil }

func TestRequireSessionAllowsAuthenticated(t *testing.T) {
	svc := &stubMW{
		sess: auth.Session{SessionID: "abc", UserID: "u1", ExpiresAt: time.Now().Add(time.Hour)},
		user: auth.User{UserID: "u1"},
	}
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		uid, _ := UserIDFromContext(r.Context())
		if uid != "u1" {
			t.Fatalf("ctx user_id = %q, want u1", uid)
		}
		w.WriteHeader(http.StatusOK)
	})
	mw := RequireSession(svc)(inner)

	r := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "abc"})
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
	svc := &stubMW{err: auth.ErrSessionNotFound}
	mw := RequireSession(svc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	svc := &stubMW{err: auth.ErrSessionExpired}
	mw := RequireSession(svc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/center/http -run TestRequireSession -v`
Expected: FAIL.

- [ ] **Step 3: Write the implementation**

Create `internal/center/http/middleware.go`:

```go
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

func UserIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxKeyUserID).(string)
	return v, ok
}

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
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/center/http -run TestRequireSession -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/center/http/middleware.go internal/center/http/middleware_test.go
git commit -m "Add RequireSession middleware"
```

---

### Task 15: Session cleanup worker (`auth/cleanup.go`)

**Files:**
- Create: `internal/center/auth/cleanup.go`
- Test: `internal/center/auth/cleanup_test.go`

The worker satisfies the existing `centerapp.Worker` interface (`Run(context.Context) error`). Pattern mirrors `internal/center/retention/worker.go`. Read that file first if details are needed.

- [ ] **Step 1: Write the failing test**

```go
package auth

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

type countingSessions struct {
	*fakeSessions
	calls atomic.Int32
}

func (c *countingSessions) DeleteExpiredBefore(ctx context.Context, cutoff time.Time) (int, error) {
	c.calls.Add(1)
	return c.fakeSessions.DeleteExpiredBefore(ctx, cutoff)
}

func TestSessionCleanupWorkerCallsDeleteExpired(t *testing.T) {
	store := &countingSessions{fakeSessions: newFakeSessions()}
	worker := NewSessionCleanupWorker(store, slog.Default(), 50*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := worker.Run(ctx)
	if !errors.Is(err, context.DeadlineExceeded) && err != nil {
		t.Fatalf("Run = %v, want nil or DeadlineExceeded", err)
	}
	if store.calls.Load() < 2 {
		t.Fatalf("DeleteExpiredBefore called %d times, want >= 2", store.calls.Load())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/center/auth -run TestSessionCleanupWorker -v`
Expected: FAIL.

- [ ] **Step 3: Write the implementation**

```go
package auth

import (
	"context"
	"log/slog"
	"time"
)

const DefaultSessionCleanupInterval = time.Hour

type SessionCleanupWorker struct {
	sessions SessionRepository
	logger   *slog.Logger
	interval time.Duration
}

func NewSessionCleanupWorker(sessions SessionRepository, logger *slog.Logger, interval time.Duration) *SessionCleanupWorker {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = DefaultSessionCleanupInterval
	}
	return &SessionCleanupWorker{sessions: sessions, logger: logger, interval: interval}
}

func (w *SessionCleanupWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	w.sweep(ctx)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			w.sweep(ctx)
		}
	}
}

func (w *SessionCleanupWorker) sweep(ctx context.Context) {
	n, err := w.sessions.DeleteExpiredBefore(ctx, time.Now().UTC())
	if err != nil {
		w.logger.Error("session sweep", "error", err)
		return
	}
	if n > 0 {
		w.logger.Info("session sweep", "deleted", n)
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/center/auth -run TestSessionCleanupWorker -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/center/auth/cleanup.go internal/center/auth/cleanup_test.go
git commit -m "Add session cleanup worker"
```

---

### Task 16: Initial user seed (`auth/seed.go`)

**Files:**
- Create: `internal/center/auth/seed.go`
- Test: `internal/center/auth/seed_test.go`

- [ ] **Step 1: Write the failing test**

```go
package auth

import (
	"context"
	"errors"
	"testing"
)

func TestSeedInitialUserCreatesWhenEmpty(t *testing.T) {
	users := newFakeUsers()
	err := SeedInitialUser(context.Background(), users, "admin", "correct-horse-battery", "管理员", staticNow())
	if err != nil {
		t.Fatalf("SeedInitialUser: %v", err)
	}
	if n, _ := users.CountUsers(context.Background()); n != 1 {
		t.Fatalf("user count = %d, want 1", n)
	}
}

func TestSeedInitialUserSkipWhenNonEmpty(t *testing.T) {
	users := newFakeUsers()
	_ = users.Create(context.Background(), User{UserID: "existing", Username: "someone", PasswordHash: "x", Role: RoleAdmin})

	err := SeedInitialUser(context.Background(), users, "admin", "correct-horse-battery", "管理员", staticNow())
	if err != nil {
		t.Fatalf("SeedInitialUser: %v", err)
	}
	if n, _ := users.CountUsers(context.Background()); n != 1 {
		t.Fatalf("user count = %d, want 1 (skip)", n)
	}
}

func TestSeedInitialUserRejectsBadInputs(t *testing.T) {
	users := newFakeUsers()
	cases := []struct {
		name, user, pass string
		wantErr          error
	}{
		{"empty username", "", "correct-horse-battery", ErrUsernameInvalid},
		{"short password", "admin", "abc", ErrPasswordTooShort},
		{"long password", "admin", string(make([]byte, MaxPasswordLength+1)), ErrPasswordTooLong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := SeedInitialUser(context.Background(), users, tc.user, tc.pass, "管理员", staticNow())
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
```

Add helper inside the same file:

```go
func staticNow() func() time.Time {
	t := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return t }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/center/auth -run TestSeedInitialUser -v`
Expected: FAIL.

- [ ] **Step 3: Write the implementation**

```go
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

func SeedInitialUser(ctx context.Context, users UserRepository, username, password, displayName string, now func() time.Time) error {
	username = strings.TrimSpace(username)
	if len(username) < MinUsernameLength || len(username) > MaxUsernameLength {
		return ErrUsernameInvalid
	}
	if len(password) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	if len(password) > MaxPasswordLength {
		return ErrPasswordTooLong
	}
	count, err := users.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return nil
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	id, err := newUserID()
	if err != nil {
		return err
	}
	t := now().UTC()
	if displayName == "" {
		displayName = username
	}
	return users.Create(ctx, User{
		UserID:            id,
		Username:          username,
		PasswordHash:      hash,
		DisplayName:       displayName,
		Role:              RoleAdmin,
		CreatedAt:         t,
		PasswordChangedAt: t,
	})
}

func newUserID() (string, error) {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	return "usr_" + hex.EncodeToString(buf[:]), nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/center/auth -run TestSeedInitialUser -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/center/auth/seed.go internal/center/auth/seed_test.go
git commit -m "Add initial user seed"
```

---

### Task 17: Config additions

**Files:**
- Modify: `internal/center/config/config.go`
- Modify: `internal/center/config/config_test.go`

- [ ] **Step 1: Write the failing test (append)**

```go
func TestLoadCenterConfigAuthFields(t *testing.T) {
	t.Setenv("HOUFENG_HTTP_ADDR", ":9090")
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://x")
	t.Setenv("HOUFENG_INITIAL_USERNAME", "admin")
	t.Setenv("HOUFENG_INITIAL_PASSWORD", "correct-horse-battery")
	t.Setenv("HOUFENG_INITIAL_DISPLAY_NAME", "管理员")
	t.Setenv("HOUFENG_SESSION_TTL", "168h")

	cfg, err := LoadCenterConfig()
	if err != nil {
		t.Fatalf("LoadCenterConfig: %v", err)
	}
	if cfg.InitialUsername != "admin" {
		t.Fatalf("InitialUsername = %q", cfg.InitialUsername)
	}
	if cfg.InitialPassword != "correct-horse-battery" {
		t.Fatalf("InitialPassword wrong")
	}
	if cfg.InitialDisplayName != "管理员" {
		t.Fatalf("InitialDisplayName = %q", cfg.InitialDisplayName)
	}
	if cfg.SessionTTL != 168*time.Hour {
		t.Fatalf("SessionTTL = %v", cfg.SessionTTL)
	}
}

func TestLoadCenterConfigAuthMissingFails(t *testing.T) {
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://x")
	// HOUFENG_INITIAL_USERNAME / _PASSWORD intentionally unset
	t.Setenv("HOUFENG_INITIAL_USERNAME", "")
	t.Setenv("HOUFENG_INITIAL_PASSWORD", "")
	_, err := LoadCenterConfig()
	if err == nil {
		t.Fatal("expected error when initial credentials missing")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/center/config -run TestLoadCenterConfigAuth -v`
Expected: FAIL.

- [ ] **Step 3: Update `config.go`**

Add to `CenterConfig`:

```go
InitialUsername    string
InitialPassword    string
InitialDisplayName string
SessionTTL         time.Duration
```

Add load logic in `LoadCenterConfig`:

```go
initialUsername, err := requiredEnv("HOUFENG_INITIAL_USERNAME")
if err != nil {
    return CenterConfig{}, err
}
initialPassword, err := requiredEnv("HOUFENG_INITIAL_PASSWORD")
if err != nil {
    return CenterConfig{}, err
}
initialDisplayName := strings.TrimSpace(os.Getenv("HOUFENG_INITIAL_DISPLAY_NAME"))
sessionTTL, err := durationEnvOrDefault("HOUFENG_SESSION_TTL", 7*24*time.Hour)
if err != nil {
    return CenterConfig{}, err
}
```

Then add the fields to the returned struct:

```go
InitialUsername:    initialUsername,
InitialPassword:    initialPassword,
InitialDisplayName: initialDisplayName,
SessionTTL:         sessionTTL,
```

> Cookie `Secure` attribute is intentionally not configurable in V1.x — the security model relies on the reverse proxy terminating HTTPS plus HttpOnly + SameSite=Lax. Document the proxy requirement in Task 21.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/center/config -run TestLoadCenterConfigAuth -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/center/config/config.go internal/center/config/config_test.go
git commit -m "Add auth config (initial user seed + session TTL)"
```

---

### Task 18: Router wiring

**Files:**
- Modify: `internal/center/http/router.go`
- Modify: `internal/center/http/router_test.go`

- [ ] **Step 1: Write the failing test (append to router_test.go)**

```go
func TestRouterAuthRoutesPublic(t *testing.T) {
	opts := RouterOptions{
		Version:                "test",
		AuthLoginHandler:       handlers.Login(&fakeAuthSvcRouter{}),
		AuthLogoutHandler:      handlers.Logout(&fakeAuthSvcRouter{}),
	}
	mux := New(opts)

	for _, p := range []string{"/api/auth/login", "/api/auth/logout"} {
		r := httptest.NewRequest(http.MethodPost, p, strings.NewReader("{}"))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code == http.StatusNotFound {
			t.Fatalf("%s 404", p)
		}
	}
}

func TestRouterProtectsExistingRoutes(t *testing.T) {
	opts := RouterOptions{
		Version:           "test",
		AuthMiddleware:    func(next http.Handler) http.Handler { return blockingMiddleware(next) },
		DashboardHandler:  http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }),
	}
	mux := New(opts)
	r := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (middleware should block)", w.Code)
	}
}
```

(Add `blockingMiddleware` and `fakeAuthSvcRouter` helpers in router_test.go.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/center/http -run TestRouterAuth -v`
Expected: FAIL.

- [ ] **Step 3: Update `router.go`**

Extend `RouterOptions`:

```go
AuthLoginHandler         stdhttp.Handler
AuthLogoutHandler        stdhttp.Handler
AuthMeHandler            stdhttp.Handler
AuthChangePasswordHandler stdhttp.Handler
AuthMiddleware           func(stdhttp.Handler) stdhttp.Handler
```

In `New(opts)`, register auth routes (no middleware):

```go
if opts.AuthLoginHandler != nil {
    mux.Handle("/api/auth/login", opts.AuthLoginHandler)
}
if opts.AuthLogoutHandler != nil {
    mux.Handle("/api/auth/logout", opts.AuthLogoutHandler)
}
if opts.AuthMeHandler != nil {
    mux.Handle("/api/auth/me", opts.AuthMeHandler)
}
if opts.AuthChangePasswordHandler != nil {
    mux.Handle("/api/auth/password", opts.AuthChangePasswordHandler)
}
```

Then introduce a `protect` helper:

```go
protect := func(h stdhttp.Handler) stdhttp.Handler {
    if opts.AuthMiddleware == nil || h == nil {
        return h
    }
    return opts.AuthMiddleware(h)
}
```

Wrap every existing user-facing handler at the point of registration. **Do NOT** wrap:
- `/api/healthz`
- `/api/agent/*` (already separate)
- `/api/auth/*`
- the SPA static handler at `/`

E.g.:

```go
mux.Handle("/api/dashboard", protect(opts.DashboardHandler))
mux.Handle("/api/events",    protect(opts.EventsHandler))
// ...repeat for incidents, settings, nodes, /api/nodes/, targets, /api/targets/
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/center/http/... -v`
Expected: PASS (all existing router tests stay green; new tests pass).

- [ ] **Step 5: Commit**

```bash
git add internal/center/http/router.go internal/center/http/router_test.go
git commit -m "Wire auth routes and middleware into router"
```

---

### Task 19: Bootstrap wiring

**Files:**
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

- [ ] **Step 1: Update `bootstrap.go`**

In `bootstrapCenter`, after migrations are applied and repos are constructed:

```go
userRepo := store.NewPostgresUserRepository(db.Pool())
sessionRepo := store.NewPostgresSessionRepository(db.Pool())

if err := auth.SeedInitialUser(ctx, userRepo, cfg.InitialUsername, cfg.InitialPassword, cfg.InitialDisplayName, time.Now); err != nil {
    db.Close()
    return nil, nil, fmt.Errorf("seed initial user: %w", err)
}

authSvc := auth.New(userRepo, sessionRepo, auth.Options{
    SessionTTL: cfg.SessionTTL,
    Now:        time.Now,
})
sessionCleanup := auth.NewSessionCleanupWorker(sessionRepo, slog.Default(), auth.DefaultSessionCleanupInterval)
authMW := centerhttp.RequireSession(authSvc)
```

Pass new fields into `RouterOptions`:

```go
AuthLoginHandler:          handlers.Login(authSvc),
AuthLogoutHandler:         handlers.Logout(authSvc),
AuthMeHandler:             handlers.Me(authSvc),
AuthChangePasswordHandler: handlers.ChangePassword(authSvc),
AuthMiddleware:            authMW,
```

Pass `sessionCleanup` into `centerapp.New(cfg.HTTPAddr, router, incidentSvc, retentionWorker, sessionCleanup)` (the constructor accepts variadic `Worker`).

- [ ] **Step 2: Update `bootstrap_test.go`**

Adjust any existing fixtures that call `bootstrapCenter` to provide non-empty `InitialUsername`/`InitialPassword`. Add a smoke assertion that the auth handlers and middleware are not nil.

- [ ] **Step 3: Run tests**

Run: `go test ./cmd/houfeng-center -v && make verify-go`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add cmd/houfeng-center/bootstrap.go cmd/houfeng-center/bootstrap_test.go
git commit -m "Wire auth service, seed, middleware, and cleanup worker"
```

---

### Task 20: End-to-end smoke test (live Postgres optional)

**Files:**
- Create: `internal/center/http/auth_e2e_test.go`

- [ ] **Step 1: Write the test**

Use the existing test pool helper (`requireTestPool` if cross-package; otherwise duplicate the bootstrap helper). The test:

1. Sets env vars and calls `bootstrapCenter`.
2. POSTs to `/api/auth/login` with the seeded credentials → expects 200 + cookie.
3. Re-uses the cookie to GET `/api/dashboard` → expects 200.
4. Drops the cookie, GET `/api/dashboard` → expects 401.
5. POSTs to `/api/auth/logout`. Re-using the same cookie GET `/api/dashboard` → 401.

Skip with `t.Skip` when test pool is not available, exactly like `internal/center/store/nodes_test.go` does.

- [ ] **Step 2: Run**

Run: `go test ./internal/center/http -run TestAuthEndToEnd -v`
Expected: PASS or SKIP (when no test DB).

- [ ] **Step 3: Commit**

```bash
git add internal/center/http/auth_e2e_test.go
git commit -m "Add auth end-to-end smoke test"
```

---

### Task 21: Update env example and deploy docs

**Files:**
- Modify: `.env.example`
- Modify: `docs/deploy/local-and-systemd.md`

- [ ] **Step 1: Update `.env.example`**

Append:

```
# Initial user seed (required on first startup; ignored if users table non-empty)
HOUFENG_INITIAL_USERNAME=admin
HOUFENG_INITIAL_PASSWORD=replace-me-with-a-real-password
HOUFENG_INITIAL_DISPLAY_NAME=

# Session TTL (rolling; refreshed on each request)
HOUFENG_SESSION_TTL=168h
```

- [ ] **Step 2: Update `docs/deploy/local-and-systemd.md`**

Add a section "Authentication" describing:

- The four new env vars (`HOUFENG_INITIAL_USERNAME`, `HOUFENG_INITIAL_PASSWORD`, `HOUFENG_INITIAL_DISPLAY_NAME`, `HOUFENG_SESSION_TTL`)
- That `HOUFENG_INITIAL_USERNAME` / `_PASSWORD` are required at first startup; on subsequent startups (when `users` already has rows) they are ignored
- That production deployments **must** terminate HTTPS at a reverse proxy (Caddy/Nginx). The session cookie is HttpOnly + SameSite=Lax but does not carry the `Secure` attribute, so an HTTPS-terminating proxy is required to prevent cookie leakage on plain HTTP
- That `/api/healthz` and `/api/agent/*` remain unauthenticated; everything else under `/api/*` requires a session cookie

- [ ] **Step 3: Run final verify**

Run: `make verify`
Expected: green (Go + web).

- [ ] **Step 4: Commit**

```bash
git add .env.example docs/deploy/local-and-systemd.md
git commit -m "Document auth env vars and protected route surface"
```

---

### Task 22: Mark gap-checklist (V1.x scope-add)

**Files:**
- Modify: `docs/release/v1-gap-checklist.md`

- [ ] **Step 1: Append a new section "Authentication (V1.x scope add)"**

Two rows:

| Area | Status | Evidence |
| --- | --- | --- |
| Username + password login (方案 2) | Closed | `internal/center/auth/`, `internal/center/store/users.go`, `internal/center/store/sessions.go`, migration `0010` |
| All non-agent / non-health API protected by session cookie | Closed | `internal/center/http/middleware.go`, `internal/center/http/router.go` |

- [ ] **Step 2: Commit**

```bash
git add docs/release/v1-gap-checklist.md
git commit -m "Document V1.x auth in gap checklist"
```

---

## Acceptance criteria

- `make verify-go` green
- `go test ./internal/center/auth/... ./internal/center/store/... ./internal/center/http/... ./cmd/houfeng-center/...` green
- Live smoke (manual or live-DB CI step):
  ```bash
  curl -sf -X POST -d '{"username":"admin","password":"<seed>"}' \
       -H 'Content-Type: application/json' http://127.0.0.1:8080/api/auth/login -c /tmp/c
  curl -sf -b /tmp/c http://127.0.0.1:8080/api/dashboard       # 200
  curl -sf    http://127.0.0.1:8080/api/dashboard               # 401
  curl -sf -X POST -b /tmp/c http://127.0.0.1:8080/api/auth/logout
  curl -sf -b /tmp/c http://127.0.0.1:8080/api/dashboard       # 401
  ```
- `/api/healthz` reachable without auth.
- Re-running the binary against the same DB does not re-seed (count check inside `SeedInitialUser`).

## Cross-plan handoff

Plan 2 (frontend foundation) **depends on Plan 1 being merged**. Plan 2 will:

- Read `loginResponse` JSON shape (Task 10) to type the frontend's auth client
- Use `/api/auth/me` for SPA boot-time identity check
- Treat 401 from any `/api/*` as "redirect to /login"
- Display `display_name || username` plus `role` localized to "管理员" in the user chip
