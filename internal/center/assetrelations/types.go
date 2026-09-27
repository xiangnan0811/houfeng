// Package assetrelations describes dated associations; an association never owns
// the service or domain identity and ending it does not retire that identity.
package assetrelations

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrInvalid = errors.New("invalid asset association")
var ErrNotFound = errors.New("asset association or object not found")
var ErrConflict = errors.New("asset association conflict")

const Service = "service"
const Domain = "domain"

type Record struct {
	AssociationID string          `json:"association_id"`
	ObjectID      string          `json:"object_id"`
	VPSID         string          `json:"vps_id"`
	TargetID      *string         `json:"target_id"`
	ServiceID     *string         `json:"service_id"`
	Address       string          `json:"address"`
	Port          *int            `json:"port"`
	StartedAt     time.Time       `json:"started_at"`
	EndedAt       *time.Time      `json:"ended_at"`
	EndReason     string          `json:"end_reason"`
	EndedBy       string          `json:"ended_by"`
	Snapshot      json.RawMessage `json:"snapshot"`
}

type LinkInput struct {
	ObjectID  string  `json:"object_id"`
	TargetID  *string `json:"target_id"`
	ServiceID *string `json:"service_id"`
	Address   string  `json:"address"`
	Port      *int    `json:"port"`
}

func Normalize(input LinkInput) LinkInput {
	input.ObjectID = strings.TrimSpace(input.ObjectID)
	input.Address = strings.TrimSpace(input.Address)
	for _, field := range []**string{&input.TargetID, &input.ServiceID} {
		if *field != nil {
			value := strings.TrimSpace(**field)
			if value == "" {
				*field = nil
			} else {
				*field = &value
			}
		}
	}
	return input
}

func Validate(kind string, input LinkInput) error {
	if input.ObjectID == "" || (kind != Service && kind != Domain) {
		return ErrInvalid
	}
	if input.Port != nil && (*input.Port < 1 || *input.Port > 65535 || kind != Service) {
		return ErrInvalid
	}
	if kind == Service && input.ServiceID != nil {
		return ErrInvalid
	}
	return nil
}

type Repository interface {
	List(context.Context, string, string, bool) ([]Record, error)
	Link(context.Context, string, string, LinkInput, string) (Record, error)
	End(context.Context, string, string, string, string, string) (Record, error)
}
