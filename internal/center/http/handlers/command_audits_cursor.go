package handlers

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"houfeng/internal/contracts/agentapi"
)

const (
	commandAuditCursorVersion   = 1
	commandAuditCursorMaxLength = 8192
	commandAuditFilterMaxLength = 256
)

type commandAuditCursorFilters struct {
	Window             string `json:"window"`
	MonitoringInstance string `json:"monitoring_instance,omitempty"`
	CommandID          string `json:"command_id,omitempty"`
	Sensitivity        string `json:"sensitivity,omitempty"`
	Outcome            string `json:"outcome,omitempty"`
	Actor              string `json:"actor,omitempty"`
	ActionID           string `json:"action_id,omitempty"`
}

type commandAuditCursorState struct {
	Version         int                       `json:"v"`
	Filters         commandAuditCursorFilters `json:"filters"`
	StartedFrom     *time.Time                `json:"started_from"`
	StartedTo       time.Time                 `json:"started_to"`
	Limit           int                       `json:"limit"`
	BeforeStartedAt time.Time                 `json:"before_started_at"`
	BeforeID        string                    `json:"before_id"`
}

func encodeCommandAuditCursor(state commandAuditCursorState) (string, error) {
	raw, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("encode command audit cursor payload: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeCommandAuditCursor(encoded string) (commandAuditCursorState, error) {
	if encoded == "" || len(encoded) > commandAuditCursorMaxLength {
		return commandAuditCursorState{}, fmt.Errorf("invalid command audit cursor length")
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return commandAuditCursorState{}, fmt.Errorf("decode command audit cursor: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var state commandAuditCursorState
	if err := decoder.Decode(&state); err != nil {
		return commandAuditCursorState{}, fmt.Errorf("decode command audit cursor payload: %w", err)
	}
	if err := ensureCommandAuditCursorEOF(decoder); err != nil {
		return commandAuditCursorState{}, err
	}
	if err := validateCommandAuditCursor(state); err != nil {
		return commandAuditCursorState{}, err
	}
	return state, nil
}

func ensureCommandAuditCursorEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("command audit cursor has trailing payload")
		}
		return fmt.Errorf("decode trailing command audit cursor payload: %w", err)
	}
	return nil
}

func validateCommandAuditCursor(state commandAuditCursorState) error {
	if state.Version != commandAuditCursorVersion {
		return fmt.Errorf("unsupported command audit cursor version")
	}
	if state.StartedTo.IsZero() || state.Limit < 1 || state.Limit > 100 || state.BeforeStartedAt.IsZero() || !validCommandAuditFilterText(state.BeforeID) {
		return fmt.Errorf("invalid command audit cursor pagination state")
	}
	if state.BeforeStartedAt.After(state.StartedTo) {
		return fmt.Errorf("invalid command audit cursor last key")
	}
	if err := validateCommandAuditCursorFilters(state.Filters); err != nil {
		return err
	}

	switch state.Filters.Window {
	case "24h":
		if !commandAuditCursorHasDuration(state, 24*time.Hour) {
			return fmt.Errorf("invalid 24h command audit cursor bounds")
		}
	case "7d":
		if !commandAuditCursorHasDuration(state, 7*24*time.Hour) {
			return fmt.Errorf("invalid 7d command audit cursor bounds")
		}
	case "30d":
		if !commandAuditCursorHasDuration(state, 30*24*time.Hour) {
			return fmt.Errorf("invalid 30d command audit cursor bounds")
		}
	case "all":
		if state.StartedFrom != nil {
			return fmt.Errorf("invalid all-window command audit cursor bounds")
		}
	case "custom":
		if state.StartedFrom == nil || !state.StartedFrom.Before(state.StartedTo) {
			return fmt.Errorf("invalid custom command audit cursor bounds")
		}
	default:
		return fmt.Errorf("invalid command audit cursor window")
	}
	if state.StartedFrom != nil && state.BeforeStartedAt.Before(*state.StartedFrom) {
		return fmt.Errorf("command audit cursor last key is outside fixed bounds")
	}
	return nil
}

func commandAuditCursorHasDuration(state commandAuditCursorState, duration time.Duration) bool {
	return state.StartedFrom != nil && state.StartedFrom.Before(state.StartedTo) && state.StartedTo.Sub(*state.StartedFrom) == duration
}

func validateCommandAuditCursorFilters(filters commandAuditCursorFilters) error {
	for _, value := range []string{filters.MonitoringInstance, filters.Actor, filters.ActionID} {
		if value != strings.TrimSpace(value) || (value != "" && !validCommandAuditFilterText(value)) {
			return fmt.Errorf("invalid command audit cursor filter")
		}
	}
	if filters.CommandID != strings.TrimSpace(filters.CommandID) || filters.Sensitivity != strings.TrimSpace(filters.Sensitivity) || filters.Outcome != strings.TrimSpace(filters.Outcome) {
		return fmt.Errorf("invalid command audit cursor enum filter")
	}
	if filters.CommandID != "" {
		if _, ok := agentapi.SensitivityForCommand(filters.CommandID); !ok {
			return fmt.Errorf("invalid command audit cursor command")
		}
	}
	if filters.Sensitivity != "" && filters.Sensitivity != "standard" && filters.Sensitivity != "sensitive" {
		return fmt.Errorf("invalid command audit cursor sensitivity")
	}
	switch filters.Outcome {
	case "", "rejected", "queued", "dispatched", "succeeded", "failed":
	default:
		return fmt.Errorf("invalid command audit cursor outcome")
	}
	return nil
}

func validCommandAuditFilterText(value string) bool {
	return value != "" && len(value) <= commandAuditFilterMaxLength && !strings.ContainsRune(value, '\x00')
}
