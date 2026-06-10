package importing

import (
	"context"
	"errors"
	"time"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/providers"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

var ErrImportBlocked = errors.New("vps import blocked")

type ProviderRepository interface {
	ListProviders(context.Context) ([]providers.Record, error)
	CreateProvider(context.Context, providers.CreateInput) (providers.Record, error)
}

type VPSAssetRepository interface {
	ListVPSAssets(context.Context, vpsassets.ListFilters) ([]vpsassets.Record, error)
	CreateVPSAsset(context.Context, vpsassets.CreateInput) (vpsassets.Record, error)
}

type SubscriptionRepository interface {
	ListSubscriptions(context.Context, subscriptions.ListFilters) ([]subscriptions.Record, error)
	CreateSubscription(context.Context, subscriptions.CreateInput) (subscriptions.Record, error)
}

type MonitoringInstanceRepository interface {
	ListMonitoringInstances(context.Context, ...monitoringinstances.ListScope) ([]monitoringinstances.Record, error)
}

type Repositories struct {
	Providers           ProviderRepository
	VPSAssets           VPSAssetRepository
	Subscriptions       SubscriptionRepository
	MonitoringInstances MonitoringInstanceRepository
}

type Options struct {
	Now                    func() time.Time
	IgnoreRepositoryErrors bool
}

type InputRecord struct {
	DisplayName            string                    `json:"display_name"`
	ProviderID             *string                   `json:"provider_id"`
	ProviderName           string                    `json:"provider_name"`
	ProductName            string                    `json:"product_name"`
	OrderRef               string                    `json:"order_ref"`
	Country                string                    `json:"country"`
	Region                 string                    `json:"region"`
	City                   string                    `json:"city"`
	Datacenter             string                    `json:"datacenter"`
	IPv4                   string                    `json:"ipv4"`
	IPv6                   string                    `json:"ipv6"`
	SSHHost                string                    `json:"ssh_host"`
	SSHPort                int                       `json:"ssh_port"`
	SSHUser                string                    `json:"ssh_user"`
	OSName                 string                    `json:"os_name"`
	Virtualization         string                    `json:"virtualization"`
	LifecycleStatus        vpsassets.LifecycleStatus `json:"lifecycle_status"`
	UsageStatus            vpsassets.UsageStatus     `json:"usage_status"`
	RenewalDecision        vpsassets.RenewalDecision `json:"renewal_decision"`
	Importance             string                    `json:"importance"`
	Labels                 []string                  `json:"labels"`
	Note                   string                    `json:"note"`
	Subscription           *SubscriptionInput        `json:"subscription"`
	MonitoringInstanceID   string                    `json:"monitoring_instance_id"`
	MonitoringInstanceName string                    `json:"monitoring_instance_name"`
	AgentTokenHint         string                    `json:"agent_token_hint"`
	TargetURL              string                    `json:"target_url"`
}

type SubscriptionInput struct {
	Price              float64              `json:"price"`
	Currency           string               `json:"currency"`
	BillingCycle       string               `json:"billing_cycle"`
	BillingMonths      int                  `json:"billing_months"`
	StartedAt          *string              `json:"started_at"`
	RenewAt            *string              `json:"renew_at"`
	AutoRenew          bool                 `json:"auto_renew"`
	AutoRenewCancelled bool                 `json:"auto_renew_cancelled"`
	Status             subscriptions.Status `json:"status"`
	PaymentMethod      string               `json:"payment_method"`
	Note               string               `json:"note"`
}

type Report struct {
	Mode                                    string                                   `json:"mode"`
	CurrentDate                             string                                   `json:"current_date"`
	DatabaseChecked                         bool                                     `json:"database_checked"`
	CanImport                               bool                                     `json:"can_import"`
	Warnings                                []string                                 `json:"warnings"`
	Totals                                  Totals                                   `json:"totals"`
	ProviderCandidates                      []ProviderCandidate                      `json:"provider_candidates"`
	VPSCandidates                           []VPSCandidate                           `json:"vps_candidates"`
	SubscriptionCandidates                  []SubscriptionCandidate                  `json:"subscription_candidates"`
	MissingProviderRows                     []RowIssue                               `json:"missing_provider_rows"`
	MissingRenewDateRows                    []RowIssue                               `json:"missing_renew_date_rows"`
	ValidationErrors                        []RowIssue                               `json:"validation_errors"`
	DuplicateCandidates                     []DuplicateCandidate                     `json:"duplicate_candidates"`
	MonitoringInstanceAssociationCandidates []MonitoringInstanceAssociationCandidate `json:"monitoring_instance_association_candidates"`
	RenewalCandidates                       []RenewalCandidate                       `json:"renewal_candidates"`
	IdlePaidCandidates                      []IdlePaidCandidate                      `json:"idle_paid_candidates"`
	Import                                  ImportResult                             `json:"import"`
}

type Totals struct {
	InputRows                               int `json:"input_rows"`
	ProviderCreateCandidates                int `json:"provider_create_candidates"`
	VPSCreateCandidates                     int `json:"vps_create_candidates"`
	SubscriptionCandidates                  int `json:"subscription_candidates"`
	MissingProviderRows                     int `json:"missing_provider_rows"`
	MissingRenewDateRows                    int `json:"missing_renew_date_rows"`
	ValidationErrors                        int `json:"validation_errors"`
	DuplicateCandidates                     int `json:"duplicate_candidates"`
	MonitoringInstanceAssociationCandidates int `json:"monitoring_instance_association_candidates"`
	RenewalCandidates                       int `json:"renewal_candidates"`
	IdlePaidCandidates                      int `json:"idle_paid_candidates"`
	ImportedProviders                       int `json:"imported_providers"`
	ImportedVPSAssets                       int `json:"imported_vps_assets"`
	ImportedSubscriptions                   int `json:"imported_subscriptions"`
}

type ProviderCandidate struct {
	Name string `json:"name"`
	Rows []int  `json:"rows"`
}

type VPSCandidate struct {
	Row          int     `json:"row"`
	DisplayName  string  `json:"display_name"`
	ProviderID   *string `json:"provider_id,omitempty"`
	ProviderName string  `json:"provider_name"`
	Country      string  `json:"country"`
	Region       string  `json:"region"`
	City         string  `json:"city"`
}

type SubscriptionCandidate struct {
	Row           int     `json:"row"`
	DisplayName   string  `json:"display_name"`
	Price         float64 `json:"price"`
	Currency      string  `json:"currency"`
	BillingMonths int     `json:"billing_months"`
	MonthlyPrice  float64 `json:"monthly_price"`
	RenewAt       *string `json:"renew_at,omitempty"`
}

type RowIssue struct {
	Row     int    `json:"row"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

type DuplicateCandidate struct {
	Type       string `json:"type"`
	Key        string `json:"key"`
	Rows       []int  `json:"rows,omitempty"`
	ExistingID string `json:"existing_id,omitempty"`
	Message    string `json:"message"`
}

type MonitoringInstanceAssociationCandidate struct {
	Row                    int    `json:"row"`
	DisplayName            string `json:"display_name"`
	MonitoringInstanceID   string `json:"monitoring_instance_id,omitempty"`
	MonitoringInstanceName string `json:"monitoring_instance_name,omitempty"`
	TargetURL              string `json:"target_url,omitempty"`
	Status                 string `json:"status"`
}

type RenewalCandidate struct {
	Row          int     `json:"row"`
	DisplayName  string  `json:"display_name"`
	RenewAt      string  `json:"renew_at"`
	DaysUntil    int     `json:"days_until"`
	Price        float64 `json:"price"`
	Currency     string  `json:"currency"`
	MonthlyPrice float64 `json:"monthly_price"`
}

type IdlePaidCandidate struct {
	Row          int     `json:"row"`
	DisplayName  string  `json:"display_name"`
	Price        float64 `json:"price"`
	Currency     string  `json:"currency"`
	MonthlyPrice float64 `json:"monthly_price"`
	RenewAt      *string `json:"renew_at,omitempty"`
}

type ImportResult struct {
	CreatedProviders     []CreatedProvider     `json:"created_providers"`
	CreatedVPSAssets     []CreatedVPSAsset     `json:"created_vps_assets"`
	CreatedSubscriptions []CreatedSubscription `json:"created_subscriptions"`
}

type CreatedProvider struct {
	ProviderID string `json:"provider_id"`
	Name       string `json:"name"`
}

type CreatedVPSAsset struct {
	Row         int    `json:"row"`
	VPSID       string `json:"vps_id"`
	DisplayName string `json:"display_name"`
}

type CreatedSubscription struct {
	Row            int    `json:"row"`
	SubscriptionID string `json:"subscription_id"`
	VPSID          string `json:"vps_id"`
}
