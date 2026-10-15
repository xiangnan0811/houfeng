package agentapi

import "time"

// IPQualityProductRevision is negotiated before any product wire fields are sent.
const IPQualityProductRevision = 1

type IPQualityScore struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	RawValue    string   `json:"raw_value"`
	Unit        string   `json:"unit"`
	Scope       string   `json:"scope"`
	Direction   string   `json:"direction"`
	SourceLabel string   `json:"source_label"`
	Value       *float64 `json:"value"`
	Min         *float64 `json:"min"`
	Max         *float64 `json:"max"`
}

type IPQualityProviderDetails struct {
	QueriedIP             string            `json:"queried_ip"`
	ObservedAt            time.Time         `json:"observed_at"`
	Method                string            `json:"method"`
	APITier               string            `json:"api_tier"`
	CountryCode           string            `json:"country_code"`
	Region                string            `json:"region"`
	City                  string            `json:"city"`
	PostalCode            string            `json:"postal_code"`
	Timezone              string            `json:"timezone"`
	Latitude              *float64          `json:"latitude"`
	Longitude             *float64          `json:"longitude"`
	RegisteredCountryCode string            `json:"registered_country_code"`
	ASN                   string            `json:"asn"`
	ASName                string            `json:"as_name"`
	ASDomain              string            `json:"as_domain"`
	ASCountryCode         string            `json:"as_country_code"`
	Route                 string            `json:"route"`
	CompanyName           string            `json:"company_name"`
	CompanyDomain         string            `json:"company_domain"`
	IsMobile              *bool             `json:"is_mobile"`
	IsAnycast             *bool             `json:"is_anycast"`
	IsBogon               *bool             `json:"is_bogon"`
	Scores                []IPQualityScore  `json:"scores"`
	FieldStates           map[string]string `json:"field_states"`
}

type IPQualityAgentProduct struct {
	Revision int                   `json:"revision"`
	PTRNames []string              `json:"ptr_names"`
	Mail     []IPQualityMailResult `json:"mail"`
}

type IPQualityMailResult struct {
	Provider     string    `json:"provider"`
	Domain       string    `json:"domain"`
	Status       string    `json:"status"`
	MXHost       string    `json:"mx_host"`
	TargetIP     string    `json:"target_ip"`
	ErrorCode    string    `json:"error_code"`
	MXPreference *uint16   `json:"mx_preference"`
	GreetingCode *int      `json:"greeting_code"`
	LatencyMS    *int      `json:"latency_ms"`
	ObservedAt   time.Time `json:"observed_at"`
}

type IPQualityServiceCheck struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Status     string `json:"status"`
	Region     string `json:"region"`
	ErrorCode  string `json:"error_code"`
	Rule       string `json:"rule"`
	HTTPStatus *int   `json:"http_status"`
}

type IPQualityBlacklistResult struct {
	Source       string     `json:"source"`
	Category     string     `json:"category"`
	Status       string     `json:"status"`
	QueryMethod  string     `json:"query_method"`
	ErrorCode    string     `json:"error_code"`
	ReferenceURL string     `json:"reference_url"`
	MatchedRange string     `json:"matched_range"`
	Codes        []string   `json:"codes"`
	ObservedAt   time.Time  `json:"observed_at"`
	DatasetAt    *time.Time `json:"dataset_at"`
}

type IPQualityRegistration struct {
	Status                string     `json:"status"`
	RIR                   string     `json:"rir"`
	NetworkHandle         string     `json:"network_handle"`
	StartAddress          string     `json:"start_address"`
	EndAddress            string     `json:"end_address"`
	NetworkName           string     `json:"network_name"`
	NetworkCountryCode    string     `json:"network_country_code"`
	RegistrantCountryCode string     `json:"registrant_country_code"`
	RegistrantName        string     `json:"registrant_name"`
	SourceURL             string     `json:"source_url"`
	RegisteredAt          *time.Time `json:"registered_at"`
	UpdatedAt             *time.Time `json:"updated_at"`
}

type IPQualityASN struct {
	Status        string     `json:"status"`
	Number        string     `json:"number"`
	Name          string     `json:"name"`
	Domain        string     `json:"domain"`
	Type          string     `json:"type"`
	CountryCode   string     `json:"country_code"`
	RIR           string     `json:"rir"`
	SourceURL     string     `json:"source_url"`
	Route         string     `json:"route"`
	RegisteredAt  *time.Time `json:"registered_at"`
	UpdatedAt     *time.Time `json:"updated_at"`
	ObservedAt    *time.Time `json:"observed_at"`
	IPv4Prefixes  *string    `json:"ipv4_prefixes"`
	IPv4Addresses *string    `json:"ipv4_addresses"`
	IPv6Prefixes  *string    `json:"ipv6_prefixes"`
	IPv6_48s      *string    `json:"ipv6_48s"`
	Origins       []string   `json:"origins"`
}

// IPQualityIPType is a geographic comparison, not a physical anycast or risk claim.
type IPQualityIPType struct {
	Status                string `json:"status"`
	Basis                 string `json:"basis,omitempty"`
	Provider              string `json:"provider,omitempty"`
	Method                string `json:"method,omitempty"`
	CountryCode           string `json:"country_code,omitempty"`
	ComparisonCountryCode string `json:"comparison_country_code,omitempty"`
	UpstreamCommit        string `json:"upstream_commit,omitempty"`
	ForkRevision          int    `json:"fork_revision,omitempty"`
}

type IPQualityProductSummary struct {
	RiskFlags              []string         `json:"risk_flags"`
	HighRiskSources        []string         `json:"high_risk_sources"`
	RestrictedServices     []string         `json:"restricted_services"`
	BlacklistListedCount   int              `json:"blacklist_listed_count"`
	BlacklistCheckedCount  int              `json:"blacklist_checked_count"`
	ServiceAvailableCount  int              `json:"service_available_count"`
	ServicePartialCount    int              `json:"service_partial_count"`
	ServiceRestrictedCount int              `json:"service_restricted_count"`
	ServiceUnknownCount    int              `json:"service_unknown_count"`
	GeoStatus              string           `json:"geo_status"`
	IPType                 *IPQualityIPType `json:"ip_type,omitempty"`
}
