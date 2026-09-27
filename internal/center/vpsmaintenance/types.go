package vpsmaintenance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

var (
	ErrInvalid  = errors.New("invalid VPS maintenance input")
	ErrNotFound = errors.New("VPS not found")
	ErrConflict = errors.New("VPS maintenance state changed")
)

type Resource struct {
	Kind            string   `json:"kind"`
	ResourceID      string   `json:"resource_id"`
	Name            string   `json:"name"`
	Control         string   `json:"control"`
	ControlRevision int64    `json:"control_revision"`
	Shared          bool     `json:"shared"`
	SharedVPSIDs    []string `json:"shared_vps_ids"`
	Eligible        bool     `json:"eligible"`
}

type Review struct {
	VPSID          string     `json:"vps_id"`
	Lifecycle      string     `json:"lifecycle_status"`
	ActiveActionID string     `json:"active_action_id"`
	Resources      []Resource `json:"resources"`
	PreviewDigest  string     `json:"preview_digest"`
}

func (r Review) Digest() string {
	r.PreviewDigest = ""
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

type StartInput struct {
	Reason                   string   `json:"reason"`
	PreviewDigest            string   `json:"preview_digest"`
	ConfirmedSharedTargetIDs []string `json:"confirmed_shared_target_ids"`
}

type EndInput struct {
	Reason string `json:"reason"`
}

// Select checks explicit shared-target consent against the current review.
// Controls already paused or maintained belong to their original operator.
func Select(r Review, input StartInput) ([]Resource, error) {
	if strings.TrimSpace(input.Reason) == "" || input.PreviewDigest == "" {
		return nil, ErrInvalid
	}
	if r.Lifecycle != "active" || r.ActiveActionID != "" || input.PreviewDigest != r.Digest() {
		return nil, ErrConflict
	}
	confirmed := make(map[string]bool)
	for _, id := range input.ConfirmedSharedTargetIDs {
		if id == "" || confirmed[id] {
			return nil, ErrInvalid
		}
		confirmed[id] = true
	}
	selected := make([]Resource, 0)
	for _, resource := range r.Resources {
		if resource.Kind == "target" && resource.Shared {
			if !confirmed[resource.ResourceID] {
				continue
			}
			delete(confirmed, resource.ResourceID)
		}
		if resource.Eligible {
			selected = append(selected, resource)
		}
	}
	if len(confirmed) != 0 {
		return nil, ErrInvalid
	}
	return selected, nil
}

type Repository interface {
	Review(context.Context, string) (Review, error)
	Start(context.Context, string, StartInput, string) (Review, error)
	End(context.Context, string, EndInput, string) (Review, error)
}
