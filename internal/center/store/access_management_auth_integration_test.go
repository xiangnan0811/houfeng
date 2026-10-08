package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"houfeng/internal/center/auth"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
)

func TestPostgresIntegrationAccessManagementAuthDisabledSessionLifecycle(t *testing.T) {
	base := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	fixture := newAuthPostgresFixture(t, func() time.Time { return base }, time.Hour, "access_disabled_lifecycle")
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	current := fixture.login(t, authIntegrationPassword)
	disabledAt := base.Add(time.Minute)
	if _, err := fixture.pool.Exec(ctx, `update users set disabled_at = $2 where user_id = $1`, fixture.userID, disabledAt); err != nil {
		t.Fatalf("disable fixture user: %v", err)
	}
	if _, err := fixture.service.Login(ctx, fixture.username, authIntegrationPassword, "ua", "203.0.113.11"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("disabled login = %v, want ErrInvalidCredentials", err)
	}
	if err := fixture.service.ValidateSession(ctx, current.SessionID); !errors.Is(err, auth.ErrSessionExpired) {
		t.Fatalf("disabled ValidateSession = %v, want ErrSessionExpired", err)
	}
	if got := authPostgresSessionCount(t, ctx, fixture.pool, fixture.userID); got != 1 {
		t.Fatalf("ValidateSession session count = %d, want 1 (side-effect-free)", got)
	}
	if _, err := fixture.service.Touch(ctx, current.SessionID); !errors.Is(err, auth.ErrSessionExpired) {
		t.Fatalf("disabled Touch = %v, want ErrSessionExpired", err)
	}
	if got := authPostgresSessionCount(t, ctx, fixture.pool, fixture.userID); got != 0 {
		t.Fatalf("disabled Touch left %d session rows, want 0", got)
	}

	if _, err := fixture.pool.Exec(ctx, `update users set disabled_at = null where user_id = $1`, fixture.userID); err != nil {
		t.Fatalf("enable fixture user: %v", err)
	}
	if _, err := fixture.service.Touch(ctx, current.SessionID); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("old disabled session touch = %v, want ErrSessionNotFound", err)
	}
}
func TestPostgresIntegrationAccessManagementAuthFiltersDisabledCollaborationActor(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	base := time.Date(2026, time.October, 8, 12, 15, 0, 0, time.UTC)
	userID, err := auth.NewUserID()
	if err != nil {
		t.Fatalf("NewUserID: %v", err)
	}
	hash, err := auth.HashPasswordWithCost(authIntegrationPassword, bcrypt.MinCost)
	if err != nil {
		t.Fatalf("HashPasswordWithCost: %v", err)
	}
	if err := NewPostgresUserRepository(pool).Create(ctx, auth.User{
		UserID: userID, Username: "collaboration-recipient", PasswordHash: hash,
		DisplayName: "Collaboration recipient", Role: auth.RoleAdmin,
		CreatedAt: base, PasswordChangedAt: base,
	}); err != nil {
		t.Fatalf("create collaboration recipient: %v", err)
	}
	reader := NewPostgresCollaborationMembershipReader()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin active membership read: %v", err)
	}
	actor, err := reader.ReadMemberActor(ctx, tx, recordauth.ProjectIDDefault, userID)
	_ = tx.Rollback(ctx)
	if err != nil {
		t.Fatalf("active ReadMemberActor: %v", err)
	}
	if actor.UserID != userID || actor.Role != recordauth.RoleProjectAdmin {
		t.Fatalf("active actor = %#v, want persisted admin actor", actor)
	}
	disabledAt := base.Add(time.Minute)
	if _, err := pool.Exec(ctx, `update users set disabled_at = $2 where user_id = $1`, userID, disabledAt); err != nil {
		t.Fatalf("disable collaboration recipient: %v", err)
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin disabled membership read: %v", err)
	}
	_, err = reader.ReadMemberActor(ctx, tx, recordauth.ProjectIDDefault, userID)
	_ = tx.Rollback(ctx)
	if !errors.Is(err, recordcollaboration.ErrMembershipDenied) {
		t.Fatalf("disabled ReadMemberActor = %v, want ErrMembershipDenied", err)
	}
}
func TestPostgresIntegrationAccessManagementAuthManagementCapabilitySnapshot(t *testing.T) {
	base := time.Date(2026, time.October, 8, 12, 30, 0, 0, time.UTC)
	fixture := newAuthPostgresFixture(t, func() time.Time { return base }, time.Hour, "access_capability_snapshot")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := fixture.pool.Exec(ctx, `update users set is_supervisor = true where user_id = $1`, fixture.userID); err != nil {
		t.Fatalf("promote fixture supervisor: %v", err)
	}
	session := fixture.login(t, authIntegrationPassword)
	if !session.ManagementCapabilities.Access {
		t.Fatalf("login management capabilities = %+v, want access=true", session.ManagementCapabilities)
	}
	if _, err := fixture.pool.Exec(ctx, `update users set is_supervisor = false where user_id = $1`, fixture.userID); err != nil {
		t.Fatalf("remove supervisor flag: %v", err)
	}
	if err := fixture.service.ValidateSession(ctx, session.SessionID); err != nil {
		t.Fatalf("ValidateSession after capability-only change: %v", err)
	}
}

func TestPostgresIntegrationAccessManagementAuthDisableBeforeLoginInsert(t *testing.T) {
	base := time.Date(2026, time.October, 8, 13, 0, 0, 0, time.UTC)
	fixture := newAuthPostgresFixture(t, func() time.Time { return base }, time.Hour, "access_disable_login_barrier")
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	holder := lockAuthUser(t, ctx, fixture.pool, fixture.userID)
	loginDone := make(chan error, 1)
	go func() {
		_, err := fixture.service.Login(ctx, fixture.username, authIntegrationPassword, "ua", "203.0.113.12")
		loginDone <- err
	}()
	if err := waitForAuthLockWaiter(ctx, fixture.pool); err != nil {
		_ = holder.Rollback(ctx)
		t.Fatalf("wait for login user lock: %v", err)
	}
	if _, err := holder.Exec(ctx, `update users set disabled_at = $2 where user_id = $1`, fixture.userID, base.Add(time.Minute)); err != nil {
		_ = holder.Rollback(ctx)
		t.Fatalf("disable user while login waits: %v", err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("commit disable while login waits: %v", err)
	}
	if err := <-loginDone; !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("login after pre-insert disable = %v, want ErrInvalidCredentials", err)
	}
	if got := authPostgresSessionCount(t, ctx, fixture.pool, fixture.userID); got != 0 {
		t.Fatalf("pre-insert disable created %d session rows", got)
	}

	if _, err := fixture.pool.Exec(ctx, `update users set disabled_at = null where user_id = $1`, fixture.userID); err != nil {
		t.Fatalf("enable fixture user: %v", err)
	}
	if _, err := fixture.service.Login(ctx, fixture.username, authIntegrationPassword, "ua", "203.0.113.12"); err != nil {
		t.Fatalf("login after enable: %v", err)
	}
}

func TestPostgresIntegrationAccessManagementAuthConcurrentSeedHasOneSupervisor(t *testing.T) {
	ctx := context.Background()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	delegate := NewPostgresUserRepository(pool)
	barrier := newSeedCountBarrier(2)
	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for _, seed := range []struct {
		username string
		password string
	}{
		{username: "concurrent-supervisor-a", password: "Seed-Alpha-2026!"},
		{username: "concurrent-supervisor-b", password: "Seed-Bravo-2026!"},
	} {
		seed := seed
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			users := &seedCountBarrierUsers{delegate: delegate, barrier: barrier}
			results <- auth.SeedInitialUserWithOptions(ctx, users, auth.SeedInitialUserOptions{
				Username:           seed.username,
				Password:           seed.password,
				DisplayName:        "Initial supervisor",
				Now:                time.Now,
				PasswordBcryptCost: bcrypt.MinCost,
			})
		}()
	}
	close(start)
	select {
	case <-barrier.ready:
	case <-time.After(8 * time.Second):
		t.Fatal("timed out waiting for concurrent seed CountUsers barrier")
	}
	close(barrier.release)
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent seed = %v, want both calls to converge successfully", err)
		}
	}

	var usersCount, supervisors int
	if err := pool.QueryRow(ctx, `select count(*) from users`).Scan(&usersCount); err != nil {
		t.Fatalf("count users after concurrent seed: %v", err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from users where is_supervisor and role = $1 and disabled_at is null`, auth.RoleAdmin).Scan(&supervisors); err != nil {
		t.Fatalf("count supervisors after concurrent seed: %v", err)
	}
	if usersCount != 1 || supervisors != 1 {
		t.Fatalf("seeded users/supervisors = %d/%d, want 1/1", usersCount, supervisors)
	}
	var username, role string
	var disabledAt *time.Time
	if err := pool.QueryRow(ctx, `
		select username, role, disabled_at
		from users
		where is_supervisor`).Scan(&username, &role, &disabledAt); err != nil {
		t.Fatalf("read seeded supervisor: %v", err)
	}
	if (username != "concurrent-supervisor-a" && username != "concurrent-supervisor-b") ||
		role != auth.RoleAdmin || disabledAt != nil {
		t.Fatalf("seeded authority username=%q role=%q disabled=%t, want active admin supervisor", username, role, disabledAt != nil)
	}
}

type seedCountBarrier struct {
	mu           sync.Mutex
	participants int
	waiting      int
	ready        chan struct{}
	release      chan struct{}
}

func newSeedCountBarrier(participants int) *seedCountBarrier {
	if participants < 1 {
		participants = 1
	}
	return &seedCountBarrier{participants: participants, ready: make(chan struct{}), release: make(chan struct{})}
}

func (b *seedCountBarrier) wait() {
	b.mu.Lock()
	b.waiting++
	if b.waiting == b.participants {
		close(b.ready)
	}
	b.mu.Unlock()
	<-b.release
}

type seedCountBarrierUsers struct {
	delegate auth.UserRepository
	barrier  *seedCountBarrier
}

func (r *seedCountBarrierUsers) Create(ctx context.Context, user auth.User) error {
	return r.delegate.Create(ctx, user)
}

func (r *seedCountBarrierUsers) FindByUsername(ctx context.Context, username string) (auth.User, error) {
	return r.delegate.FindByUsername(ctx, username)
}

func (r *seedCountBarrierUsers) FindByID(ctx context.Context, userID string) (auth.User, error) {
	return r.delegate.FindByID(ctx, userID)
}

func (r *seedCountBarrierUsers) CountUsers(ctx context.Context) (int, error) {
	count, err := r.delegate.CountUsers(ctx)
	if err != nil {
		return 0, err
	}
	r.barrier.wait()
	return count, nil
}
