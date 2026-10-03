package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/auth"
)

const authIntegrationPassword = "correct-horse-battery"
const authReplacementPassword = "Rotated-Credential-2026!"
const authWinnerPasswordOne = "Winner-Alpha-2026!"
const authWinnerPasswordTwo = "Winner-Bravo-2026!"
const authIntegrationHMACKey = "0123456789abcdef0123456789abcdef"

type authPostgresFixture struct {
	pool     *pgxpool.Pool
	users    *PostgresUserRepository
	sessions *PostgresSessionRepository
	service  *auth.Service
	userID   string
	username string
}

func newAuthPostgresFixture(t *testing.T, now func() time.Time, ttl time.Duration, suffix string) *authPostgresFixture {
	t.Helper()
	ctx := context.Background()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	userID := "usr_auth_" + suffix
	username := "auth_" + suffix
	createdAt := now().UTC().Truncate(time.Microsecond)
	hash, err := auth.HashPassword(authIntegrationPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into users (user_id, username, password_hash, display_name, role, created_at, password_changed_at)
		values ($1, $2, $3, $4, $5, $6, $6)`, userID, username, hash, "Auth integration", auth.RoleAdmin, createdAt); err != nil {
		t.Fatalf("insert auth integration user: %v", err)
	}
	users := NewPostgresUserRepository(pool)
	sessions, err := NewPostgresSessionRepository(pool, []byte(authIntegrationHMACKey))
	if err != nil {
		t.Fatalf("NewPostgresSessionRepository: %v", err)
	}
	service := auth.New(users, sessions, auth.Options{SessionTTL: ttl, Now: now})
	return &authPostgresFixture{pool: pool, users: users, sessions: sessions, service: service, userID: userID, username: username}
}

func (f *authPostgresFixture) login(t *testing.T, password string) auth.Session {
	t.Helper()
	session, err := f.service.Login(context.Background(), f.username, password, "auth-integration", "203.0.113.10")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	return session
}

func authPostgresUserHash(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string) string {
	t.Helper()
	var hash string
	if err := pool.QueryRow(ctx, `select password_hash from users where user_id = $1`, userID).Scan(&hash); err != nil {
		t.Fatalf("query password hash: %v", err)
	}
	return hash
}

func authPostgresSessionCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `select count(*)::int from sessions where user_id = $1`, userID).Scan(&count); err != nil {
		t.Fatalf("query session count: %v", err)
	}
	return count
}

func waitForAuthLockWaiter(ctx context.Context, pool *pgxpool.Pool) error {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := pool.QueryRow(ctx, `
			select count(*)
			from pg_stat_activity
			where datname = current_database()
			  and pid <> pg_backend_pid()
			  and wait_event_type = 'Lock'
			  and state = 'active'`).Scan(&waiting); err != nil {
			return err
		}
		if waiting > 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	return errors.New("timed out waiting for auth row lock waiter")
}

func waitForAuthSessionInsertAdvisoryWaiter(ctx context.Context, pool *pgxpool.Pool) error {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := pool.QueryRow(ctx, `
			select count(*)
			from pg_stat_activity
			where datname = current_database()
			  and pid <> pg_backend_pid()
			  and wait_event_type = 'Lock'
			  and wait_event = 'advisory'
			  and state = 'active'
			  and query like '%insert into sessions%'`).Scan(&waiting); err != nil {
			return err
		}
		if waiting > 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	return errors.New("timed out waiting for auth session insert advisory waiter")
}

func waitForAuthChangeUserLockWaiter(ctx context.Context, pool *pgxpool.Pool) error {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := pool.QueryRow(ctx, `
			select count(*)
			from pg_stat_activity
			where datname = current_database()
			  and pid <> pg_backend_pid()
			  and wait_event_type = 'Lock'
			  and state = 'active'
			  and query like '%password_hash%'
			  and query like '%password_changed_at%'
			  and query like '%for update%'`).Scan(&waiting); err != nil {
			return err
		}
		if waiting > 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	return errors.New("timed out waiting for auth change user lock waiter")
}

func lockAuthUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string) pgx.Tx {
	t.Helper()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatalf("begin auth lock holder: %v", err)
	}
	if err := tx.QueryRow(ctx, `select user_id from users where user_id = $1 for update`, userID).Scan(new(string)); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("lock auth user: %v", err)
	}
	return tx
}

func authInsertAdvisoryKey(userID string) string {
	return "houfeng-auth-insert:" + userID
}

func holdAuthSessionInsertAdvisory(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string) pgx.Tx {
	t.Helper()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatalf("begin auth advisory holder: %v", err)
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1))`, authInsertAdvisoryKey(userID)); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("hold auth insert advisory lock: %v", err)
	}
	return tx
}

func installAuthSessionInsertAdvisoryBlock(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		create function houfeng_auth_test_advisory_insert() returns trigger
		language plpgsql as $$
		begin
			perform pg_advisory_xact_lock(hashtext('houfeng-auth-insert:' || new.user_id));
			return new;
		end;
		$$;
		create trigger houfeng_auth_test_advisory_insert
		before insert on sessions for each row
		execute function houfeng_auth_test_advisory_insert()`)
	if err != nil {
		t.Fatalf("install auth insert advisory barrier: %v", err)
	}
}

func installAuthFailureTrigger(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql); err != nil {
		t.Fatalf("install auth failure trigger: %v", err)
	}
}

func TestPostgresIntegrationAuthPasswordRotationAtomicBoundary(t *testing.T) {
	base := time.Date(2026, time.June, 23, 10, 0, 0, 0, time.UTC)

	t.Run("stale_login_after_committed_change_has_no_session", func(t *testing.T) {
		fixture := newAuthPostgresFixture(t, func() time.Time { return base }, time.Hour, "stale_login")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		newHash, err := auth.HashPassword(authReplacementPassword)
		if err != nil {
			t.Fatalf("HashPassword replacement: %v", err)
		}
		holder := lockAuthUser(t, ctx, fixture.pool, fixture.userID)
		loginDone := make(chan error, 1)
		go func() {
			_, err := fixture.service.Login(ctx, fixture.username, authIntegrationPassword, "ua", "203.0.113.10")
			loginDone <- err
		}()
		if err := waitForAuthLockWaiter(ctx, fixture.pool); err != nil {
			_ = holder.Rollback(ctx)
			t.Fatalf("wait for stale login lock: %v", err)
		}
		changedAt := base.Add(time.Minute)
		if _, err := holder.Exec(ctx, `update users set password_hash = $2, password_changed_at = $3 where user_id = $1`, fixture.userID, newHash, changedAt); err != nil {
			_ = holder.Rollback(ctx)
			t.Fatalf("change password in holder: %v", err)
		}
		if err := holder.Commit(ctx); err != nil {
			t.Fatalf("commit password holder: %v", err)
		}
		if err := <-loginDone; !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("stale login error = %v, want ErrInvalidCredentials", err)
		}
		if got := authPostgresSessionCount(t, ctx, fixture.pool, fixture.userID); got != 0 {
			t.Fatalf("stale login created %d sessions", got)
		}
		if got := authPostgresUserHash(t, ctx, fixture.pool, fixture.userID); got != newHash {
			t.Fatal("committed password change was not retained")
		}
	})

	t.Run("login_then_change_revokes_other_session_and_retains_current", func(t *testing.T) {
		fixture := newAuthPostgresFixture(t, func() time.Time { return base }, time.Hour, "reverse_interleave")
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		current := fixture.login(t, authIntegrationPassword)
		installAuthSessionInsertAdvisoryBlock(t, ctx, fixture.pool)
		advisoryHolder := holdAuthSessionInsertAdvisory(t, ctx, fixture.pool, fixture.userID)
		released := false
		defer func() {
			if !released {
				_ = advisoryHolder.Rollback(context.Background())
			}
		}()
		loginDone := make(chan auth.Session, 1)
		loginErr := make(chan error, 1)
		go func() {
			session, err := fixture.service.Login(ctx, fixture.username, authIntegrationPassword, "other", "203.0.113.11")
			loginDone <- session
			loginErr <- err
		}()
		if err := waitForAuthSessionInsertAdvisoryWaiter(ctx, fixture.pool); err != nil {
			t.Fatalf("wait for login advisory barrier: %v", err)
		}
		changeDone := make(chan error, 1)
		go func() {
			changeDone <- fixture.service.ChangePassword(ctx, fixture.userID, current.SessionID, authIntegrationPassword, authReplacementPassword)
		}()
		if err := waitForAuthChangeUserLockWaiter(ctx, fixture.pool); err != nil {
			t.Fatalf("wait for change user lock: %v", err)
		}
		if err := advisoryHolder.Commit(ctx); err != nil {
			t.Fatalf("release login advisory barrier: %v", err)
		}
		released = true
		if err := <-loginErr; err != nil {
			t.Fatalf("barrier login: %v", err)
		}
		_ = <-loginDone
		if err := <-changeDone; err != nil {
			t.Fatalf("change password: %v", err)
		}
		if _, err := fixture.service.Touch(ctx, current.SessionID); err != nil {
			t.Fatalf("current session after change: %v", err)
		}
		var sessions []string
		rows, err := fixture.pool.Query(ctx, `select session_id_hash from sessions where user_id = $1`, fixture.userID)
		if err != nil {
			t.Fatalf("list sessions: %v", err)
		}
		for rows.Next() {
			var hash string
			if err := rows.Scan(&hash); err != nil {
				rows.Close()
				t.Fatalf("scan sessions: %v", err)
			}
			sessions = append(sessions, hash)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatalf("iterate sessions: %v", err)
		}
		rows.Close()
		if len(sessions) != 1 {
			t.Fatalf("session rows after change = %d, want current only", len(sessions))
		}
		if _, err := fixture.service.Login(ctx, fixture.username, authIntegrationPassword, "", ""); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("old password login = %v, want ErrInvalidCredentials", err)
		}
		if _, err := fixture.service.Login(ctx, fixture.username, authReplacementPassword, "", ""); err != nil {
			t.Fatalf("new password login: %v", err)
		}
	})

	t.Run("same_expected_hash_concurrent_changes_have_one_winner", func(t *testing.T) {
		fixture := newAuthPostgresFixture(t, func() time.Time { return base }, time.Hour, "concurrent_changes")
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		first := fixture.login(t, authIntegrationPassword)
		second := fixture.login(t, authIntegrationPassword)
		start := make(chan struct{})
		results := make(chan struct {
			id  string
			err error
		}, 2)
		var group sync.WaitGroup
		for _, input := range []struct {
			id       string
			password string
		}{
			{id: first.SessionID, password: authWinnerPasswordOne},
			{id: second.SessionID, password: authWinnerPasswordTwo},
		} {
			input := input
			group.Add(1)
			go func() {
				defer group.Done()
				<-start
				err := fixture.service.ChangePassword(ctx, fixture.userID, input.id, authIntegrationPassword, input.password)
				results <- struct {
					id  string
					err error
				}{id: input.id, err: err}
			}()
		}
		close(start)
		group.Wait()
		close(results)
		winnerID := ""
		winnerPassword := ""
		for result := range results {
			if result.err == nil {
				if winnerID != "" {
					t.Fatal("both concurrent password changes succeeded")
				}
				winnerID = result.id
				if result.id == first.SessionID {
					winnerPassword = authWinnerPasswordOne
				} else {
					winnerPassword = authWinnerPasswordTwo
				}
				continue
			}
			if !errors.Is(result.err, auth.ErrInvalidCredentials) {
				t.Fatalf("losing password change error = %v, want ErrInvalidCredentials", result.err)
			}
		}
		if winnerID == "" {
			t.Fatal("no concurrent password change won")
		}
		if _, err := fixture.service.Touch(ctx, winnerID); err != nil {
			t.Fatalf("winner session touch: %v", err)
		}
		loserID := second.SessionID
		if loserID == winnerID {
			loserID = first.SessionID
		}
		if _, err := fixture.service.Touch(ctx, loserID); !errors.Is(err, auth.ErrSessionNotFound) {
			t.Fatalf("loser session touch = %v, want ErrSessionNotFound", err)
		}
		if _, err := fixture.service.Login(ctx, fixture.username, winnerPassword, "", ""); err != nil {
			t.Fatalf("winner password login: %v", err)
		}
		if _, err := fixture.service.Login(ctx, fixture.username, authIntegrationPassword, "", ""); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("old password login = %v, want ErrInvalidCredentials", err)
		}
	})

	t.Run("touch_with_nonzero_and_backward_clock_does_not_reject_watermark", func(t *testing.T) {
		clock := base
		fixture := newAuthPostgresFixture(t, func() time.Time { return clock }, 4*time.Hour, "clock_watermark")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		current := fixture.login(t, authIntegrationPassword)
		clock = base.Add(2 * time.Hour)
		if err := fixture.service.ChangePassword(ctx, fixture.userID, current.SessionID, authIntegrationPassword, authReplacementPassword); err != nil {
			t.Fatalf("change password at future clock: %v", err)
		}
		clock = base.Add(30 * time.Minute)
		refreshed, err := fixture.service.Touch(ctx, current.SessionID)
		if err != nil {
			t.Fatalf("touch after clock rollback: %v", err)
		}
		if refreshed.LastSeenAt.Before(base.Add(2 * time.Hour)) {
			t.Fatalf("touch regressed password watermark: %s", refreshed.LastSeenAt)
		}
		newSession, err := fixture.service.Login(ctx, fixture.username, authReplacementPassword, "", "")
		if err != nil {
			t.Fatalf("new password login after clock rollback: %v", err)
		}
		if newSession.IssuedAt.Before(base.Add(2 * time.Hour)) {
			t.Fatalf("new session issued before password watermark: %s", newSession.IssuedAt)
		}
		if _, err := fixture.service.Login(ctx, fixture.username, authIntegrationPassword, "", ""); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("old password login = %v, want ErrInvalidCredentials", err)
		}
	})

	t.Run("lock_wait_crossing_expiry_does_not_change_password", func(t *testing.T) {
		clock := base
		fixture := newAuthPostgresFixture(t, func() time.Time { return clock }, time.Hour, "expiry_wait")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		current := fixture.login(t, authIntegrationPassword)
		oldHash := authPostgresUserHash(t, ctx, fixture.pool, fixture.userID)
		holder := lockAuthUser(t, ctx, fixture.pool, fixture.userID)
		changeDone := make(chan error, 1)
		go func() {
			changeDone <- fixture.service.ChangePassword(ctx, fixture.userID, current.SessionID, authIntegrationPassword, authReplacementPassword)
		}()
		if err := waitForAuthLockWaiter(ctx, fixture.pool); err != nil {
			_ = holder.Rollback(ctx)
			t.Fatalf("wait for expiry change lock: %v", err)
		}
		clock = current.ExpiresAt.Add(time.Second)
		if err := holder.Commit(ctx); err != nil {
			t.Fatalf("release expiry lock: %v", err)
		}
		if err := <-changeDone; !errors.Is(err, auth.ErrSessionExpired) {
			t.Fatalf("change after expiry lock wait = %v, want ErrSessionExpired", err)
		}
		if got := authPostgresUserHash(t, ctx, fixture.pool, fixture.userID); got != oldHash {
			t.Fatal("password hash changed despite expiry rejection")
		}
		if _, err := fixture.service.Login(ctx, fixture.username, authIntegrationPassword, "", ""); err != nil {
			t.Fatalf("old password remains usable after rejected change: %v", err)
		}
	})

	t.Run("touch_lock_wait_crossing_expiry_deletes_without_refresh", func(t *testing.T) {
		clock := base
		fixture := newAuthPostgresFixture(t, func() time.Time { return clock }, time.Hour, "expiry_touch")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		current := fixture.login(t, authIntegrationPassword)
		holder := lockAuthUser(t, ctx, fixture.pool, fixture.userID)
		touchDone := make(chan error, 1)
		go func() {
			_, err := fixture.service.Touch(ctx, current.SessionID)
			touchDone <- err
		}()
		if err := waitForAuthLockWaiter(ctx, fixture.pool); err != nil {
			_ = holder.Rollback(ctx)
			t.Fatalf("wait for expiry touch lock: %v", err)
		}
		clock = current.ExpiresAt.Add(time.Second)
		if err := holder.Commit(ctx); err != nil {
			t.Fatalf("release expiry touch lock: %v", err)
		}
		if err := <-touchDone; !errors.Is(err, auth.ErrSessionExpired) {
			t.Fatalf("touch after expiry lock wait = %v, want ErrSessionExpired", err)
		}
		if got := authPostgresSessionCount(t, ctx, fixture.pool, fixture.userID); got != 0 {
			t.Fatalf("expired touch left %d session rows", got)
		}
	})

	t.Run("logout_racing_touch_or_change_never_recreates_session", func(t *testing.T) {
		t.Run("touch", func(t *testing.T) {
			fixture := newAuthPostgresFixture(t, func() time.Time { return base }, time.Hour, "logout_touch")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			current := fixture.login(t, authIntegrationPassword)
			holder := lockAuthUser(t, ctx, fixture.pool, fixture.userID)
			touchDone := make(chan error, 1)
			go func() {
				_, err := fixture.service.Touch(ctx, current.SessionID)
				touchDone <- err
			}()
			if err := waitForAuthLockWaiter(ctx, fixture.pool); err != nil {
				_ = holder.Rollback(ctx)
				t.Fatalf("wait for touch lock: %v", err)
			}
			if err := fixture.sessions.Delete(ctx, current.SessionID); err != nil {
				t.Fatalf("logout delete: %v", err)
			}
			if err := holder.Commit(ctx); err != nil {
				t.Fatalf("release touch lock: %v", err)
			}
			if err := <-touchDone; !errors.Is(err, auth.ErrSessionNotFound) {
				t.Fatalf("touch after logout = %v, want ErrSessionNotFound", err)
			}
			if got := authPostgresSessionCount(t, ctx, fixture.pool, fixture.userID); got != 0 {
				t.Fatalf("sessions after logout/touch race = %d, want 0", got)
			}
		})
		t.Run("change", func(t *testing.T) {
			fixture := newAuthPostgresFixture(t, func() time.Time { return base }, time.Hour, "logout_change")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			current := fixture.login(t, authIntegrationPassword)
			holder := lockAuthUser(t, ctx, fixture.pool, fixture.userID)
			changeDone := make(chan error, 1)
			go func() {
				changeDone <- fixture.service.ChangePassword(ctx, fixture.userID, current.SessionID, authIntegrationPassword, authReplacementPassword)
			}()
			if err := waitForAuthLockWaiter(ctx, fixture.pool); err != nil {
				_ = holder.Rollback(ctx)
				t.Fatalf("wait for change lock: %v", err)
			}
			if err := fixture.sessions.Delete(ctx, current.SessionID); err != nil {
				_ = holder.Rollback(ctx)
				t.Fatalf("logout delete: %v", err)
			}
			if err := holder.Commit(ctx); err != nil {
				t.Fatalf("release change lock: %v", err)
			}
			if err := <-changeDone; !errors.Is(err, auth.ErrSessionNotFound) {
				t.Fatalf("change after logout = %v, want ErrSessionNotFound", err)
			}
			if _, err := fixture.service.Login(ctx, fixture.username, authIntegrationPassword, "", ""); err != nil {
				t.Fatalf("old password after rejected change: %v", err)
			}
		})
	})

	t.Run("write_failures_roll_back_all_password_rotation_state", func(t *testing.T) {
		failureCases := []struct {
			name    string
			wantErr string
			sql     func(fixture *authPostgresFixture, current auth.Session) string
		}{
			{name: "password_update", wantErr: "auth test password update failure", sql: func(fixture *authPostgresFixture, _ auth.Session) string {
				return fmt.Sprintf(`
					create function houfeng_auth_test_fail_user_update() returns trigger language plpgsql as $$
					begin raise exception 'auth test password update failure'; end; $$;
					create trigger houfeng_auth_test_fail_user_update before update of password_hash on users
					for each row when (old.user_id = '%s') execute function houfeng_auth_test_fail_user_update()`, fixture.userID)
			}},
			{name: "current_session_update", wantErr: "auth test current session failure", sql: func(fixture *authPostgresFixture, current auth.Session) string {
				return fmt.Sprintf(`
					create function houfeng_auth_test_fail_session_update() returns trigger language plpgsql as $$
					begin raise exception 'auth test current session failure'; end; $$;
					create trigger houfeng_auth_test_fail_session_update before update of issued_at on sessions
					for each row when (old.session_id_hash = '%s') execute function houfeng_auth_test_fail_session_update()`, fixture.sessions.hashSessionID(current.SessionID))
			}},
			{name: "other_session_delete", wantErr: "auth test other session delete failure", sql: func(fixture *authPostgresFixture, current auth.Session) string {
				return fmt.Sprintf(`
					create function houfeng_auth_test_fail_session_delete() returns trigger language plpgsql as $$
					begin raise exception 'auth test other session delete failure'; end; $$;
					create trigger houfeng_auth_test_fail_session_delete before delete on sessions
					for each row when (old.user_id = '%s' and old.session_id_hash <> '%s') execute function houfeng_auth_test_fail_session_delete()`, fixture.userID, fixture.sessions.hashSessionID(current.SessionID))
			}},
		}
		for _, failureCase := range failureCases {
			failureCase := failureCase
			t.Run(failureCase.name, func(t *testing.T) {
				fixture := newAuthPostgresFixture(t, func() time.Time { return base }, time.Hour, "rollback_"+failureCase.name)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				current := fixture.login(t, authIntegrationPassword)
				other := fixture.login(t, authIntegrationPassword)
				oldHash := authPostgresUserHash(t, ctx, fixture.pool, fixture.userID)
				installAuthFailureTrigger(t, ctx, fixture.pool, failureCase.sql(fixture, current))
				changeErr := fixture.service.ChangePassword(ctx, fixture.userID, current.SessionID, authIntegrationPassword, authReplacementPassword)
				if changeErr == nil || !strings.Contains(changeErr.Error(), failureCase.wantErr) {
					t.Fatalf("ChangePassword error = %v, want injected %q", changeErr, failureCase.wantErr)
				}
				if got := authPostgresUserHash(t, ctx, fixture.pool, fixture.userID); got != oldHash {
					t.Fatal("password hash changed despite rollback")
				}
				if _, err := fixture.service.Touch(ctx, current.SessionID); err != nil {
					t.Fatalf("current session after rollback: %v", err)
				}
				if _, err := fixture.service.Touch(ctx, other.SessionID); err != nil {
					t.Fatalf("other session after rollback: %v", err)
				}
				if got := authPostgresSessionCount(t, ctx, fixture.pool, fixture.userID); got != 2 {
					t.Fatalf("session count after rollback = %d, want 2", got)
				}
			})
		}
	})
}
