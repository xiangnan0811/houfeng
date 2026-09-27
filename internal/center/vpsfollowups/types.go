package vpsfollowups

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrInvalid = errors.New("invalid VPS follow-up")
var ErrNotFound = errors.New("VPS follow-up not found")
var ErrConflict = errors.New("VPS follow-up already closed")

type Record struct {
	FollowupID       string          `json:"followup_id"`
	VPSID            string          `json:"vps_id"`
	Kind             string          `json:"kind"`
	DedupeKey        string          `json:"dedupe_key"`
	Status           string          `json:"status"`
	Summary          string          `json:"summary"`
	Details          json.RawMessage `json:"details"`
	ResolutionReason string          `json:"resolution_reason"`
	ResolvedBy       string          `json:"resolved_by"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	ResolvedAt       *time.Time      `json:"resolved_at"`
}

type CreateInput struct {
	Kind    string          `json:"kind"`
	Summary string          `json:"summary"`
	Details json.RawMessage `json:"details"`
}
type ResolveInput struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func ValidateCreate(input CreateInput) error {
	if strings.TrimSpace(input.Summary) == "" {
		return ErrInvalid
	}
	switch input.Kind {
	case "migration", "potential_charge", "archived_online", "review", "annotation":
	default:
		return ErrInvalid
	}
	details := bytes.TrimSpace(input.Details)
	if len(details) > 0 && (!json.Valid(details) || details[0] != '{') {
		return ErrInvalid
	}
	return nil
}
func ValidateResolve(input ResolveInput) error {
	if (input.Status != "resolved" && input.Status != "ignored") || strings.TrimSpace(input.Reason) == "" {
		return ErrInvalid
	}
	return nil
}

type Repository interface {
	List(context.Context, string) ([]Record, error)
	Create(context.Context, string, CreateInput, string) (Record, error)
	Resolve(context.Context, string, string, ResolveInput, string) (Record, error)
}
