package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"houfeng/internal/center/accessadmin"
	"houfeng/internal/center/auth"
	"houfeng/internal/center/http/sessionctx"
)

type accessAdminHandlerRepository struct {
	supervisorErr  error
	activeErr      error
	createUserErr  error
	createGroupErr error
	setMemberErr   error
	users          []accessadmin.UserSummary
	groups         []accessadmin.GroupSummary
	members        []accessadmin.MemberSummary
	myGroups       []accessadmin.GroupSummary
	createCalls    int
	setMemberCall  int
	lastGroupID    string
	lastUserID     string
	lastPresent    bool
}

func (f *accessAdminHandlerRepository) AuthorizeSupervisor(context.Context, string) error {
	return f.supervisorErr
}
func (f *accessAdminHandlerRepository) AuthorizeActiveUser(context.Context, string) error {
	return f.activeErr
}
func (f *accessAdminHandlerRepository) ListUsers(context.Context, string) ([]accessadmin.UserSummary, error) {
	return f.users, nil
}
func (f *accessAdminHandlerRepository) CreateUser(_ context.Context, _ string, user auth.User) (accessadmin.UserSummary, error) {
	f.createCalls++
	if f.createUserErr != nil {
		return accessadmin.UserSummary{}, f.createUserErr
	}
	return accessadmin.UserSummary{
		UserID: user.UserID, Username: user.Username, DisplayName: user.DisplayName,
		Role: user.Role, IsSupervisor: user.IsSupervisor, CreatedAt: user.CreatedAt,
	}, nil
}
func (f *accessAdminHandlerRepository) SetUserEnabled(context.Context, string, string, bool, time.Time) (accessadmin.UserSummary, error) {
	return accessadmin.UserSummary{}, nil
}
func (f *accessAdminHandlerRepository) ResetUserPassword(context.Context, string, string, string, time.Time) error {
	return nil
}
func (f *accessAdminHandlerRepository) ListGroups(context.Context, string) ([]accessadmin.GroupSummary, error) {
	return f.groups, nil
}
func (f *accessAdminHandlerRepository) CreateGroup(_ context.Context, _ string, groupID, displayName string, _ time.Time) (accessadmin.GroupSummary, error) {
	f.createCalls++
	if f.createGroupErr != nil {
		return accessadmin.GroupSummary{}, f.createGroupErr
	}
	return accessadmin.GroupSummary{GroupID: groupID, DisplayName: displayName}, nil
}
func (f *accessAdminHandlerRepository) RenameGroup(context.Context, string, string, string, time.Time) (accessadmin.GroupSummary, error) {
	return accessadmin.GroupSummary{}, nil
}
func (f *accessAdminHandlerRepository) ListMembers(context.Context, string, string) ([]accessadmin.MemberSummary, error) {
	return f.members, nil
}
func (f *accessAdminHandlerRepository) SetMember(_ context.Context, _ string, groupID, userID string, present bool, _ time.Time) error {
	f.setMemberCall++
	f.lastGroupID, f.lastUserID, f.lastPresent = groupID, userID, present
	return f.setMemberErr
}
func (f *accessAdminHandlerRepository) ListMyGroups(context.Context, string) ([]accessadmin.GroupSummary, error) {
	return f.myGroups, nil
}

func newAccessAdminHandlerRequest(method, path, body, actorID string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	return req.WithContext(sessionctx.WithUserID(req.Context(), actorID))
}

func TestAccessAdminOrdinaryAdminForbiddenBeforeBodyDecode(t *testing.T) {
	repo := &accessAdminHandlerRepository{supervisorErr: accessadmin.ErrManagementForbidden}
	service := accessadmin.New(repo, accessadmin.Options{PasswordBcryptCost: bcrypt.MinCost})
	req := newAccessAdminHandlerRequest(http.MethodPost, "/api/admin/users", "{", "usr_fedcba987654321001234567")
	response := httptest.NewRecorder()

	AccessAdmin(service).ServeHTTP(response, req)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"management_forbidden"`) {
		t.Fatalf("status/body = %d %s, want management_forbidden 403", response.Code, response.Body.String())
	}
	if repo.createCalls != 0 {
		t.Fatal("ordinary admin malformed request reached mutation")
	}
}

func TestAccessAdminCreateUserRejectsUnknownFields(t *testing.T) {
	repo := &accessAdminHandlerRepository{}
	service := accessadmin.New(repo, accessadmin.Options{PasswordBcryptCost: bcrypt.MinCost})
	req := newAccessAdminHandlerRequest(http.MethodPost, "/api/admin/users", `{"username":"new","password":"Strong-Access-2026!","unexpected":true}`, "usr_0123456789abcdef01234567")
	response := httptest.NewRecorder()

	AccessAdmin(service).ServeHTTP(response, req)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_request"`) {
		t.Fatalf("status/body = %d %s, want invalid_request 400", response.Code, response.Body.String())
	}
	if repo.createCalls != 0 {
		t.Fatal("unknown-field request reached mutation")
	}
}

func TestAccessAdminMemberUsesPathValuesAndReturnsNoContent(t *testing.T) {
	repo := &accessAdminHandlerRepository{}
	service := accessadmin.New(repo, accessadmin.Options{})
	req := newAccessAdminHandlerRequest(http.MethodPut, "/api/admin/record-access-groups/rag_0123456789abcdef/members/usr_fedcba987654321001234567", `{}`, "usr_0123456789abcdef01234567")
	req.SetPathValue("id", "rag_0123456789abcdef")
	req.SetPathValue("user_id", "usr_fedcba987654321001234567")
	response := httptest.NewRecorder()

	AccessAdmin(service).ServeHTTP(response, req)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body=%s, want 204", response.Code, response.Body.String())
	}
	if repo.setMemberCall != 1 || repo.lastGroupID != "rag_0123456789abcdef" || repo.lastUserID != "usr_fedcba987654321001234567" || !repo.lastPresent {
		t.Fatalf("member call = %d/%q/%q/%v", repo.setMemberCall, repo.lastGroupID, repo.lastUserID, repo.lastPresent)
	}
}

func TestAccessAdminMineGroupsAllowsActiveNonSupervisor(t *testing.T) {
	repo := &accessAdminHandlerRepository{
		supervisorErr: accessadmin.ErrManagementForbidden,
		myGroups:      []accessadmin.GroupSummary{{GroupID: "rag_0123456789abcdef", DisplayName: "验收组"}},
	}
	service := accessadmin.New(repo, accessadmin.Options{})
	req := newAccessAdminHandlerRequest(http.MethodGet, "/api/record-access-groups/mine", "", "usr_fedcba987654321001234567")
	response := httptest.NewRecorder()

	AccessAdmin(service).ServeHTTP(response, req)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"group_id":"rag_0123456789abcdef"`) {
		t.Fatalf("status/body = %d %s, want mine groups", response.Code, response.Body.String())
	}
}

func TestAccessAdminMapsConflictAndUnavailableErrors(t *testing.T) {
	tests := []struct {
		name       string
		repo       *accessAdminHandlerRepository
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "username conflict",
			repo:       &accessAdminHandlerRepository{createUserErr: accessadmin.ErrUsernameTaken},
			body:       `{"username":"new","password":"Strong-Access-2026!"}`,
			wantStatus: http.StatusConflict,
			wantCode:   "username_taken",
		},
		{
			name:       "group conflict",
			repo:       &accessAdminHandlerRepository{createGroupErr: accessadmin.ErrGroupNameTaken},
			body:       `{"display_name":"重复组"}`,
			wantStatus: http.StatusConflict,
			wantCode:   "group_name_taken",
		},
		{
			name:       "repository unavailable",
			repo:       &accessAdminHandlerRepository{createGroupErr: errors.New("sql detail")},
			body:       `{"display_name":"组"}`,
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "management_unavailable",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := accessadmin.New(test.repo, accessadmin.Options{PasswordBcryptCost: bcrypt.MinCost})
			path := "/api/admin/record-access-groups"
			method := http.MethodPost
			if test.name == "username conflict" {
				path = "/api/admin/users"
			}
			req := newAccessAdminHandlerRequest(method, path, test.body, "usr_0123456789abcdef01234567")
			response := httptest.NewRecorder()
			AccessAdmin(service).ServeHTTP(response, req)
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), `"code":"`+test.wantCode+`"`) {
				t.Fatalf("status/body = %d %s, want %d/%s", response.Code, response.Body.String(), test.wantStatus, test.wantCode)
			}
		})
	}
}
