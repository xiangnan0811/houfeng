package ipquality

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"houfeng/internal/contracts/agentapi"
)

type IPQualityProductView struct {
	Revision        int                                 `json:"revision"`
	ReportID        string                              `json:"report_id"`
	IPAddress       string                              `json:"ip_address"`
	IPVersion       int                                 `json:"ip_version"`
	State           string                              `json:"state"`
	AgentObservedAt time.Time                           `json:"agent_observed_at"`
	CompletedAt     *time.Time                          `json:"completed_at"`
	PTRNames        []string                            `json:"ptr_names"`
	Providers       []ProviderResultRead                `json:"providers"`
	Mail            []agentapi.IPQualityMailResult      `json:"mail"`
	Blacklists      []agentapi.IPQualityBlacklistResult `json:"blacklists"`
	Registration    agentapi.IPQualityRegistration      `json:"registration"`
	ASN             agentapi.IPQualityASN               `json:"asn"`
	Summary         agentapi.IPQualityProductSummary    `json:"summary"`
}

type SourceOutcome struct {
	SourceID   string    `json:"source_id"`
	Status     string    `json:"status"`
	ErrorCode  string    `json:"error_code"`
	ObservedAt time.Time `json:"observed_at"`
}

type IntelligenceSnapshot struct {
	Providers      []ProviderResultRead                `json:"providers"`
	Blacklists     []agentapi.IPQualityBlacklistResult `json:"blacklists"`
	Registration   agentapi.IPQualityRegistration      `json:"registration"`
	ASN            agentapi.IPQualityASN               `json:"asn"`
	SourceOutcomes []SourceOutcome                     `json:"source_outcomes"`
	// Internal execution metadata is never part of the snapshot API.
	State                  string                 `json:"-"`
	CompletedAt            *time.Time             `json:"-"`
	CredentialFingerprints map[string]string      `json:"-"`
	CacheWrites            []SourceCacheEntry     `json:"-"`
	PublicASN              *agentapi.IPQualityASN `json:"-"`
}

type IntelligenceClaim struct {
	ReportID        string
	OwnerID         string
	OwnerGeneration int64
	DeadlineAt      time.Time
	LeaseUntil      time.Time
	AttemptCount    int
	Report          ReportWrite
}

type SourceCacheEntry struct {
	SourceID              string
	Resource              string
	CredentialFingerprint string
	FetchedAt             time.Time
	ExpiresAt             time.Time
	BlockedUntil          *time.Time
	PayloadJSON           json.RawMessage
}

var ErrIntelligenceOwnership = errors.New("ip quality intelligence ownership expired")

type IntelligenceRepository interface {
	ClaimIPQualityIntelligence(context.Context, string) (*IntelligenceClaim, error)
	CompleteIPQualityIntelligence(context.Context, IntelligenceClaim, IntelligenceSnapshot) error
	IPQualityIntelligenceEligible(context.Context, IntelligenceClaim) (bool, error)
	LoadIPQualitySourceCache(context.Context, string, string, string) (*SourceCacheEntry, error)
}
