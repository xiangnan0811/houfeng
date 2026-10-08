package accessadmin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"houfeng/internal/center/auth"
)

const (
	testSupervisorID = "usr_0123456789abcdef01234567"
	testUserID       = "usr_fedcba987654321001234567"
	testGroupID      = "rag_0123456789abcdef"
)

type fakeRepository struct {
	authorizeSupervisorErr error
	authorizeActiveErr     error
	createUserErr          error
	createGroupErr         error
	setMemberErr           error
	createdUser            auth.User
	createdGroupID         string
	createdGroupName       string
	setMemberGroupID       string
	setMemberUserID        string
	setMemberPresent       bool
	createUserCalls        int
	setMemberCalls         int
}

func (f *fakeRepository) AuthorizeSupervisor(context.Context, string) error {
	return f.authorizeSupervisorErr
}

func (f *fakeRepository) AuthorizeActiveUser(context.Context, string) error {
	return f.authorizeActiveErr
}

func (f *fakeRepository) ListUsers(context.Context, string) ([]UserSummary, error) {
	return []UserSummary{}, nil
}

func (f *fakeRepository) CreateUser(_ context.Context, _ string, user auth.User) (UserSummary, error) {
	f.createUserCalls++
	f.createdUser = user
	if f.createUserErr != nil {
		return UserSummary{}, f.createUserErr
	}
	return UserSummary{
		UserID: user.UserID, Username: user.Username, DisplayName: user.DisplayName,
		Role: user.Role, IsSupervisor: user.IsSupervisor,
		CreatedAt: user.CreatedAt,
	}, nil
}

func (f *fakeRepository) SetUserEnabled(context.Context, string, string, bool, time.Time) (UserSummary, error) {
	return UserSummary{}, nil
}

func (f *fakeRepository) ResetUserPassword(context.Context, string, string, string, time.Time) error {
	return nil
}

func (f *fakeRepository) ListGroups(context.Context, string) ([]GroupSummary, error) {
	return []GroupSummary{}, nil
}

func (f *fakeRepository) CreateGroup(_ context.Context, _ string, groupID, displayName string, _ time.Time) (GroupSummary, error) {
	f.createdGroupID = groupID
	f.createdGroupName = displayName
	if f.createGroupErr != nil {
		return GroupSummary{}, f.createGroupErr
	}
	return GroupSummary{GroupID: groupID, DisplayName: displayName}, nil
}

func (f *fakeRepository) RenameGroup(context.Context, string, string, string, time.Time) (GroupSummary, error) {
	return GroupSummary{}, nil
}

func (f *fakeRepository) ListMembers(context.Context, string, string) ([]MemberSummary, error) {
	return []MemberSummary{}, nil
}

func (f *fakeRepository) SetMember(_ context.Context, _ string, groupID, userID string, present bool, _ time.Time) error {
	f.setMemberCalls++
	f.setMemberGroupID = groupID
	f.setMemberUserID = userID
	f.setMemberPresent = present
	return f.setMemberErr
}

func (f *fakeRepository) ListMyGroups(context.Context, string) ([]GroupSummary, error) {
	return []GroupSummary{}, nil
}

func TestCreateUserAuthorizesBeforeValidatingInput(t *testing.T) {
	repo := &fakeRepository{authorizeSupervisorErr: ErrManagementForbidden}
	service := New(repo, Options{})

	_, err := service.CreateUser(context.Background(), testSupervisorID, CreateUserInput{
		Username: " ", Password: "malformed", DisplayName: "ignored",
	})
	if !errors.Is(err, ErrManagementForbidden) {
		t.Fatalf("CreateUser error = %v, want ErrManagementForbidden", err)
	}
	if repo.createUserCalls != 0 {
		t.Fatal("CreateUser reached persistence after authorization failure")
	}
}

func TestCreateUserUsesConfiguredPasswordCostAndDefaultDisplayName(t *testing.T) {
	now := time.Date(2026, time.October, 8, 12, 0, 0, 123456789, time.FixedZone("local", 3600))
	repo := &fakeRepository{}
	service := New(repo, Options{
		Now:                func() time.Time { return now },
		PasswordBcryptCost: bcrypt.MinCost,
	})

	user, err := service.CreateUser(context.Background(), testSupervisorID, CreateUserInput{
		Username: " new-user ", Password: "Strong-Access-2026!", DisplayName: "   ",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if repo.createdUser.Username != "new-user" || repo.createdUser.DisplayName != "new-user" {
		t.Fatalf("created user = %#v, want trimmed username/default display name", repo.createdUser)
	}
	if repo.createdUser.Role != auth.RoleAdmin || repo.createdUser.IsSupervisor {
		t.Fatalf("created user authority = role %q supervisor %v", repo.createdUser.Role, repo.createdUser.IsSupervisor)
	}
	if !repo.createdUser.CreatedAt.Equal(now.UTC()) || !repo.createdUser.PasswordChangedAt.Equal(now.UTC()) {
		t.Fatalf("created timestamps = %v/%v, want %v", repo.createdUser.CreatedAt, repo.createdUser.PasswordChangedAt, now.UTC())
	}
	cost, err := bcrypt.Cost([]byte(repo.createdUser.PasswordHash))
	if err != nil {
		t.Fatalf("bcrypt.Cost: %v", err)
	}
	if cost != bcrypt.MinCost {
		t.Fatalf("bcrypt cost = %d, want %d", cost, bcrypt.MinCost)
	}
	if user.UserID != repo.createdUser.UserID || len(user.UserID) != len("usr_")+24 {
		t.Fatalf("returned user id = %q, want NewUserID grammar", user.UserID)
	}
}

func TestPasswordHashConfigurationFailureIsUnavailable(t *testing.T) {
	repo := &fakeRepository{}
	service := New(repo, Options{PasswordBcryptCost: bcrypt.MinCost - 1})
	_, err := service.CreateUser(context.Background(), testSupervisorID, CreateUserInput{
		Username: "new-user", Password: "Strong-Access-2026!",
	})
	if !errors.Is(err, ErrManagementUnavailable) {
		t.Fatalf("CreateUser invalid bcrypt cost = %v, want ErrManagementUnavailable", err)
	}
	if repo.createUserCalls != 0 {
		t.Fatal("invalid bcrypt configuration reached persistence")
	}
}

func TestDisplayNameAndGroupValidation(t *testing.T) {
	repo := &fakeRepository{}
	service := New(repo, Options{})
	if _, err := service.CreateGroup(context.Background(), testSupervisorID, "   "); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty group name error = %v, want ErrInvalidRequest", err)
	}
	if _, err := service.CreateGroup(context.Background(), testSupervisorID, "界"+string(make([]rune, 100))); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("long group name error = %v, want ErrInvalidRequest", err)
	}
	if repo.createdGroupID != "" {
		t.Fatal("invalid group input reached persistence")
	}
}

func TestSetMemberValidatesOpaqueIDsAndPassesPresence(t *testing.T) {
	repo := &fakeRepository{}
	service := New(repo, Options{})
	if err := service.SetMember(context.Background(), testSupervisorID, testGroupID, testUserID, true); err != nil {
		t.Fatalf("SetMember add: %v", err)
	}
	if repo.setMemberCalls != 1 || repo.setMemberGroupID != testGroupID || repo.setMemberUserID != testUserID || !repo.setMemberPresent {
		t.Fatalf("SetMember call = %#v/%d, want add %q/%q", repo, repo.setMemberCalls, testGroupID, testUserID)
	}
	if err := service.SetMember(context.Background(), testSupervisorID, "rag_BAD", testUserID, false); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("malformed group id error = %v, want ErrInvalidRequest", err)
	}
	if repo.setMemberCalls != 1 {
		t.Fatal("malformed group id reached persistence")
	}
}

func TestCreateUserMapsUsernameConflict(t *testing.T) {
	repo := &fakeRepository{createUserErr: auth.ErrUsernameTaken}
	service := New(repo, Options{PasswordBcryptCost: bcrypt.MinCost})
	_, err := service.CreateUser(context.Background(), testSupervisorID, CreateUserInput{
		Username: "conflict", Password: "Strong-Access-2026!",
	})
	if !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("CreateUser conflict = %v, want ErrUsernameTaken", err)
	}
}
func TestMemberSummaryWireUnion(t *testing.T) {
	createdAt := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	live, err := json.Marshal(MemberSummary{UserSummary: UserSummary{
		UserID: "usr_0123456789abcdef01234567", Username: "operator",
		DisplayName: "Operator", Role: auth.RoleAdmin, CreatedAt: createdAt,
	}})
	if err != nil {
		t.Fatalf("marshal live member: %v", err)
	}
	wantLive := `{"user_id":"usr_0123456789abcdef01234567","username":"operator","display_name":"Operator","role":"admin","is_supervisor":false,"disabled_at":null,"created_at":"2026-10-08T12:00:00Z"}`
	if string(live) != wantLive {
		t.Fatalf("live member JSON = %s, want %s", live, wantLive)
	}
	missing, err := json.Marshal(MemberSummary{UserSummary: UserSummary{UserID: "usr_fedcba987654321001234567"}, Missing: true})
	if err != nil {
		t.Fatalf("marshal missing member: %v", err)
	}
	if string(missing) != `{"user_id":"usr_fedcba987654321001234567","missing":true}` {
		t.Fatalf("missing member JSON = %s", missing)
	}
}
