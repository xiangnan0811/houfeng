package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"houfeng/internal/center/accessadmin"
	"houfeng/internal/center/auth"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

const accessManagementHMACKey = "0123456789abcdef0123456789abcdef"

func TestPostgresIntegrationAccessManagementLifecycleAndIdempotency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := openTemporaryAssetLifecyclePostgresSchema(t, ctx)
	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	supervisorID := "usr_0123456789abcdef01234567"
	supervisorName := "access-supervisor"
	if err := seedAccessManagementUser(t, ctx, pool, supervisorID, supervisorName, true, now); err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresAccessAdminRepository(pool)
	service := accessadmin.New(repository, accessadmin.Options{Now: func() time.Time { return now }, PasswordBcryptCost: bcrypt.MinCost})

	created, err := service.CreateUser(ctx, supervisorID, accessadmin.CreateUserInput{
		Username: "managed-user", Password: "Strong-Access-2026!", DisplayName: "Managed User",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if created.IsSupervisor || created.Role != auth.RoleAdmin || created.UserID == "" {
		t.Fatalf("created user = %#v, want ordinary admin", created)
	}
	if _, err := service.CreateUser(ctx, supervisorID, accessadmin.CreateUserInput{
		Username: "managed-user", Password: "Strong-Access-2026!",
	}); !errors.Is(err, accessadmin.ErrUsernameTaken) {
		t.Fatalf("duplicate CreateUser = %v, want ErrUsernameTaken", err)
	}

	group, err := service.CreateGroup(ctx, supervisorID, "验收受限组")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if _, err := service.CreateGroup(ctx, supervisorID, "验收受限组"); !errors.Is(err, accessadmin.ErrGroupNameTaken) {
		t.Fatalf("duplicate CreateGroup = %v, want ErrGroupNameTaken", err)
	}
	if err := service.SetMember(ctx, supervisorID, group.GroupID, created.UserID, true); err != nil {
		t.Fatalf("SetMember(add): %v", err)
	}
	if err := service.SetMember(ctx, supervisorID, group.GroupID, created.UserID, true); err != nil {
		t.Fatalf("SetMember(add idempotent): %v", err)
	}
	members, err := service.ListMembers(ctx, supervisorID, group.GroupID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 1 || members[0].UserID != created.UserID || members[0].Missing {
		t.Fatalf("members = %#v, want one live member", members)
	}
	if err := service.SetMember(ctx, supervisorID, group.GroupID, created.UserID, false); err != nil {
		t.Fatalf("SetMember(remove): %v", err)
	}
	if err := service.SetMember(ctx, supervisorID, group.GroupID, created.UserID, false); err != nil {
		t.Fatalf("SetMember(remove idempotent): %v", err)
	}

	if _, err := service.SetUserEnabled(ctx, supervisorID, supervisorID, false); !errors.Is(err, accessadmin.ErrSupervisorProtected) {
		t.Fatalf("disable supervisor = %v, want ErrSupervisorProtected", err)
	}
	if err := service.ResetUserPassword(ctx, supervisorID, supervisorID, "Strong-Access-2026!"); !errors.Is(err, accessadmin.ErrSupervisorProtected) {
		t.Fatalf("reset supervisor = %v, want ErrSupervisorProtected", err)
	}
	if _, err := service.SetUserEnabled(ctx, supervisorID, created.UserID, false); err != nil {
		t.Fatalf("disable managed user: %v", err)
	}
	if _, err := service.SetUserEnabled(ctx, supervisorID, created.UserID, false); err != nil {
		t.Fatalf("repeat disable managed user: %v", err)
	}
	if err := service.SetMember(ctx, supervisorID, group.GroupID, created.UserID, true); !errors.Is(err, accessadmin.ErrUserDisabled) {
		t.Fatalf("add disabled user = %v, want ErrUserDisabled", err)
	}
	if _, err := service.SetUserEnabled(ctx, supervisorID, created.UserID, true); err != nil {
		t.Fatalf("enable managed user: %v", err)
	}
	if err := service.SetMember(ctx, supervisorID, group.GroupID, created.UserID, true); err != nil {
		t.Fatalf("add enabled user: %v", err)
	}
}

func TestPostgresIntegrationAccessManagementABRecordAuthorizationRevocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "access-management-records", 2)
	defer runtimePool.Close()
	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	aID := "usr_0123456789abcdef01234567"
	bID := "usr_fedcba987654321001234567"
	if err := seedAccessManagementUser(t, ctx, fixture.db, aID, "access-a", true, now); err != nil {
		t.Fatal(err)
	}
	if err := seedAccessManagementUser(t, ctx, fixture.db, bID, "access-b", false, now); err != nil {
		t.Fatal(err)
	}
	accessService := accessadmin.New(NewPostgresAccessAdminRepository(runtimePool), accessadmin.Options{Now: func() time.Time { return now }, PasswordBcryptCost: bcrypt.MinCost})
	group, err := accessService.CreateGroup(ctx, aID, "验收受限组")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := accessService.SetMember(ctx, aID, group.GroupID, aID, true); err != nil {
		t.Fatalf("SetMember(A): %v", err)
	}
	if err := accessService.SetMember(ctx, aID, group.GroupID, bID, true); err != nil {
		t.Fatalf("SetMember(B): %v", err)
	}

	recordRepository := newRecordsPostgresRepository(t, runtimePool)
	recordID := "rec_accessmanagement"
	firstInput := accessManagementRestrictedRevisionInput(t, "A/B first", group.GroupID)
	first, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordCreate, recordID, "", 0, 0, firstInput, "access-management-create",
	))
	if err != nil {
		t.Fatalf("CommitRevision(first): %v", err)
	}
	secondInput := accessManagementRestrictedRevisionInput(t, "A/B second", group.GroupID)
	second, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordUpdate, recordID, first.RevisionID, first.LockVersion,
		first.AuthorizationEpoch, secondInput, "access-management-update",
	))
	if err != nil {
		t.Fatalf("CommitRevision(second): %v", err)
	}

	resolver := &fakeCurrentRecordSubjectResolver{resolved: records.ResolvedSubject{
		ProjectID:        recordauth.ProjectIDDefault,
		StableID:         testStoreRecordVPSID,
		IdentitySnapshot: mustStoreRecordSnapshot(t, records.SubjectKindVPS, map[string]string{"display_name": "A/B VPS"}),
		LiveRoute:        "/vps/" + testStoreRecordVPSID,
		CaptureAuthorization: mustStoreLiveSourceAuthorization(
			t, recordauth.SourceKindVPS, testStoreRecordVPSID, secondInput.VisibilityScope(),
		),
	}}
	authorizations := newPostgresCurrentRecordAuthorizationSource(runtimePool, resolver, allowRecordPlatformAdmissionGate)
	aActor := accessManagementActor(t, aID, nil)
	bActor := accessManagementActor(t, bID, []string{group.GroupID})
	// Both actors are members at record creation; A is then removed and should
	// lose current, historical, and attachment authorization on the next read.
	aActor.GroupIDs = []string{group.GroupID}
	assertAccessManagementRecordReadable(t, ctx, authorizations, aActor, recordID, first.RevisionID, true)
	assertAccessManagementRecordReadable(t, ctx, authorizations, bActor, recordID, first.RevisionID, true)

	sessions, err := NewPostgresSessionRepository(runtimePool, []byte(accessManagementHMACKey))
	if err != nil {
		t.Fatalf("NewPostgresSessionRepository: %v", err)
	}
	authService := auth.New(NewPostgresUserRepository(runtimePool), sessions, auth.Options{Now: func() time.Time { return now }, PasswordBcryptCost: bcrypt.MinCost})
	aSession, err := authService.Login(ctx, "access-a", "Strong-Access-2026!", "access-test", "203.0.113.10")
	if err != nil {
		t.Fatalf("Login(A): %v", err)
	}
	if got, err := authService.UserBySession(ctx, aSession.SessionID); err != nil || got.UserID != aID {
		t.Fatalf("UserBySession(A before revoke) = %#v/%v", got, err)
	}
	if err := accessService.SetMember(ctx, aID, group.GroupID, aID, false); err != nil {
		t.Fatalf("SetMember(remove A): %v", err)
	}
	aActor.GroupIDs = nil
	assertAccessManagementRecordReadable(t, ctx, authorizations, aActor, recordID, first.RevisionID, false)
	assertAccessManagementRecordReadable(t, ctx, authorizations, bActor, recordID, first.RevisionID, true)
	if got, err := authService.UserBySession(ctx, aSession.SessionID); err != nil || got.UserID != aID {
		t.Fatalf("UserBySession(A after group revoke) = %#v/%v, want valid session", got, err)
	}
	if err := accessService.SetMember(ctx, aID, group.GroupID, aID, true); err != nil {
		t.Fatalf("SetMember(restore A): %v", err)
	}
	aActor.GroupIDs = []string{group.GroupID}
	assertAccessManagementRecordReadable(t, ctx, authorizations, aActor, recordID, first.RevisionID, true)

	// Historical and current revisions are both checked by the helper, and the
	// attachment adapter uses the same current authorization source.
	_ = second.RevisionID
}

func seedAccessManagementUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID, username string, supervisor bool, now time.Time) error {
	t.Helper()
	hash, err := auth.HashPasswordWithCost("Strong-Access-2026!", bcrypt.MinCost)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		insert into users (
			user_id, username, password_hash, display_name, role,
			is_supervisor, disabled_at, created_at, password_changed_at
		) values ($1, $2, $3, $4, $5, $6, null, $7, $7)`,
		userID, username, hash, username, auth.RoleAdmin, supervisor, now.UTC())
	return err
}

func accessManagementRestrictedRevisionInput(t *testing.T, title, groupID string) records.CompleteRevisionInput {
	t.Helper()
	base := recordsPostgresCompleteRevisionInput(t, title)
	visibility := mustStoreRestrictedVisibility(t, nil, []string{groupID}, 2)
	subjects := base.Subjects()
	subjects[0].CaptureAuthorization = mustStoreLiveSourceAuthorization(t, recordauth.SourceKindVPS, testStoreRecordVPSID, visibility)
	input, err := records.NormalizeCompleteRevisionInput(records.CompleteRevisionValues{
		Title:                  base.Title(),
		BodyMarkdown:           base.BodyMarkdown(),
		MarkdownDialectVersion: base.MarkdownDialectVersion(),
		RecordType:             base.RecordType(),
		BusinessStatus:         base.BusinessStatus(),
		ImpactLevel:            base.ImpactLevel(),
		OccurredAt:             base.OccurredAt(),
		CompletedAt:            base.CompletedAt(),
		VisibilityScope:        visibility,
		Subjects:               subjects,
		Tags:                   base.Tags(),
		OwnerID:                base.OwnerID(),
		Participants:           base.Participants(),
		AttachmentIDs:          base.AttachmentIDs(),
		EvidenceSnapshotIDs:    base.EvidenceSnapshotIDs(),
		FollowUpAt:             base.FollowUpAt(),
		Template:               base.Template(),
		AuthorID:               base.AuthorID(),
		SaveReason:             base.SaveReason(),
	})
	if err != nil {
		t.Fatalf("NormalizeCompleteRevisionInput(restricted): %v", err)
	}
	return input
}

func accessManagementActor(t *testing.T, userID string, groupIDs []string) recordauth.ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: userID, Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault, GroupIDs: groupIDs,
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope(%s): %v", userID, err)
	}
	return actor
}

func assertAccessManagementRecordReadable(
	t *testing.T,
	ctx context.Context,
	authorizations *PostgresCurrentRecordAuthorizationSource,
	actor recordauth.ActorScope,
	recordID, historicalRevisionID string,
	wantReadable bool,
) {
	t.Helper()
	current, err := authorizations.ResolveCurrentRecordAuthorization(ctx, actor, recordID)
	if err != nil {
		t.Fatalf("ResolveCurrentRecordAuthorization(%s): %v", actor.UserID, err)
	}
	currentErr := records.AuthorizeRecordResource(actor, recordauth.CapabilityRecordRead, current.Evidence)
	historical, err := authorizations.ResolveRecordRevisionAuthorization(ctx, actor, recordID, historicalRevisionID)
	if err != nil {
		t.Fatalf("ResolveRecordRevisionAuthorization(%s): %v", actor.UserID, err)
	}
	historicalErr := records.AuthorizeRecordResource(actor, recordauth.CapabilityRecordRead, historical.Evidence)
	attachmentErr := authorizations.AuthorizeRecordAttachmentRead(ctx, actor, recordID)
	if wantReadable {
		if currentErr != nil || historicalErr != nil || attachmentErr != nil {
			t.Fatalf("actor %s read errors current=%v historical=%v attachment=%v", actor.UserID, currentErr, historicalErr, attachmentErr)
		}
		return
	}
	if !errors.Is(currentErr, recordauth.ErrDenied) || !errors.Is(historicalErr, recordauth.ErrDenied) || !errors.Is(attachmentErr, recordauth.ErrDenied) {
		t.Fatalf("actor %s revoke errors current=%v historical=%v attachment=%v, want ErrDenied", actor.UserID, currentErr, historicalErr, attachmentErr)
	}
}

const accessManagementPassword = "Strong-Access-2026!"

type accessManagementAuthCase struct {
	owner        *authPostgresFixture
	authPool     *pgxpool.Pool
	authService  *auth.Service
	sessions     *PostgresSessionRepository
	management   *accessadmin.Service
	target       accessadmin.UserSummary
	targetSecret string
	now          func() time.Time
}

func TestPostgresIntegrationAccessManagementSessionLifecycleAndRollback(t *testing.T) {
	t.Run("disable_after_login_commit_revokes_session_and_enable_does_not_revive", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		fixture := newAccessManagementAuthCase(t, ctx, "disable_commit")
		session, err := fixture.authService.Login(ctx, fixture.target.Username, fixture.targetSecret, "access-test", "203.0.113.10")
		if err != nil {
			t.Fatalf("Login(target): %v", err)
		}
		if _, err := fixture.management.SetUserEnabled(ctx, fixture.owner.userID, fixture.target.UserID, false); err != nil {
			t.Fatalf("SetUserEnabled(disable): %v", err)
		}
		if err := fixture.sessions.ValidateSession(ctx, session.SessionID, fixture.now); !errors.Is(err, auth.ErrSessionNotFound) {
			t.Fatalf("ValidateSession(disabled) = %v, want ErrSessionNotFound", err)
		}
		if got := authPostgresSessionCount(t, ctx, fixture.owner.pool, fixture.target.UserID); got != 0 {
			t.Fatalf("disabled target session count = %d, want 0", got)
		}
		if _, err := fixture.management.SetUserEnabled(ctx, fixture.owner.userID, fixture.target.UserID, true); err != nil {
			t.Fatalf("SetUserEnabled(enable): %v", err)
		}
		if err := fixture.sessions.ValidateSession(ctx, session.SessionID, fixture.now); !errors.Is(err, auth.ErrSessionNotFound) {
			t.Fatalf("ValidateSession(old cookie after enable) = %v, want ErrSessionNotFound", err)
		}
		if _, err := fixture.authService.Login(ctx, fixture.target.Username, fixture.targetSecret, "access-test", "203.0.113.10"); err != nil {
			t.Fatalf("Login(after enable): %v", err)
		}
	})

	t.Run("reset_old_hash_login_and_touch_are_rejected_after_commit", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		fixture := newAccessManagementAuthCase(t, ctx, "reset_commit")
		oldSession, err := fixture.authService.Login(ctx, fixture.target.Username, fixture.targetSecret, "access-test", "203.0.113.11")
		if err != nil {
			t.Fatalf("Login(target): %v", err)
		}
		oldHash := authPostgresUserHash(t, ctx, fixture.owner.pool, fixture.target.UserID)
		if err := fixture.management.ResetUserPassword(ctx, fixture.owner.userID, fixture.target.UserID, "Replacement-Access-2026!"); err != nil {
			t.Fatalf("ResetUserPassword: %v", err)
		}
		if _, err := fixture.sessions.CreateIfPasswordHash(ctx, oldHash, auth.Session{
			SessionID: authTestSessionID(t),
			UserID:    fixture.target.UserID,
		}, fixture.now, time.Hour); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("CreateIfPasswordHash(old hash) = %v, want ErrInvalidCredentials", err)
		}
		if _, err := fixture.sessions.TouchWithUserLock(ctx, oldSession.SessionID, fixture.now, time.Hour); !errors.Is(err, auth.ErrSessionNotFound) {
			t.Fatalf("TouchWithUserLock(old cookie) = %v, want ErrSessionNotFound", err)
		}
		if _, err := fixture.authService.Login(ctx, fixture.target.Username, fixture.targetSecret, "access-test", "203.0.113.11"); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("Login(old password) = %v, want ErrInvalidCredentials", err)
		}
		if _, err := fixture.authService.Login(ctx, fixture.target.Username, "Replacement-Access-2026!", "access-test", "203.0.113.11"); err != nil {
			t.Fatalf("Login(new password): %v", err)
		}
	})

	t.Run("reset_waits_for_old_hash_login_and_then_deletes_committed_session", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		fixture := newAccessManagementAuthCase(t, ctx, "reset_interleave")
		installAuthSessionInsertAdvisoryBlock(t, ctx, fixture.owner.pool)
		holder := holdAuthSessionInsertAdvisory(t, ctx, fixture.owner.pool, fixture.target.UserID)
		released := false
		defer func() {
			if !released {
				_ = holder.Rollback(context.Background())
			}
		}()
		loginDone := make(chan struct {
			session auth.Session
			err     error
		}, 1)
		go func() {
			session, err := fixture.authService.Login(ctx, fixture.target.Username, fixture.targetSecret, "access-test", "203.0.113.12")
			loginDone <- struct {
				session auth.Session
				err     error
			}{session: session, err: err}
		}()
		if err := waitForAuthSessionInsertAdvisoryWaiter(ctx, fixture.owner.pool); err != nil {
			t.Fatalf("wait for old-hash login insert barrier: %v", err)
		}
		resetDone := make(chan error, 1)
		go func() {
			resetDone <- fixture.management.ResetUserPassword(ctx, fixture.owner.userID, fixture.target.UserID, "Replacement-Access-2026!")
		}()
		if err := waitForAccessAdminTargetLockWaiter(ctx, fixture.owner.pool); err != nil {
			t.Fatalf("wait for reset target lock: %v", err)
		}
		if err := holder.Commit(ctx); err != nil {
			t.Fatalf("release old-hash login barrier: %v", err)
		}
		released = true
		loginResult := <-loginDone
		if loginResult.err != nil {
			t.Fatalf("old-hash login at interleave barrier: %v", loginResult.err)
		}
		if err := <-resetDone; err != nil {
			t.Fatalf("ResetUserPassword after login barrier: %v", err)
		}
		if err := fixture.sessions.ValidateSession(ctx, loginResult.session.SessionID, fixture.now); !errors.Is(err, auth.ErrSessionNotFound) {
			t.Fatalf("barrier login session validation = %v, want ErrSessionNotFound", err)
		}
	})

	t.Run("disable_rollback_preserves_active_user_and_session", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		fixture := newAccessManagementAuthCase(t, ctx, "disable_rollback")
		session, err := fixture.authService.Login(ctx, fixture.target.Username, fixture.targetSecret, "access-test", "203.0.113.13")
		if err != nil {
			t.Fatalf("Login(target): %v", err)
		}
		installAccessManagementDeleteFailure(t, ctx, fixture.owner.pool, "access_management_disable_failure")
		if _, err := fixture.management.SetUserEnabled(ctx, fixture.owner.userID, fixture.target.UserID, false); !errors.Is(err, accessadmin.ErrManagementUnavailable) {
			t.Fatalf("SetUserEnabled(triggered rollback) = %v, want ErrManagementUnavailable", err)
		}
		var active bool
		if err := fixture.owner.pool.QueryRow(ctx, `select disabled_at is null from users where user_id = $1`, fixture.target.UserID).Scan(&active); err != nil {
			t.Fatalf("read rollback user state: %v", err)
		}
		if !active {
			t.Fatal("disable transaction left user disabled after rollback")
		}
		if err := fixture.sessions.ValidateSession(ctx, session.SessionID, fixture.now); err != nil {
			t.Fatalf("session after disable rollback: %v", err)
		}
	})

	t.Run("reset_rollback_preserves_old_hash_and_sessions", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		fixture := newAccessManagementAuthCase(t, ctx, "reset_rollback")
		session, err := fixture.authService.Login(ctx, fixture.target.Username, fixture.targetSecret, "access-test", "203.0.113.14")
		if err != nil {
			t.Fatalf("Login(target): %v", err)
		}
		oldHash := authPostgresUserHash(t, ctx, fixture.owner.pool, fixture.target.UserID)
		installAccessManagementDeleteFailure(t, ctx, fixture.owner.pool, "access_management_reset_failure")
		if err := fixture.management.ResetUserPassword(ctx, fixture.owner.userID, fixture.target.UserID, "Replacement-Access-2026!"); !errors.Is(err, accessadmin.ErrManagementUnavailable) {
			t.Fatalf("ResetUserPassword(triggered rollback) = %v, want ErrManagementUnavailable", err)
		}
		if got := authPostgresUserHash(t, ctx, fixture.owner.pool, fixture.target.UserID); got != oldHash {
			t.Fatal("reset rollback changed password hash")
		}
		if err := fixture.sessions.ValidateSession(ctx, session.SessionID, fixture.now); err != nil {
			t.Fatalf("session after reset rollback: %v", err)
		}
		if _, err := fixture.authService.Login(ctx, fixture.target.Username, fixture.targetSecret, "access-test", "203.0.113.14"); err != nil {
			t.Fatalf("Login(old password after reset rollback): %v", err)
		}
	})
}

func newAccessManagementAuthCase(t *testing.T, ctx context.Context, suffix string) *accessManagementAuthCase {
	t.Helper()
	now := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	owner := newAuthPostgresFixture(t, func() time.Time { return now }, time.Hour, suffix)
	if _, err := owner.pool.Exec(ctx, `update users set is_supervisor = true where user_id = $1`, owner.userID); err != nil {
		t.Fatalf("promote auth fixture supervisor: %v", err)
	}
	managementPool := cloneAccessManagementPool(t, ctx, owner.pool, 4)
	authPool := cloneAccessManagementPool(t, ctx, owner.pool, 4)
	sessions, err := NewPostgresSessionRepository(authPool, []byte(accessManagementHMACKey))
	if err != nil {
		t.Fatalf("NewPostgresSessionRepository: %v", err)
	}
	authService := auth.New(NewPostgresUserRepository(authPool), sessions, auth.Options{SessionTTL: time.Hour, Now: func() time.Time { return now }, PasswordBcryptCost: bcrypt.MinCost})
	management := accessadmin.New(NewPostgresAccessAdminRepository(managementPool), accessadmin.Options{Now: func() time.Time { return now }, PasswordBcryptCost: bcrypt.MinCost})
	target, err := management.CreateUser(ctx, owner.userID, accessadmin.CreateUserInput{
		Username: "managed-" + suffix, Password: accessManagementPassword, DisplayName: "Managed " + suffix,
	})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", suffix, err)
	}
	return &accessManagementAuthCase{
		owner: owner, authPool: authPool, authService: authService, sessions: sessions,
		management: management, target: target, targetSecret: accessManagementPassword, now: func() time.Time { return now },
	}
}

func cloneAccessManagementPool(t *testing.T, ctx context.Context, source *pgxpool.Pool, maxConns int32) *pgxpool.Pool {
	t.Helper()
	config := source.Config().Copy()
	config.MinConns = 0
	config.MaxConns = maxConns
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("clone access-management pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func authTestSessionID(t *testing.T) string {
	t.Helper()
	id, err := auth.NewSessionID()
	if err != nil {
		t.Fatalf("NewSessionID: %v", err)
	}
	return id
}

func waitForAccessAdminTargetLockWaiter(ctx context.Context, pool *pgxpool.Pool) error {
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
			  and query like '%select is_supervisor%'
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
	return errors.New("timed out waiting for access-management target lock waiter")
}

func installAccessManagementDeleteFailure(t *testing.T, ctx context.Context, pool *pgxpool.Pool, functionName string) {
	t.Helper()
	if functionName != "access_management_disable_failure" && functionName != "access_management_reset_failure" {
		t.Fatalf("unexpected access-management trigger name %q", functionName)
	}
	if _, err := pool.Exec(ctx, `
		create function `+functionName+`() returns trigger
		language plpgsql as $$
		begin
			raise exception 'access-management test delete failure';
		end;
		$$;
		create trigger `+functionName+`
		before delete on sessions for each row
		execute function `+functionName+`()`); err != nil {
		t.Fatalf("install %s: %v", functionName, err)
	}
}
