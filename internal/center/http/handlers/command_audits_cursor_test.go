package handlers

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCommandAuditCursorRawURLRoundTrip(t *testing.T) {
	t.Parallel()

	state := commandAuditCursorState{
		Version: 1,
		Filters: commandAuditCursorFilters{
			Window:             "custom",
			MonitoringInstance: "Tokyo",
			CommandID:          "uptime",
			Sensitivity:        "standard",
			Outcome:            "succeeded",
			Actor:              "admin",
			ActionID:           "act_001",
		},
		StartedFrom:     timePtr(time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)),
		StartedTo:       time.Date(2026, time.July, 2, 0, 0, 0, 0, time.UTC),
		Limit:           50,
		BeforeStartedAt: time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC),
		BeforeID:        "act_001",
	}
	encoded, err := encodeCommandAuditCursor(state)
	if err != nil {
		t.Fatalf("encodeCommandAuditCursor() error = %v", err)
	}
	if strings.ContainsAny(encoded, "+/=") {
		t.Fatalf("cursor = %q, want unpadded URL-safe base64", encoded)
	}
	decoded, err := decodeCommandAuditCursor(encoded)
	if err != nil {
		t.Fatalf("decodeCommandAuditCursor() error = %v", err)
	}
	if !reflect.DeepEqual(decoded, state) {
		t.Fatalf("decoded = %#v, want %#v", decoded, state)
	}
}

func TestCommandAuditCursorRejectsCorruptUnknownAndInvalidPayloads(t *testing.T) {
	t.Parallel()

	valid := commandAuditCursorState{
		Version:         1,
		Filters:         commandAuditCursorFilters{Window: "30d"},
		StartedFrom:     timePtr(time.Date(2026, time.June, 12, 0, 0, 0, 0, time.UTC)),
		StartedTo:       time.Date(2026, time.July, 12, 0, 0, 0, 0, time.UTC),
		Limit:           20,
		BeforeStartedAt: time.Date(2026, time.July, 11, 0, 0, 0, 0, time.UTC),
		BeforeID:        "act_001",
	}
	unknownJSON, err := json.Marshal(map[string]any{
		"v":                 1,
		"filters":           map[string]any{"window": "30d"},
		"started_from":      "2026-06-12T00:00:00Z",
		"started_to":        "2026-07-12T00:00:00Z",
		"limit":             20,
		"before_started_at": "2026-07-11T00:00:00Z",
		"before_id":         "act_001",
		"unknown":           true,
	})
	if err != nil {
		t.Fatalf("marshal unknown cursor: %v", err)
	}
	tests := []struct {
		name   string
		cursor string
	}{
		{name: "invalid base64", cursor: "%%%"},
		{name: "padded base64", cursor: base64.URLEncoding.EncodeToString([]byte(`{"v":1}`))},
		{name: "unknown json field", cursor: base64.RawURLEncoding.EncodeToString(unknownJSON)},
		{name: "unknown version", cursor: mustEncodeCommandAuditCursor(t, withCommandAuditCursorVersion(valid, 2))},
		{name: "bad window", cursor: mustEncodeCommandAuditCursor(t, withCommandAuditCursorWindow(valid, "tomorrow"))},
		{name: "bad fixed duration", cursor: mustEncodeCommandAuditCursor(t, withCommandAuditCursorStartedFrom(valid, timePtr(valid.StartedFrom.Add(time.Hour))))},
		{name: "bad limit", cursor: mustEncodeCommandAuditCursor(t, withCommandAuditCursorLimit(valid, 101))},
		{name: "missing before id", cursor: mustEncodeCommandAuditCursor(t, withCommandAuditCursorBeforeID(valid, ""))},
		{name: "before outside upper bound", cursor: mustEncodeCommandAuditCursor(t, withCommandAuditCursorBeforeStartedAt(valid, valid.StartedTo.Add(time.Second)))},
		{name: "unknown command", cursor: mustEncodeCommandAuditCursor(t, withCommandAuditCursorCommandID(valid, "unknown_command"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := decodeCommandAuditCursor(tt.cursor); err == nil {
				t.Fatalf("decodeCommandAuditCursor(%q) error = nil", tt.cursor)
			}
		})
	}
}

func mustEncodeCommandAuditCursor(t *testing.T, state commandAuditCursorState) string {
	t.Helper()
	encoded, err := encodeCommandAuditCursor(state)
	if err != nil {
		t.Fatalf("encode command audit cursor: %v", err)
	}
	return encoded
}

func withCommandAuditCursorVersion(state commandAuditCursorState, value int) commandAuditCursorState {
	state.Version = value
	return state
}

func withCommandAuditCursorWindow(state commandAuditCursorState, value string) commandAuditCursorState {
	state.Filters.Window = value
	return state
}

func withCommandAuditCursorStartedFrom(state commandAuditCursorState, value *time.Time) commandAuditCursorState {
	state.StartedFrom = value
	return state
}

func withCommandAuditCursorLimit(state commandAuditCursorState, value int) commandAuditCursorState {
	state.Limit = value
	return state
}

func withCommandAuditCursorBeforeID(state commandAuditCursorState, value string) commandAuditCursorState {
	state.BeforeID = value
	return state
}

func withCommandAuditCursorBeforeStartedAt(state commandAuditCursorState, value time.Time) commandAuditCursorState {
	state.BeforeStartedAt = value
	return state
}

func withCommandAuditCursorCommandID(state commandAuditCursorState, value string) commandAuditCursorState {
	state.Filters.CommandID = value
	return state
}
