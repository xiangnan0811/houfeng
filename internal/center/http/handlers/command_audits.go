package handlers

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"houfeng/internal/center/commandaudits"
	"houfeng/internal/contracts/agentapi"
)

type commandAuditRepository interface {
	ListCommandAudits(context.Context, commandaudits.Query) (commandaudits.Page, error)
}

type CommandAuditOptions struct {
	Now func() time.Time
}

func CommandAudits(repo commandAuditRepository) http.Handler {
	return CommandAuditsWithOptions(repo, CommandAuditOptions{})
}

func CommandAuditsWithOptions(repo commandAuditRepository, opts CommandAuditOptions) http.Handler {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		query, cursorState, err := parseCommandAuditRequest(r.URL.Query(), now)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		page, err := repo.ListCommandAudits(r.Context(), query)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		response := commandAuditListResponse{
			Items: mapCommandAuditActions(page.Items),
		}
		if page.HasMore {
			if len(page.Items) == 0 {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			last := page.Items[len(page.Items)-1]
			if last.ID == "" || last.StartedAt.IsZero() {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			cursorState.BeforeStartedAt = last.StartedAt.UTC()
			cursorState.BeforeID = last.ID
			response.NextCursor, err = encodeCommandAuditCursor(cursorState)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
		}
		writeJSON(w, http.StatusOK, response)
	})
}

type commandAuditListResponse struct {
	Items      []commandAuditActionResponse `json:"items"`
	NextCursor string                       `json:"next_cursor,omitempty"`
}

type commandAuditActionResponse struct {
	ID                 string                                   `json:"id"`
	ActionID           string                                   `json:"action_id,omitempty"`
	MonitoringInstance commandaudits.MonitoringInstanceIdentity `json:"monitoring_instance"`
	CommandID          string                                   `json:"command_id"`
	Sensitivity        string                                   `json:"sensitivity"`
	Outcome            string                                   `json:"outcome"`
	Actor              *commandaudits.ActorIdentity             `json:"actor"`
	StartedAt          time.Time                                `json:"started_at"`
	Events             []commandAuditEventResponse              `json:"events"`
}

type commandAuditEventResponse struct {
	AuditID         string    `json:"audit_id"`
	EventType       string    `json:"event_type"`
	Source          string    `json:"source"`
	OccurredAt      time.Time `json:"occurred_at"`
	ExitCode        *int      `json:"exit_code,omitempty"`
	RejectionReason string    `json:"rejection_reason,omitempty"`
}

func mapCommandAuditActions(actions []commandaudits.Action) []commandAuditActionResponse {
	result := make([]commandAuditActionResponse, 0, len(actions))
	for _, action := range actions {
		events := make([]commandAuditEventResponse, 0, len(action.Events))
		for _, event := range action.Events {
			mapped := commandAuditEventResponse{
				AuditID:    event.AuditID,
				EventType:  event.EventType,
				Source:     event.Source,
				OccurredAt: event.OccurredAt,
				ExitCode:   event.ExitCode,
			}
			if event.EventType == "rejected" && event.RejectionReason == commandaudits.RejectionReasonSensitiveConfirmationRequired {
				mapped.RejectionReason = event.RejectionReason
			}
			events = append(events, mapped)
		}
		result = append(result, commandAuditActionResponse{
			ID:                 action.ID,
			ActionID:           action.ActionID,
			MonitoringInstance: action.MonitoringInstance,
			CommandID:          action.CommandID,
			Sensitivity:        action.Sensitivity,
			Outcome:            action.Outcome,
			Actor:              action.Actor,
			StartedAt:          action.StartedAt,
			Events:             events,
		})
	}
	return result
}

func parseCommandAuditRequest(values url.Values, now func() time.Time) (commandaudits.Query, commandAuditCursorState, error) {
	if err := validateCommandAuditQueryKeys(values); err != nil {
		return commandaudits.Query{}, commandAuditCursorState{}, err
	}
	if cursorValues, ok := values["cursor"]; ok {
		if len(values) != 1 || len(cursorValues) != 1 {
			return commandaudits.Query{}, commandAuditCursorState{}, strconv.ErrSyntax
		}
		state, err := decodeCommandAuditCursor(cursorValues[0])
		if err != nil {
			return commandaudits.Query{}, commandAuditCursorState{}, err
		}
		return commandAuditQueryFromCursor(state), state, nil
	}

	fixedNow := now().UTC()
	filters, err := parseInitialCommandAuditFilters(values)
	if err != nil {
		return commandaudits.Query{}, commandAuditCursorState{}, err
	}
	limit, err := parseCommandAuditLimit(values)
	if err != nil {
		return commandaudits.Query{}, commandAuditCursorState{}, err
	}
	startedFrom, startedTo, err := parseCommandAuditBounds(values, filters.Window, fixedNow)
	if err != nil {
		return commandaudits.Query{}, commandAuditCursorState{}, err
	}
	state := commandAuditCursorState{
		Version:     commandAuditCursorVersion,
		Filters:     filters,
		StartedFrom: startedFrom,
		StartedTo:   startedTo,
		Limit:       limit,
	}
	return commandAuditQueryFromCursor(state), state, nil
}

func validateCommandAuditQueryKeys(values url.Values) error {
	allowed := map[string]struct{}{
		"window": {}, "started_from": {}, "started_to": {}, "monitoring_instance": {},
		"command_id": {}, "sensitivity": {}, "outcome": {}, "actor": {}, "action_id": {},
		"limit": {}, "cursor": {},
	}
	for key, entries := range values {
		if _, ok := allowed[key]; !ok || len(entries) != 1 {
			return strconv.ErrSyntax
		}
	}
	return nil
}

func parseInitialCommandAuditFilters(values url.Values) (commandAuditCursorFilters, error) {
	window := strings.TrimSpace(values.Get("window"))
	if window == "" {
		window = "30d"
	}
	filters := commandAuditCursorFilters{
		Window:             window,
		MonitoringInstance: strings.TrimSpace(values.Get("monitoring_instance")),
		CommandID:          strings.TrimSpace(values.Get("command_id")),
		Sensitivity:        strings.TrimSpace(values.Get("sensitivity")),
		Outcome:            strings.TrimSpace(values.Get("outcome")),
		Actor:              strings.TrimSpace(values.Get("actor")),
		ActionID:           strings.TrimSpace(values.Get("action_id")),
	}
	if filters.Window != "24h" && filters.Window != "7d" && filters.Window != "30d" && filters.Window != "all" && filters.Window != "custom" {
		return commandAuditCursorFilters{}, strconv.ErrSyntax
	}
	for _, value := range []string{filters.MonitoringInstance, filters.Actor, filters.ActionID} {
		if value != "" && !validCommandAuditFilterText(value) {
			return commandAuditCursorFilters{}, strconv.ErrSyntax
		}
	}
	if filters.CommandID != "" {
		if _, ok := agentapi.SensitivityForCommand(filters.CommandID); !ok {
			return commandAuditCursorFilters{}, strconv.ErrSyntax
		}
	}
	if filters.Sensitivity != "" && filters.Sensitivity != "standard" && filters.Sensitivity != "sensitive" {
		return commandAuditCursorFilters{}, strconv.ErrSyntax
	}
	switch filters.Outcome {
	case "", "rejected", "queued", "dispatched", "succeeded", "failed":
	default:
		return commandAuditCursorFilters{}, strconv.ErrSyntax
	}
	return filters, nil
}

func parseCommandAuditLimit(values url.Values) (int, error) {
	raw, ok := values["limit"]
	if !ok {
		return 20, nil
	}
	limit, err := strconv.Atoi(strings.TrimSpace(raw[0]))
	if err != nil || limit < 1 || limit > 100 {
		return 0, strconv.ErrSyntax
	}
	return limit, nil
}

func parseCommandAuditBounds(values url.Values, window string, fixedNow time.Time) (*time.Time, time.Time, error) {
	_, hasFrom := values["started_from"]
	_, hasTo := values["started_to"]
	if window != "custom" {
		if hasFrom || hasTo {
			return nil, time.Time{}, strconv.ErrSyntax
		}
		switch window {
		case "24h":
			from := fixedNow.Add(-24 * time.Hour)
			return &from, fixedNow, nil
		case "7d":
			from := fixedNow.Add(-7 * 24 * time.Hour)
			return &from, fixedNow, nil
		case "30d":
			from := fixedNow.Add(-30 * 24 * time.Hour)
			return &from, fixedNow, nil
		case "all":
			return nil, fixedNow, nil
		}
	}
	if !hasFrom || !hasTo {
		return nil, time.Time{}, strconv.ErrSyntax
	}
	from, err := time.Parse(time.RFC3339, strings.TrimSpace(values.Get("started_from")))
	if err != nil {
		return nil, time.Time{}, strconv.ErrSyntax
	}
	to, err := time.Parse(time.RFC3339, strings.TrimSpace(values.Get("started_to")))
	if err != nil || !from.Before(to) {
		return nil, time.Time{}, strconv.ErrSyntax
	}
	from = from.UTC()
	return &from, to.UTC(), nil
}

func commandAuditQueryFromCursor(state commandAuditCursorState) commandaudits.Query {
	return commandaudits.Query{
		StartedFrom:        state.StartedFrom,
		StartedTo:          state.StartedTo.UTC(),
		MonitoringInstance: state.Filters.MonitoringInstance,
		CommandID:          state.Filters.CommandID,
		Sensitivity:        state.Filters.Sensitivity,
		Outcome:            state.Filters.Outcome,
		Actor:              state.Filters.Actor,
		ActionID:           state.Filters.ActionID,
		Limit:              state.Limit,
		BeforeStartedAt:    commandAuditCursorBeforeTime(state.BeforeStartedAt),
		BeforeID:           state.BeforeID,
	}
}

func commandAuditCursorBeforeTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	value = value.UTC()
	return &value
}
