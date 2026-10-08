package accessadmin

import (
	"context"
	"encoding/json"
	"errors"
	"houfeng/internal/center/auth"
	"time"
)

var (
	ErrInvalidRequest        = errors.New("invalid access-management request")
	ErrManagementForbidden   = errors.New("access-management forbidden")
	ErrManagementUnavailable = errors.New("access-management unavailable")
	ErrResourceNotFound      = errors.New("access-management resource not found")
	ErrUsernameTaken         = errors.New("username already taken")
	ErrGroupNameTaken        = errors.New("record access-group name already taken")
	ErrSupervisorProtected   = errors.New("supervisor account is protected")
	ErrUserDisabled          = errors.New("user is disabled")
)

// Options controls the clock and password policy used by the management
// service. A zero value uses time.Now and auth.DefaultPasswordBcryptCost.
type Options struct {
	Now                func() time.Time
	PasswordBcryptCost int
}

type UserSummary struct {
	UserID       string     `json:"user_id"`
	Username     string     `json:"username"`
	DisplayName  string     `json:"display_name"`
	Role         string     `json:"role"`
	IsSupervisor bool       `json:"is_supervisor"`
	DisabledAt   *time.Time `json:"disabled_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

type GroupSummary struct {
	GroupID     string `json:"group_id"`
	DisplayName string `json:"display_name"`
}

// MemberSummary deliberately keeps the normal member projection flat so it is
// the same shape as UserSummary. A dangling membership contains only user_id
// and missing=true on the wire.
type MemberSummary struct {
	UserSummary
	Missing bool
}

// MarshalJSON preserves the two distinct member states on the wire: a live
// account has the complete flat UserSummary, while a dangling membership has
// only its stable ID and an explicit missing marker.
func (member MemberSummary) MarshalJSON() ([]byte, error) {
	if member.Missing {
		return json.Marshal(struct {
			UserID  string `json:"user_id"`
			Missing bool   `json:"missing"`
		}{
			UserID:  member.UserID,
			Missing: true,
		})
	}
	return json.Marshal(member.UserSummary)
}

type CreateUserInput struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

// Repository is the persistence boundary for the access-management service.
// Every method that changes state re-checks the supervisor inside its own
// transaction; the explicit authorization methods are also used before body
// parsing so ordinary admins cannot learn validation or resource details.
type Repository interface {
	AuthorizeSupervisor(context.Context, string) error
	AuthorizeActiveUser(context.Context, string) error

	ListUsers(context.Context, string) ([]UserSummary, error)
	CreateUser(context.Context, string, auth.User) (UserSummary, error)
	SetUserEnabled(context.Context, string, string, bool, time.Time) (UserSummary, error)
	ResetUserPassword(context.Context, string, string, string, time.Time) error

	ListGroups(context.Context, string) ([]GroupSummary, error)
	CreateGroup(context.Context, string, string, string, time.Time) (GroupSummary, error)
	RenameGroup(context.Context, string, string, string, time.Time) (GroupSummary, error)
	ListMembers(context.Context, string, string) ([]MemberSummary, error)
	SetMember(context.Context, string, string, string, bool, time.Time) error
	ListMyGroups(context.Context, string) ([]GroupSummary, error)
}
