package accessadmin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"houfeng/internal/center/auth"
	"houfeng/internal/center/ids"
)

const (
	maxDisplayNameLength = 100
	userIDPrefix         = "usr_"
	groupIDPrefix        = "rag_"
)

type Service struct {
	repo               Repository
	now                func() time.Time
	passwordBcryptCost int
}

func New(repo Repository, opts Options) *Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	cost := opts.PasswordBcryptCost
	if cost == 0 {
		cost = auth.DefaultPasswordBcryptCost
	}
	return &Service{repo: repo, now: now, passwordBcryptCost: cost}
}

// AuthorizeSupervisor is intentionally exposed for HTTP handlers. Handlers
// call it before decoding request bodies so a normal admin receives the same
// 403 regardless of malformed input or target existence.
func (s *Service) AuthorizeSupervisor(ctx context.Context, actorID string) error {
	if s == nil || s.repo == nil {
		return ErrManagementUnavailable
	}
	if strings.TrimSpace(actorID) == "" {
		return ErrManagementForbidden
	}
	return mapRepositoryError(s.repo.AuthorizeSupervisor(ctx, actorID))
}

func (s *Service) AuthorizeActiveUser(ctx context.Context, actorID string) error {
	if s == nil || s.repo == nil {
		return ErrManagementUnavailable
	}
	if strings.TrimSpace(actorID) == "" {
		return ErrManagementForbidden
	}
	return mapRepositoryError(s.repo.AuthorizeActiveUser(ctx, actorID))
}

func (s *Service) ListUsers(ctx context.Context, actorID string) ([]UserSummary, error) {
	if err := s.AuthorizeSupervisor(ctx, actorID); err != nil {
		return nil, err
	}
	users, err := s.repo.ListUsers(ctx, actorID)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	if users == nil {
		users = []UserSummary{}
	}
	return users, nil
}

func (s *Service) CreateUser(ctx context.Context, actorID string, input CreateUserInput) (UserSummary, error) {
	if err := s.AuthorizeSupervisor(ctx, actorID); err != nil {
		return UserSummary{}, err
	}
	username, err := normalizeUsername(input.Username)
	if err != nil {
		return UserSummary{}, err
	}
	passwordHash, err := auth.HashPasswordWithCost(input.Password, s.passwordBcryptCost)
	if err != nil {
		return UserSummary{}, mapPasswordHashError(err)
	}
	userID, err := auth.NewUserID()
	if err != nil {
		return UserSummary{}, fmt.Errorf("%w: create user id", ErrManagementUnavailable)
	}
	displayName, err := normalizeDisplayName(input.DisplayName)
	if err != nil {
		return UserSummary{}, err
	}
	if displayName == "" {
		displayName = username
	}
	now := s.now().UTC()
	user, err := s.repo.CreateUser(ctx, actorID, auth.User{
		UserID:            userID,
		Username:          username,
		PasswordHash:      passwordHash,
		DisplayName:       displayName,
		Role:              auth.RoleAdmin,
		IsSupervisor:      false,
		CreatedAt:         now,
		PasswordChangedAt: now,
	})
	if err != nil {
		return UserSummary{}, mapRepositoryError(err)
	}
	return user, nil
}

func (s *Service) SetUserEnabled(ctx context.Context, actorID, targetID string, enabled bool) (UserSummary, error) {
	if err := s.AuthorizeSupervisor(ctx, actorID); err != nil {
		return UserSummary{}, err
	}
	if !validUserID(strings.TrimSpace(targetID)) {
		return UserSummary{}, fmt.Errorf("%w: user_id", ErrInvalidRequest)
	}
	user, err := s.repo.SetUserEnabled(ctx, actorID, strings.TrimSpace(targetID), enabled, s.now().UTC())
	if err != nil {
		return UserSummary{}, mapRepositoryError(err)
	}
	return user, nil
}

func (s *Service) ResetUserPassword(ctx context.Context, actorID, targetID, password string) error {
	if err := s.AuthorizeSupervisor(ctx, actorID); err != nil {
		return err
	}
	if !validUserID(strings.TrimSpace(targetID)) {
		return fmt.Errorf("%w: user_id", ErrInvalidRequest)
	}
	newHash, err := auth.HashPasswordWithCost(password, s.passwordBcryptCost)
	if err != nil {
		return mapPasswordHashError(err)
	}
	if err := s.repo.ResetUserPassword(ctx, actorID, strings.TrimSpace(targetID), newHash, s.now().UTC()); err != nil {
		return mapRepositoryError(err)
	}
	return nil
}

func (s *Service) ListGroups(ctx context.Context, actorID string) ([]GroupSummary, error) {
	if err := s.AuthorizeSupervisor(ctx, actorID); err != nil {
		return nil, err
	}
	groups, err := s.repo.ListGroups(ctx, actorID)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	if groups == nil {
		groups = []GroupSummary{}
	}
	return groups, nil
}

func (s *Service) CreateGroup(ctx context.Context, actorID, displayName string) (GroupSummary, error) {
	if err := s.AuthorizeSupervisor(ctx, actorID); err != nil {
		return GroupSummary{}, err
	}
	displayName, err := normalizeDisplayName(displayName)
	if err != nil {
		return GroupSummary{}, err
	}
	if displayName == "" {
		return GroupSummary{}, fmt.Errorf("%w: display_name", ErrInvalidRequest)
	}
	groupID, err := ids.New("rag")
	if err != nil {
		return GroupSummary{}, fmt.Errorf("%w: create group id", ErrManagementUnavailable)
	}
	group, err := s.repo.CreateGroup(ctx, actorID, groupID, displayName, s.now().UTC())
	if err != nil {
		return GroupSummary{}, mapRepositoryError(err)
	}
	return group, nil
}

func (s *Service) RenameGroup(ctx context.Context, actorID, groupID, displayName string) (GroupSummary, error) {
	if err := s.AuthorizeSupervisor(ctx, actorID); err != nil {
		return GroupSummary{}, err
	}
	groupID = strings.TrimSpace(groupID)
	if !validGroupID(groupID) {
		return GroupSummary{}, fmt.Errorf("%w: group_id", ErrInvalidRequest)
	}
	normalizedDisplayName, err := normalizeDisplayName(displayName)
	displayName = normalizedDisplayName
	if err != nil {
		return GroupSummary{}, err
	}
	if displayName == "" {
		return GroupSummary{}, fmt.Errorf("%w: display_name", ErrInvalidRequest)
	}
	group, err := s.repo.RenameGroup(ctx, actorID, groupID, displayName, s.now().UTC())
	if err != nil {
		return GroupSummary{}, mapRepositoryError(err)
	}
	return group, nil
}

func (s *Service) ListMembers(ctx context.Context, actorID, groupID string) ([]MemberSummary, error) {
	if err := s.AuthorizeSupervisor(ctx, actorID); err != nil {
		return nil, err
	}
	groupID = strings.TrimSpace(groupID)
	if !validGroupID(groupID) {
		return nil, fmt.Errorf("%w: group_id", ErrInvalidRequest)
	}
	members, err := s.repo.ListMembers(ctx, actorID, groupID)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	if members == nil {
		members = []MemberSummary{}
	}
	return members, nil
}

func (s *Service) SetMember(ctx context.Context, actorID, groupID, userID string, present bool) error {
	if err := s.AuthorizeSupervisor(ctx, actorID); err != nil {
		return err
	}
	groupID = strings.TrimSpace(groupID)
	userID = strings.TrimSpace(userID)
	if !validGroupID(groupID) {
		return fmt.Errorf("%w: group_id", ErrInvalidRequest)
	}
	if !validUserID(userID) {
		return fmt.Errorf("%w: user_id", ErrInvalidRequest)
	}
	if err := s.repo.SetMember(ctx, actorID, groupID, userID, present, s.now().UTC()); err != nil {
		return mapRepositoryError(err)
	}
	return nil
}

func (s *Service) ListMyGroups(ctx context.Context, actorID string) ([]GroupSummary, error) {
	if err := s.AuthorizeActiveUser(ctx, actorID); err != nil {
		return nil, err
	}
	groups, err := s.repo.ListMyGroups(ctx, actorID)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	if groups == nil {
		groups = []GroupSummary{}
	}
	return groups, nil
}

func normalizeUsername(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) < auth.MinUsernameLength || len(value) > auth.MaxUsernameLength {
		return "", fmt.Errorf("%w: username", ErrInvalidRequest)
	}
	return value, nil
}

func normalizeDisplayName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > maxDisplayNameLength {
		return "", fmt.Errorf("%w: display_name", ErrInvalidRequest)
	}
	return value, nil
}

func validUserID(value string) bool {
	if len(value) != len(userIDPrefix)+24 || !strings.HasPrefix(value, userIDPrefix) {
		return false
	}
	for _, c := range value[len(userIDPrefix):] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func validGroupID(value string) bool {
	if len(value) <= len(groupIDPrefix) || len(value) > len(groupIDPrefix)+64 || !strings.HasPrefix(value, groupIDPrefix) {
		return false
	}
	for _, c := range value[len(groupIDPrefix):] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'z')) {
			return false
		}
	}
	return true
}
func mapPasswordHashError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, auth.ErrPasswordTooShort) ||
		errors.Is(err, auth.ErrPasswordTooLong) ||
		errors.Is(err, auth.ErrPasswordTooWeak) {
		return fmt.Errorf("%w: password", ErrInvalidRequest)
	}
	return fmt.Errorf("%w: password hashing", ErrManagementUnavailable)
}

func mapRepositoryError(err error) error {
	if err == nil {
		return nil
	}
	var known = []error{
		ErrInvalidRequest,
		ErrManagementForbidden,
		ErrManagementUnavailable,
		ErrResourceNotFound,
		ErrUsernameTaken,
		ErrGroupNameTaken,
		ErrSupervisorProtected,
		ErrUserDisabled,
	}
	for _, sentinel := range known {
		if errors.Is(err, sentinel) {
			return err
		}
	}
	if errors.Is(err, auth.ErrUsernameTaken) {
		return fmt.Errorf("%w: username", ErrUsernameTaken)
	}
	if errors.Is(err, auth.ErrUserNotFound) {
		return ErrResourceNotFound
	}
	return fmt.Errorf("%w: %v", ErrManagementUnavailable, err)
}
