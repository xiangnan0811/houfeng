package handlers

import (
	"errors"
	"net/http"
	"strings"

	"houfeng/internal/center/accessadmin"
	"houfeng/internal/center/http/sessionctx"
)

const (
	accessAdminUsersPath  = "/api/admin/users"
	accessAdminGroupsPath = "/api/admin/record-access-groups"
	accessMineGroupsPath  = "/api/record-access-groups/mine"
)

type accessAdminRoute int

const (
	accessRouteUnknown accessAdminRoute = iota
	accessRouteUsers
	accessRouteUserState
	accessRouteUserPassword
	accessRouteGroups
	accessRouteGroup
	accessRouteMembers
	accessRouteMember
	accessRouteMineGroups
)

type accessAdminCreateUserRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type accessAdminDisplayNameRequest struct {
	DisplayName string `json:"display_name"`
}

type accessAdminResetPasswordRequest struct {
	Password string `json:"password"`
}

// AccessAdmin serves the account and record-access-group management contract.
// The same handler is registered for each concrete route by the bootstrap; it
// uses PathValue for route parameters and never takes an actor from the URL or
// request body.
func AccessAdmin(service *accessadmin.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		route, ok := accessAdminRouteForRequest(r)
		if !ok {
			writeCodedError(w, http.StatusNotFound, "resource not found", "resource_not_found")
			return
		}

		switch route {
		case accessRouteUsers:
			serveAccessAdminUsers(w, r, service)
		case accessRouteUserState:
			serveAccessAdminUserState(w, r, service)
		case accessRouteUserPassword:
			serveAccessAdminUserPassword(w, r, service)
		case accessRouteGroups:
			serveAccessAdminGroups(w, r, service)
		case accessRouteGroup:
			serveAccessAdminGroup(w, r, service)
		case accessRouteMembers:
			serveAccessAdminMembers(w, r, service)
		case accessRouteMember:
			serveAccessAdminMember(w, r, service)
		case accessRouteMineGroups:
			serveAccessAdminMineGroups(w, r, service)
		default:
			writeCodedError(w, http.StatusNotFound, "resource not found", "resource_not_found")
		}
	})
}

func serveAccessAdminUsers(w http.ResponseWriter, r *http.Request, service *accessadmin.Service) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeAccessAdminMethodNotAllowed(w)
		return
	}
	actorID, ok := requireAccessAdminSupervisor(w, r, service)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		users, err := service.ListUsers(r.Context(), actorID)
		if err != nil {
			writeAccessAdminError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": users})
		return
	}

	var input accessAdminCreateUserRequest
	if err := decodeJSON(r, &input); err != nil {
		writeAccessAdminError(w, accessadmin.ErrInvalidRequest)
		return
	}
	user, err := service.CreateUser(r.Context(), actorID, accessadmin.CreateUserInput{
		Username: input.Username, Password: input.Password, DisplayName: input.DisplayName,
	})
	if err != nil {
		writeAccessAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func serveAccessAdminUserState(w http.ResponseWriter, r *http.Request, service *accessadmin.Service) {
	if r.Method != http.MethodPost {
		writeAccessAdminMethodNotAllowed(w)
		return
	}
	actorID, ok := requireAccessAdminSupervisor(w, r, service)
	if !ok {
		return
	}
	if err := decodeAccessAdminEmptyObject(r); err != nil {
		writeAccessAdminError(w, accessadmin.ErrInvalidRequest)
		return
	}
	targetID := accessAdminPathValue(r, "id")
	enabled := strings.HasSuffix(r.URL.Path, "/enable")
	user, err := service.SetUserEnabled(r.Context(), actorID, targetID, enabled)
	if err != nil {
		writeAccessAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func serveAccessAdminUserPassword(w http.ResponseWriter, r *http.Request, service *accessadmin.Service) {
	if r.Method != http.MethodPost {
		writeAccessAdminMethodNotAllowed(w)
		return
	}
	actorID, ok := requireAccessAdminSupervisor(w, r, service)
	if !ok {
		return
	}
	var input accessAdminResetPasswordRequest
	if err := decodeJSON(r, &input); err != nil {
		writeAccessAdminError(w, accessadmin.ErrInvalidRequest)
		return
	}
	targetID := accessAdminPathValue(r, "id")
	if err := service.ResetUserPassword(r.Context(), actorID, targetID, input.Password); err != nil {
		writeAccessAdminError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func serveAccessAdminGroups(w http.ResponseWriter, r *http.Request, service *accessadmin.Service) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeAccessAdminMethodNotAllowed(w)
		return
	}
	actorID, ok := requireAccessAdminSupervisor(w, r, service)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		groups, err := service.ListGroups(r.Context(), actorID)
		if err != nil {
			writeAccessAdminError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": groups})
		return
	}
	var input accessAdminDisplayNameRequest
	if err := decodeJSON(r, &input); err != nil {
		writeAccessAdminError(w, accessadmin.ErrInvalidRequest)
		return
	}
	group, err := service.CreateGroup(r.Context(), actorID, input.DisplayName)
	if err != nil {
		writeAccessAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, group)
}

func serveAccessAdminGroup(w http.ResponseWriter, r *http.Request, service *accessadmin.Service) {
	if r.Method != http.MethodPatch {
		writeAccessAdminMethodNotAllowed(w)
		return
	}
	actorID, ok := requireAccessAdminSupervisor(w, r, service)
	if !ok {
		return
	}
	var input accessAdminDisplayNameRequest
	if err := decodeJSON(r, &input); err != nil {
		writeAccessAdminError(w, accessadmin.ErrInvalidRequest)
		return
	}
	groupID := accessAdminPathValue(r, "id")
	group, err := service.RenameGroup(r.Context(), actorID, groupID, input.DisplayName)
	if err != nil {
		writeAccessAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, group)
}

func serveAccessAdminMembers(w http.ResponseWriter, r *http.Request, service *accessadmin.Service) {
	if r.Method != http.MethodGet {
		writeAccessAdminMethodNotAllowed(w)
		return
	}
	actorID, ok := requireAccessAdminSupervisor(w, r, service)
	if !ok {
		return
	}
	groupID := accessAdminPathValue(r, "id")
	members, err := service.ListMembers(r.Context(), actorID, groupID)
	if err != nil {
		writeAccessAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": members})
}

func serveAccessAdminMember(w http.ResponseWriter, r *http.Request, service *accessadmin.Service) {
	if r.Method != http.MethodPut && r.Method != http.MethodDelete {
		writeAccessAdminMethodNotAllowed(w)
		return
	}
	actorID, ok := requireAccessAdminSupervisor(w, r, service)
	if !ok {
		return
	}
	if r.Method == http.MethodPut {
		if err := decodeAccessAdminEmptyObject(r); err != nil {
			writeAccessAdminError(w, accessadmin.ErrInvalidRequest)
			return
		}
	}
	groupID := accessAdminPathValue(r, "id")
	userID := accessAdminPathValue(r, "user_id")
	if err := service.SetMember(r.Context(), actorID, groupID, userID, r.Method == http.MethodPut); err != nil {
		writeAccessAdminError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func serveAccessAdminMineGroups(w http.ResponseWriter, r *http.Request, service *accessadmin.Service) {
	if r.Method != http.MethodGet {
		writeAccessAdminMethodNotAllowed(w)
		return
	}
	actorID, ok := requireAccessAdminActiveUser(w, r, service)
	if !ok {
		return
	}
	groups, err := service.ListMyGroups(r.Context(), actorID)
	if err != nil {
		writeAccessAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": groups})
}

func requireAccessAdminSupervisor(w http.ResponseWriter, r *http.Request, service *accessadmin.Service) (string, bool) {
	actorID, ok := sessionctx.UserIDFromContext(r.Context())
	if !ok || strings.TrimSpace(actorID) == "" {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return "", false
	}
	if err := service.AuthorizeSupervisor(r.Context(), actorID); err != nil {
		writeAccessAdminError(w, err)
		return "", false
	}
	return actorID, true
}

func requireAccessAdminActiveUser(w http.ResponseWriter, r *http.Request, service *accessadmin.Service) (string, bool) {
	actorID, ok := sessionctx.UserIDFromContext(r.Context())
	if !ok || strings.TrimSpace(actorID) == "" {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return "", false
	}
	if err := service.AuthorizeActiveUser(r.Context(), actorID); err != nil {
		writeAccessAdminError(w, err)
		return "", false
	}
	return actorID, true
}

func decodeAccessAdminEmptyObject(r *http.Request) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
	var input struct{}
	return decodeJSON(r, &input)
}

func accessAdminRouteForRequest(r *http.Request) (accessAdminRoute, bool) {
	path := r.URL.Path
	switch path {
	case accessAdminUsersPath:
		return accessRouteUsers, true
	case accessAdminGroupsPath:
		return accessRouteGroups, true
	case accessMineGroupsPath:
		return accessRouteMineGroups, true
	}
	if strings.HasPrefix(path, accessAdminUsersPath+"/") {
		switch {
		case strings.HasSuffix(path, "/disable"), strings.HasSuffix(path, "/enable"):
			return accessRouteUserState, true
		case strings.HasSuffix(path, "/reset-password"):
			return accessRouteUserPassword, true
		default:
			return accessRouteUnknown, false
		}
	}
	if strings.HasPrefix(path, accessAdminGroupsPath+"/") {
		switch {
		case strings.HasSuffix(path, "/members") && !strings.HasSuffix(path, "/members/"):
			return accessRouteMembers, true
		case strings.Contains(path, "/members/"):
			return accessRouteMember, true
		default:
			return accessRouteGroup, true
		}
	}
	return accessRouteUnknown, false
}

func accessAdminPathValue(r *http.Request, name string) string {
	return r.PathValue(name)
}

func writeAccessAdminMethodNotAllowed(w http.ResponseWriter) {
	writeCodedError(w, http.StatusMethodNotAllowed, "method not allowed", "method_not_allowed")
}

func writeAccessAdminError(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		return
	case errors.Is(err, accessadmin.ErrInvalidRequest):
		writeCodedError(w, http.StatusBadRequest, "invalid request", "invalid_request")
	case errors.Is(err, accessadmin.ErrManagementForbidden):
		writeCodedError(w, http.StatusForbidden, "management access forbidden", "management_forbidden")
	case errors.Is(err, accessadmin.ErrResourceNotFound):
		writeCodedError(w, http.StatusNotFound, "resource not found", "resource_not_found")
	case errors.Is(err, accessadmin.ErrUsernameTaken):
		writeCodedError(w, http.StatusConflict, "username is already taken", "username_taken")
	case errors.Is(err, accessadmin.ErrGroupNameTaken):
		writeCodedError(w, http.StatusConflict, "record access-group name is already taken", "group_name_taken")
	case errors.Is(err, accessadmin.ErrSupervisorProtected):
		writeCodedError(w, http.StatusConflict, "supervisor account is protected", "supervisor_protected")
	case errors.Is(err, accessadmin.ErrUserDisabled):
		writeCodedError(w, http.StatusConflict, "user is disabled", "user_disabled")
	default:
		writeCodedError(w, http.StatusServiceUnavailable, "management service unavailable", "management_unavailable")
	}
}
