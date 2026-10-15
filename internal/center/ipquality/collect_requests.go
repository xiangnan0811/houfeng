package ipquality

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"houfeng/internal/contracts/agentapi"
)

// Requests are process-local: center restart loses them, not stored reports.
const (
	CollectRequestPending    = "pending"
	CollectRequestDispatched = "dispatched"
	CollectRequestCompleted  = "completed"
	CollectRequestExpired    = "expired"
	CollectRequestFailed     = "failed"
)

const (
	CollectRequestTTL              = 10 * time.Minute
	collectRequestRetention        = 30 * time.Minute
	maxCollectErrorSummaryRunes    = 200
	CollectScopeDualStack          = "dual_stack"
	CollectScopeSingleFamily       = "single_family"
	CollectScopeLegacySingleFamily = "legacy_single_family"
)

var ErrCollectionScopeConflict = errors.New("collection_scope_conflict")
var ErrInvalidCollectionScope = errors.New("invalid collection scope")

type CollectFamilyResult struct {
	IPVersion    int    `json:"ip_version"`
	Availability string `json:"availability"`
	ReportStatus string `json:"report_status"`
	ErrorCode    string `json:"error_code,omitempty"`
}

type CollectRequest struct {
	RequestID            string                `json:"request_id"`
	MonitoringInstanceID string                `json:"monitoring_instance_id"`
	Status               string                `json:"status"`
	RequestedIPVersions  []int                 `json:"requested_ip_versions"`
	ErrorCode            string                `json:"error_code,omitempty"`
	RequestedAt          time.Time             `json:"requested_at"`
	ExpiresAt            time.Time             `json:"expires_at"`
	DispatchedAt         *time.Time            `json:"dispatched_at,omitempty"`
	CompletedAt          *time.Time            `json:"completed_at,omitempty"`
	ReportStatus         string                `json:"report_status,omitempty"`
	ErrorSummary         string                `json:"error_summary,omitempty"`
	CollectionScope      string                `json:"collection_scope,omitempty"`
	CollectionID         string                `json:"collection_id,omitempty"`
	FamilyResults        []CollectFamilyResult `json:"family_results,omitempty"`
}

func (r CollectRequest) Active() bool {
	return r.Status == CollectRequestPending || r.Status == CollectRequestDispatched
}

type collectGroup struct {
	id         string
	observedAt time.Time
	revision   int
	expected   []int
	families   [2]*CollectFamilyResult
}

func (g *collectGroup) complete() bool {
	for _, family := range g.expected {
		if g.families[(family-4)/2] == nil {
			return false
		}
	}
	return true
}

func (g *collectGroup) newerThan(other *collectGroup) bool {
	return other == nil || g.observedAt.After(other.observedAt) ||
		(g.observedAt.Equal(other.observedAt) && g.id > other.id)
}

type collectEntry struct {
	request CollectRequest
	groups  map[string]*collectGroup
}

type CollectRequests struct {
	mu      sync.Mutex
	entries map[string]*collectEntry
	newID   func() (string, error)
}

func NewCollectRequests(newID func() (string, error)) *CollectRequests {
	return &CollectRequests{entries: make(map[string]*collectEntry), newID: newID}
}

// Request reuses an active request rather than causing another external collection.
func (c *CollectRequests) Request(monitoringInstanceID string, now time.Time, selections ...[]int) (CollectRequest, error) {
	versions := []int{4, 6}
	if len(selections) > 1 {
		return CollectRequest{}, ErrInvalidCollectionScope
	}
	if len(selections) == 1 {
		versions = selections[0]
	}
	if !agentapi.ValidIPQualityVersions(versions) {
		return CollectRequest{}, ErrInvalidCollectionScope
	}
	now = now.UTC()
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.currentLocked(monitoringInstanceID, now); ok && entry.request.Active() {
		if !slices.Equal(entry.request.RequestedIPVersions, versions) {
			return cloneCollectRequest(entry.request), ErrCollectionScopeConflict
		}
		return cloneCollectRequest(entry.request), nil
	}
	requestID, err := c.newID()
	if err != nil {
		return CollectRequest{}, fmt.Errorf("generate ip quality collect request id: %w", err)
	}
	request := CollectRequest{
		RequestID:            requestID,
		MonitoringInstanceID: monitoringInstanceID,
		RequestedIPVersions:  append([]int(nil), versions...),
		Status:               CollectRequestPending,
		RequestedAt:          now,
		ExpiresAt:            now.Add(CollectRequestTTL),
	}
	c.entries[monitoringInstanceID] = &collectEntry{request: request}
	return cloneCollectRequest(request), nil
}

func (c *CollectRequests) Latest(monitoringInstanceID string, now time.Time) (CollectRequest, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.currentLocked(monitoringInstanceID, now.UTC())
	if !ok {
		return CollectRequest{}, false
	}
	return cloneCollectRequest(entry.request), true
}

// PendingRequest atomically snapshots ID and scope and decides dispatch. A nil
// revision means no accepted writable live evidence; zero means an incapable
// live agent. Neither capability nor this snapshot is persisted.
func (c *CollectRequests) PendingRequest(monitoringInstanceID string, now time.Time, liveRevision *int) CollectRequest {
	now = now.UTC()
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.currentLocked(monitoringInstanceID, now)
	if !ok || !entry.request.Active() {
		return CollectRequest{}
	}
	if len(entry.request.RequestedIPVersions) == 1 {
		if liveRevision == nil {
			return CollectRequest{}
		}
		if *liveRevision != agentapi.IPQualitySelectiveCollectionRevision {
			entry.request.Status = CollectRequestFailed
			entry.request.ErrorCode = "unsupported_agent"
			entry.request.CompletedAt = &now
			entry.groups = nil
			return CollectRequest{}
		}
	}
	if entry.request.Status == CollectRequestPending {
		entry.request.Status = CollectRequestDispatched
		entry.request.DispatchedAt = &now
	}
	return cloneCollectRequest(entry.request)
}

// ObserveReports is called only after a recorded sync transaction. Apply the
// entire batch before selecting a complete group, so batch order cannot choose
// an older collection. Partial groups never combine across collection IDs.
func (c *CollectRequests) ObserveReports(monitoringInstanceID string, reports []ReportWrite, now time.Time) {
	now = now.UTC()
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.currentLocked(monitoringInstanceID, now)
	if !ok || !entry.request.Active() {
		return
	}
	var legacy *ReportWrite
	for i := range reports {
		report := &reports[i]
		metadata := report.CollectionMetadata
		if metadata.CollectRequestID == "" || metadata.CollectRequestID != entry.request.RequestID ||
			(report.MonitoringInstanceID != "" && report.MonitoringInstanceID != monitoringInstanceID) {
			continue
		}
		if !metadata.Paired {
			if len(entry.request.RequestedIPVersions) == 2 && terminalReportStatus(report.Status) && (report.IPVersion == 4 || report.IPVersion == 6) &&
				(legacy == nil || report.ObservedAt.After(legacy.ObservedAt)) {
				legacy = report
			}
			continue
		}
		if !validPairedReport(*report) || !slices.Equal(metadata.ExpectedIPVersions, entry.request.RequestedIPVersions) {
			continue
		}
		if entry.groups == nil {
			entry.groups = make(map[string]*collectGroup)
		}
		group := entry.groups[metadata.CollectionID]
		if group == nil {
			group = &collectGroup{id: metadata.CollectionID, observedAt: report.ObservedAt,
				revision: metadata.Revision, expected: append([]int(nil), metadata.ExpectedIPVersions...)}
			entry.groups[group.id] = group
		}
		if !group.observedAt.Equal(report.ObservedAt) || group.revision != metadata.Revision ||
			!slices.Equal(group.expected, metadata.ExpectedIPVersions) {
			continue
		}
		index := (report.IPVersion - 4) / 2
		if group.families[index] == nil {
			group.families[index] = &CollectFamilyResult{
				IPVersion:    report.IPVersion,
				Availability: metadata.Availability,
				ReportStatus: report.Status,
				ErrorCode:    SafeCollectErrorCode(report.ErrorCode),
			}
		}
	}
	var newest, complete *collectGroup
	for _, group := range entry.groups {
		if group.newerThan(newest) {
			newest = group
		}
		if group.complete() && group.newerThan(complete) {
			complete = group
		}
	}
	if complete != nil {
		projectCollectGroup(&entry.request, complete)
		entry.request.ReportStatus = aggregateCollectStatus(complete.families)
		// Never publish agent-provided error text for a paired request.
		if entry.request.ReportStatus != "success" {
			entry.request.ErrorSummary = "one or more IP families did not complete successfully"
		}
		completeCollectRequest(entry, now)
		return
	}
	if legacy != nil {
		entry.request.CollectionScope = CollectScopeLegacySingleFamily
		entry.request.CollectionID = ""
		entry.request.FamilyResults = []CollectFamilyResult{{
			IPVersion: legacy.IPVersion, Availability: agentapi.IPQualityFamilyUnknown,
			ReportStatus: legacy.Status, ErrorCode: SafeCollectErrorCode(legacy.ErrorCode),
		}}
		entry.request.ReportStatus = legacy.Status
		entry.request.ErrorSummary = truncateRunes(strings.TrimSpace(legacy.ErrorSummary), maxCollectErrorSummaryRunes)
		completeCollectRequest(entry, now)
		return
	}
	if newest != nil {
		projectCollectGroup(&entry.request, newest)
	}
}

func validPairedReport(report ReportWrite) bool {
	metadata := report.CollectionMetadata
	if !metadata.Valid || metadata.IPVersion != report.IPVersion || report.ObservedAt.IsZero() || !terminalReportStatus(report.Status) {
		return false
	}
	address, err := netip.ParseAddr(report.IPAddress)
	if err != nil || address.Zone() != "" || address.Is4In6() || (address.Is4() && report.IPVersion != 4) || (address.Is6() && report.IPVersion != 6) {
		return false
	}
	if metadata.Availability == agentapi.IPQualityFamilyUnavailable {
		return address.IsUnspecified() && report.Status == "failure" && report.ErrorCode == "address_family_unavailable"
	}
	if address.IsUnspecified() {
		return report.Status == "failure" && report.ErrorCode == "lookup_failed"
	}
	if report.Status != "failure" && metadata.Availability != agentapi.IPQualityFamilyAvailable {
		return false
	}
	return report.ErrorCode != "address_family_unavailable" && report.ErrorCode != "lookup_failed" && address.IsGlobalUnicast()
}

func terminalReportStatus(status string) bool {
	return status == "success" || status == "partial" || status == "failure"
}

func aggregateCollectStatus(families [2]*CollectFamilyResult) string {
	usable, valid, allSuccess := 0, 0, true
	for _, family := range families {
		if family == nil {
			continue
		}
		if family.Availability == agentapi.IPQualityFamilyUnavailable {
			continue
		}
		usable++
		if family.ReportStatus == "success" || family.ReportStatus == "partial" {
			valid++
		}
		allSuccess = allSuccess && family.ReportStatus == "success"
	}
	if usable > 0 && allSuccess {
		return "success"
	}
	if valid > 0 {
		return "partial"
	}
	return "failure"
}

// SafeCollectErrorCode projects only fixed diagnostic codes into public responses.
// Unknown values are omitted rather than exposing arbitrary agent-provided text.
func SafeCollectErrorCode(code string) string {
	switch code {
	case "address_family_unavailable", "lookup_failed", "disabled", "ip_mismatch", "egress_ambiguous",
		"provider_partial", "service_partial", "probe_failed", "non_json_response", "http_status",
		"dns_failed", "connection_refused", "network_unreachable", "tls_failed", "timeout", "request_failed":
		return code
	default:
		return ""
	}
}

func projectCollectGroup(request *CollectRequest, group *collectGroup) {
	request.CollectionScope = CollectScopeDualStack
	if len(group.expected) == 1 {
		request.CollectionScope = CollectScopeSingleFamily
	}
	request.CollectionID = group.id
	request.FamilyResults = make([]CollectFamilyResult, 0, 2)
	for _, family := range group.families {
		if family != nil {
			request.FamilyResults = append(request.FamilyResults, *family)
		}
	}
}

func completeCollectRequest(entry *collectEntry, now time.Time) {
	entry.request.Status = CollectRequestCompleted
	entry.request.CompletedAt = &now
	entry.groups = nil
}

func cloneCollectRequest(request CollectRequest) CollectRequest {
	request.RequestedIPVersions = append([]int(nil), request.RequestedIPVersions...)
	request.FamilyResults = append([]CollectFamilyResult(nil), request.FamilyResults...)
	if request.DispatchedAt != nil {
		value := *request.DispatchedAt
		request.DispatchedAt = &value
	}
	if request.CompletedAt != nil {
		value := *request.CompletedAt
		request.CompletedAt = &value
	}
	return request
}

func (c *CollectRequests) currentLocked(monitoringInstanceID string, now time.Time) (*collectEntry, bool) {
	c.pruneLocked(now)
	entry, ok := c.entries[monitoringInstanceID]
	if !ok {
		return nil, false
	}
	if entry.request.Active() && !now.Before(entry.request.ExpiresAt) {
		entry.request.Status = CollectRequestExpired
		entry.groups = nil
	}
	return entry, true
}

func (c *CollectRequests) pruneLocked(now time.Time) {
	for key, entry := range c.entries {
		if now.Sub(entry.request.RequestedAt) > collectRequestRetention {
			delete(c.entries, key)
		}
	}
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
