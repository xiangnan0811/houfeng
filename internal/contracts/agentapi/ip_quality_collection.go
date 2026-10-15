package agentapi

import (
	"encoding/json"
	"strings"
)

const (
	IPQualityDiagnosticsCollectionRevisionKey = "collection_revision"
	IPQualityDiagnosticsCollectionIDKey       = "collection_id"
	IPQualityDiagnosticsExpectedIPVersionsKey = "expected_ip_versions"
	IPQualityDiagnosticsIPFamilyKey           = "ip_address_family"
	IPQualityDiagnosticsFamilyAvailabilityKey = "family_availability"
	IPQualityCollectionRevision               = 4
	IPQualitySelectiveCollectionRevision      = 5
	IPQualityCollectionRevisionHeader         = "X-Houfeng-IPQuality-Collection-Revision"
	IPQualityFamilyAvailable                  = "available"
	IPQualityFamilyUnavailable                = "unavailable"
	IPQualityFamilyUnknown                    = "unknown"
)

// CollectionMetadata is parsed before diagnostics sanitization. Paired remains
// true for unsupported or malformed paired metadata: it must not become legacy.
// These fields are internal metadata, not additions to the sync wire payload.
type CollectionMetadata struct {
	Paired             bool
	Valid              bool
	Revision           int
	CollectionID       string
	ExpectedIPVersions []int
	IPVersion          int
	Availability       string
	CollectRequestID   string
}

func ParseCollectionMetadata(raw json.RawMessage) CollectionMetadata {
	var result CollectionMetadata
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return result
	}
	var requestID string
	if json.Unmarshal(fields[IPQualityDiagnosticsCollectRequestIDKey], &requestID) == nil {
		requestID = strings.TrimSpace(requestID)
		if validCollectionID(requestID) {
			result.CollectRequestID = requestID
		}
	}
	_, hasID := fields[IPQualityDiagnosticsCollectionIDKey]
	_, hasExpected := fields[IPQualityDiagnosticsExpectedIPVersionsKey]
	var numericRevision float64
	_ = json.Unmarshal(fields[IPQualityDiagnosticsCollectionRevisionKey], &numericRevision)
	result.Paired = hasID || hasExpected || numericRevision >= IPQualityCollectionRevision
	if !result.Paired {
		return result
	}
	revisionErr := json.Unmarshal(fields[IPQualityDiagnosticsCollectionRevisionKey], &result.Revision)
	idErr := json.Unmarshal(fields[IPQualityDiagnosticsCollectionIDKey], &result.CollectionID)
	var expected []int
	expectedErr := json.Unmarshal(fields[IPQualityDiagnosticsExpectedIPVersionsKey], &expected)
	familyErr := json.Unmarshal(fields[IPQualityDiagnosticsIPFamilyKey], &result.IPVersion)
	availabilityErr := json.Unmarshal(fields[IPQualityDiagnosticsFamilyAvailabilityKey], &result.Availability)
	result.ExpectedIPVersions = expected
	validRevision := result.Revision == IPQualitySelectiveCollectionRevision ||
		(result.Revision == IPQualityCollectionRevision && len(expected) == 2)
	includesFamily := false
	for _, family := range expected {
		includesFamily = includesFamily || family == result.IPVersion
	}
	result.Valid = revisionErr == nil && validRevision &&
		idErr == nil && validCollectionID(result.CollectionID) &&
		expectedErr == nil && ValidIPQualityVersions(expected) &&
		familyErr == nil && includesFamily &&
		availabilityErr == nil && (result.Availability == IPQualityFamilyAvailable || result.Availability == IPQualityFamilyUnavailable || result.Availability == IPQualityFamilyUnknown)
	return result
}

func validCollectionID(value string) bool {
	if len(value) < 1 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

// ValidIPQualityVersions accepts only canonical nonempty family selections.
func ValidIPQualityVersions(versions []int) bool {
	return len(versions) == 1 && (versions[0] == 4 || versions[0] == 6) ||
		len(versions) == 2 && versions[0] == 4 && versions[1] == 6
}

// CollectionIPVersions returns an owned selection. Periodic and legacy plans
// always remain dual-stack, even when a stale selector is present.
func CollectionIPVersions(plan *IPQualityPlan) []int {
	if plan != nil && plan.CollectionRevision == IPQualitySelectiveCollectionRevision &&
		strings.TrimSpace(plan.CollectRequestID) != "" && ValidIPQualityVersions(plan.CollectIPVersions) {
		return append([]int(nil), plan.CollectIPVersions...)
	}
	return []int{4, 6}
}
