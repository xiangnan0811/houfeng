package ipquality

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"houfeng/internal/contracts/agentapi"
)

func newTestCollectRequests() *CollectRequests {
	counter := 0
	return NewCollectRequests(func() (string, error) {
		counter++
		return fmt.Sprintf("ipqc_%03d", counter), nil
	})
}

func TestCollectRequestsReusesActiveRequest(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.September, 30, 8, 0, 0, 0, time.UTC)
	requests := newTestCollectRequests()

	first, err := requests.Request("mi_001", now)
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	if first.Status != CollectRequestPending || !first.ExpiresAt.Equal(now.Add(CollectRequestTTL)) {
		t.Fatalf("first = %#v, want pending with TTL", first)
	}
	if first.CollectionScope != "" || first.CollectionID != "" || len(first.FamilyResults) != 0 {
		t.Fatalf("pending request fabricated collection results: %#v", first)
	}
	second, err := requests.Request("mi_001", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Request(second) error = %v", err)
	}
	if second.RequestID != first.RequestID {
		t.Fatalf("second request id = %q, want reuse of active %q", second.RequestID, first.RequestID)
	}
	other, _ := requests.Request("mi_002", now)
	if other.RequestID == first.RequestID {
		t.Fatal("different monitoring instances must not share a request")
	}
}

func TestCollectRequestsDispatchAndCompleteFromEchoedReport(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.September, 30, 8, 0, 0, 0, time.UTC)
	requests := newTestCollectRequests()
	request, _ := requests.Request("mi_001", now)

	if got := requests.PendingRequest("mi_unknown", now, nil); got.RequestID != "" {
		t.Fatalf("PendingRequest(unknown) = %#v, want empty", got)
	}
	if got := requests.PendingRequest("mi_001", now.Add(5*time.Second), nil); got.RequestID != request.RequestID {
		t.Fatalf("PendingRequest() = %#v, want %q", got, request.RequestID)
	}
	dispatched, _ := requests.Latest("mi_001", now.Add(6*time.Second))
	if dispatched.Status != CollectRequestDispatched || dispatched.DispatchedAt == nil {
		t.Fatalf("latest = %#v, want dispatched", dispatched)
	}
	// 再次下发保持首次下发时间。
	requests.PendingRequest("mi_001", now.Add(10*time.Second), nil)
	again, _ := requests.Latest("mi_001", now.Add(11*time.Second))
	if !again.DispatchedAt.Equal(now.Add(5 * time.Second)) {
		t.Fatalf("DispatchedAt = %s, want first dispatch time", again.DispatchedAt)
	}

	// 周期报告、离线补传或其他请求的报告，不论时间多新，都不能完成本次请求。
	requests.ObserveReports("mi_001", []ReportWrite{
		{Status: "success", ObservedAt: now.Add(15 * time.Second)},
		legacyCollectReport("ipqc_other", "success", now.Add(15*time.Second)),
	}, now.Add(20*time.Second))
	if latest, _ := requests.Latest("mi_001", now.Add(21*time.Second)); latest.Status != CollectRequestDispatched {
		t.Fatalf("latest = %#v, unmatched reports must not complete the request", latest)
	}

	// 被认领的周期采集可能在发起前很久就开始，只要带回同一 ID 就完成。
	requests.ObserveReports("mi_001", []ReportWrite{{
		Status:             "failure",
		ObservedAt:         now.Add(-5 * time.Minute),
		ErrorSummary:       strings.Repeat("超时", 150),
		IPVersion:          4,
		CollectionMetadata: agentapi.ParseCollectionMetadata([]byte(fmt.Sprintf(`{"collect_request_id":%q}`, request.RequestID))),
	}}, now.Add(30*time.Second))
	completed, _ := requests.Latest("mi_001", now.Add(31*time.Second))
	if completed.Status != CollectRequestCompleted || completed.ReportStatus != "failure" || completed.CompletedAt == nil {
		t.Fatalf("completed = %#v, want completed failure", completed)
	}
	if completed.CollectionScope != CollectScopeLegacySingleFamily || completed.CollectionID != "" ||
		len(completed.FamilyResults) != 1 || completed.FamilyResults[0].IPVersion != 4 ||
		completed.FamilyResults[0].Availability != "unknown" {
		t.Fatalf("legacy report incorrectly claimed dual-stack completion: %#v", completed)
	}
	if len([]rune(completed.ErrorSummary)) != maxCollectErrorSummaryRunes+1 {
		t.Fatalf("ErrorSummary runes = %d, want truncated to %d plus ellipsis", len([]rune(completed.ErrorSummary)), maxCollectErrorSummaryRunes)
	}
	if got := requests.PendingRequest("mi_001", now.Add(32*time.Second), nil); got.RequestID != "" {
		t.Fatalf("PendingRequest(after completion) = %#v, want empty", got)
	}

	next, _ := requests.Request("mi_001", now.Add(40*time.Second))
	if next.RequestID == request.RequestID || next.Status != CollectRequestPending {
		t.Fatalf("next = %#v, want a new pending request after completion", next)
	}
}

func TestCollectRequestsExpireAndPrune(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.September, 30, 8, 0, 0, 0, time.UTC)
	requests := newTestCollectRequests()
	request, _ := requests.Request("mi_001", now)
	requests.PendingRequest("mi_001", now, nil)

	expiredAt := now.Add(CollectRequestTTL)
	if got := requests.PendingRequest("mi_001", expiredAt, nil); got.RequestID != "" {
		t.Fatalf("PendingRequest(expired) = %#v, want empty", got)
	}
	expired, ok := requests.Latest("mi_001", expiredAt)
	if !ok || expired.Status != CollectRequestExpired || expired.CompletedAt != nil {
		t.Fatalf("latest = %#v, want expired without a fabricated completion time", expired)
	}
	requests.ObserveReports("mi_001", []ReportWrite{legacyCollectReport(request.RequestID, "success", now)}, expiredAt.Add(time.Second))
	if late, _ := requests.Latest("mi_001", expiredAt.Add(2*time.Second)); late.Status != CollectRequestExpired {
		t.Fatalf("late report must not revive an expired request: %#v", late)
	}

	pruneAt := expiredAt.Add(collectRequestRetention)
	requests.Request("mi_002", now.Add(collectRequestRetention+time.Second))
	if retained, ok := requests.Latest("mi_001", pruneAt.Add(-time.Nanosecond)); !ok || !reflect.DeepEqual(retained, expired) {
		t.Fatalf("expired request must remain readable throughout retention: %#v, ok=%t", retained, ok)
	}
	if _, ok := requests.Latest("mi_001", pruneAt); ok {
		t.Fatal("expired request must be pruned exactly at its terminal retention cutoff")
	}
}

func TestLegacyCollectRequestMetadataParsing(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]string{
		`{"source_version":"v2","collect_request_id":"ipqc_0123abcd"}`: "ipqc_0123abcd",
		`{"collect_request_id":" ipqc-ABC_1 "}`:                        "ipqc-ABC_1",
		`{"source_version":"v2"}`:                                      "",
		`{"collect_request_id":123}`:                                   "",
		`{"collect_request_id":"ipqc 1"}`:                              "",
		`{"collect_request_id":"ipqc_<script>"}`:                       "",
		`{"collect_request_id":"` + strings.Repeat("a", 65) + `"}`:     "",
		`["collect_request_id"]`:                                       "",
		`not json`:                                                     "",
		``:                                                             "",
	} {
		if got := agentapi.ParseCollectionMetadata([]byte(raw)).CollectRequestID; got != want {
			t.Fatalf("ParseCollectionMetadata(%s).CollectRequestID = %q, want %q", raw, got, want)
		}
	}
}

func legacyCollectReport(requestID, status string, observedAt time.Time) ReportWrite {
	raw := []byte(fmt.Sprintf(`{"collect_request_id":%q}`, requestID))
	return ReportWrite{
		MonitoringInstanceID: "mi_001", IPAddress: "198.51.100.10", IPVersion: 4,
		Status: status, ObservedAt: observedAt, DiagnosticsJSON: raw,
		CollectionMetadata: agentapi.ParseCollectionMetadata(raw),
	}
}

func pairedCollectReport(requestID, collectionID string, family int, availability, status string, observedAt time.Time) ReportWrite {
	raw := []byte(fmt.Sprintf(`{"collection_revision":4,"collection_id":%q,"expected_ip_versions":[4,6],"ip_address_family":%d,"family_availability":%q,"collect_request_id":%q}`,
		collectionID, family, availability, requestID))
	report := ReportWrite{
		MonitoringInstanceID: "mi_001", IPVersion: family, Status: status, ObservedAt: observedAt,
		DiagnosticsJSON: raw, CollectionMetadata: agentapi.ParseCollectionMetadata(raw),
		IPAddress: "198.51.100.10",
	}
	if family == 6 {
		report.IPAddress = "2001:db8::10"
	}
	if status == "failure" {
		report.IPAddress, report.ErrorCode = "0.0.0.0", "lookup_failed"
		if family == 6 {
			report.IPAddress = "::"
		}
	}
	if availability == "unavailable" {
		report.ErrorCode = "address_family_unavailable"
	}
	return report
}

func TestCollectRequestsPairedGroupsNeverMixOrOverwriteFamily(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	registry := newTestCollectRequests()
	request, _ := registry.Request("mi_001", now)
	registry.PendingRequest("mi_001", now, nil)
	a4 := pairedCollectReport(request.RequestID, "A", 4, "available", "success", now)
	b6 := pairedCollectReport(request.RequestID, "B", 6, "available", "success", now.Add(time.Second))
	registry.ObserveReports("mi_001", []ReportWrite{a4, b6}, now.Add(2*time.Second))
	current, _ := registry.Latest("mi_001", now.Add(3*time.Second))
	if current.Status != CollectRequestDispatched || current.CollectionID != "B" ||
		current.CollectionScope != CollectScopeDualStack || len(current.FamilyResults) != 1 || current.FamilyResults[0].IPVersion != 6 {
		t.Fatalf("mixed groups leaked or completed: %#v", current)
	}
	// Duplicate IPv4 and a same-ID IPv6 with a different time cannot complete A.
	duplicate := pairedCollectReport(request.RequestID, "A", 4, "available", "failure", now)
	wrongTime := pairedCollectReport(request.RequestID, "A", 6, "available", "failure", now.Add(time.Second))
	registry.ObserveReports("mi_001", []ReportWrite{duplicate, wrongTime}, now.Add(4*time.Second))
	if current, _ := registry.Latest("mi_001", now.Add(5*time.Second)); current.Status != CollectRequestDispatched {
		t.Fatalf("duplicate or mismatched time completed request: %#v", current)
	}
	a6 := pairedCollectReport(request.RequestID, "A", 6, "unavailable", "failure", now)
	registry.ObserveReports("mi_001", []ReportWrite{a6}, now.Add(6*time.Second))
	current, _ = registry.Latest("mi_001", now.Add(7*time.Second))
	if current.Status != CollectRequestCompleted || current.CollectionID != "A" ||
		current.ReportStatus != "success" || len(current.FamilyResults) != 2 ||
		current.FamilyResults[0].ReportStatus != "success" || current.FamilyResults[1].Availability != "unavailable" {
		t.Fatalf("same-group split batches or first recorded value lost: %#v", current)
	}
	// Completion freezes the selected group, even when a newer one finishes.
	b4 := pairedCollectReport(request.RequestID, "B", 4, "available", "success", b6.ObservedAt)
	registry.ObserveReports("mi_001", []ReportWrite{b4}, now.Add(8*time.Second))
	frozen, _ := registry.Latest("mi_001", now.Add(9*time.Second))
	if frozen.CollectionID != "A" || !frozen.CompletedAt.Equal(*current.CompletedAt) {
		t.Fatalf("late report changed completed request: %#v", frozen)
	}
}

func TestCollectRequestsSelectCompleteGroupAfterWholeBatch(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, reverse := range []bool{false, true} {
		registry := newTestCollectRequests()
		request, _ := registry.Request("mi_001", now)
		reports := []ReportWrite{
			pairedCollectReport(request.RequestID, "old", 4, "available", "success", now),
			pairedCollectReport(request.RequestID, "old", 6, "available", "success", now),
			pairedCollectReport(request.RequestID, "new-A", 4, "available", "success", now.Add(time.Second)),
			pairedCollectReport(request.RequestID, "new-A", 6, "available", "success", now.Add(time.Second)),
			pairedCollectReport(request.RequestID, "new-B", 4, "available", "failure", now.Add(time.Second)),
			pairedCollectReport(request.RequestID, "new-B", 6, "available", "success", now.Add(time.Second)),
			pairedCollectReport(request.RequestID, "newest-incomplete", 4, "available", "success", now.Add(2*time.Second)),
		}
		if reverse {
			for i, j := 0, len(reports)-1; i < j; i, j = i+1, j-1 {
				reports[i], reports[j] = reports[j], reports[i]
			}
		}
		registry.ObserveReports("mi_001", reports, now.Add(3*time.Second))
		current, _ := registry.Latest("mi_001", now.Add(4*time.Second))
		if current.CollectionID != "new-B" || current.ReportStatus != "partial" || current.Status != CollectRequestCompleted {
			t.Fatalf("batch order %v chose wrong collection: %#v", reverse, current)
		}
	}
}

func TestCollectRequestsRejectMalformedPairedTerminals(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for name, mutate := range map[string]func(*ReportWrite){
		"future revision": func(r *ReportWrite) {
			r.DiagnosticsJSON = []byte(strings.Replace(string(r.DiagnosticsJSON), `"collection_revision":4`, `"collection_revision":6`, 1))
			r.CollectionMetadata = agentapi.ParseCollectionMetadata(r.DiagnosticsJSON)
		},
		"missing collection ID": func(r *ReportWrite) {
			r.DiagnosticsJSON = []byte(`{"collection_revision":4,"collect_request_id":"ipqc_001"}`)
			r.CollectionMetadata = agentapi.ParseCollectionMetadata(r.DiagnosticsJSON)
		},
		"wrong expected families": func(r *ReportWrite) {
			r.DiagnosticsJSON = []byte(strings.Replace(string(r.DiagnosticsJSON), `[4,6]`, `[6,4]`, 1))
			r.CollectionMetadata = agentapi.ParseCollectionMetadata(r.DiagnosticsJSON)
		},
		"invalid availability": func(r *ReportWrite) {
			r.DiagnosticsJSON = []byte(strings.Replace(string(r.DiagnosticsJSON), `"available"`, `"offline"`, 1))
			r.CollectionMetadata = agentapi.ParseCollectionMetadata(r.DiagnosticsJSON)
		},
		"unavailable success": func(r *ReportWrite) {
			r.DiagnosticsJSON = []byte(strings.Replace(string(r.DiagnosticsJSON), `"available"`, `"unavailable"`, 1))
			r.CollectionMetadata = agentapi.ParseCollectionMetadata(r.DiagnosticsJSON)
			r.IPAddress, r.ErrorCode = "0.0.0.0", "address_family_unavailable"
		},
		"successful discovery marked unknown": func(r *ReportWrite) {
			r.DiagnosticsJSON = []byte(strings.Replace(string(r.DiagnosticsJSON), `"available"`, `"unknown"`, 1))
			r.CollectionMetadata = agentapi.ParseCollectionMetadata(r.DiagnosticsJSON)
		},
		"real address with lookup failure code": func(r *ReportWrite) { r.ErrorCode = "lookup_failed" },
		"metadata family mismatch":              func(r *ReportWrite) { r.IPVersion = 6; r.IPAddress = "2001:db8::10" },
		"address family mismatch":               func(r *ReportWrite) { r.IPAddress = "2001:db8::10" },
		"invalid address":                       func(r *ReportWrite) { r.IPAddress = "not-an-address" },
		"placeholder success":                   func(r *ReportWrite) { r.IPAddress = "0.0.0.0" },
		"placeholder wrong failure code":        func(r *ReportWrite) { r.Status = "failure"; r.IPAddress = "0.0.0.0"; r.ErrorCode = "timeout" },
		"unavailable nonplaceholder": func(r *ReportWrite) {
			r.DiagnosticsJSON = []byte(strings.Replace(string(r.DiagnosticsJSON), `"available"`, `"unavailable"`, 1))
			r.CollectionMetadata = agentapi.ParseCollectionMetadata(r.DiagnosticsJSON)
			r.Status, r.ErrorCode = "failure", "address_family_unavailable"
		},
		"unknown status":        func(r *ReportWrite) { r.Status = "unknown" },
		"wrong instance":        func(r *ReportWrite) { r.MonitoringInstanceID = "mi_other" },
		"zero observation time": func(r *ReportWrite) { r.ObservedAt = time.Time{} },
	} {
		t.Run(name, func(t *testing.T) {
			registry := newTestCollectRequests()
			request, _ := registry.Request("mi_001", now)
			bad := pairedCollectReport(request.RequestID, "round", 4, "available", "success", now)
			mutate(&bad)
			good := pairedCollectReport(request.RequestID, "round", 6, "available", "success", now)
			registry.ObserveReports("mi_001", []ReportWrite{bad, good}, now.Add(time.Second))
			current, _ := registry.Latest("mi_001", now.Add(2*time.Second))
			if !current.Active() || len(current.FamilyResults) != 1 || current.FamilyResults[0].IPVersion != 6 {
				t.Fatalf("malformed paired report advanced or downgraded request: %#v", current)
			}
		})
	}
}

func TestCollectRequestsAggregateFamilyTerminals(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name, availability4, status4, availability6, status6, want string
	}{
		{"dual success", "available", "success", "available", "success", "success"},
		{"IPv4 only", "available", "success", "unavailable", "failure", "success"},
		{"IPv6 only", "unavailable", "failure", "available", "success", "success"},
		{"IPv4 lookup failure", "available", "failure", "available", "success", "partial"},
		{"partial and unavailable", "available", "partial", "unavailable", "failure", "partial"},
		{"unknown failures", "unknown", "failure", "unknown", "failure", "failure"},
		{"unavailable both", "unavailable", "failure", "unavailable", "failure", "failure"},
		{"partial and failure", "available", "partial", "available", "failure", "partial"},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry := newTestCollectRequests()
			request, _ := registry.Request("mi_001", now)
			a := pairedCollectReport(request.RequestID, "round", 4, test.availability4, test.status4, now)
			b := pairedCollectReport(request.RequestID, "round", 6, test.availability6, test.status6, now)
			registry.ObserveReports("mi_001", []ReportWrite{a, b}, now.Add(time.Second))
			current, _ := registry.Latest("mi_001", now.Add(2*time.Second))
			if current.Status != CollectRequestCompleted || current.ReportStatus != test.want {
				t.Fatalf("aggregated = %#v, want %s", current, test.want)
			}
		})
	}
}

func TestCollectRequestsDoNotExposeMutableOrUnsafeResults(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	registry := newTestCollectRequests()
	request, _ := registry.Request("mi_001", now)
	registry.PendingRequest("mi_001", now, nil)
	a := pairedCollectReport(request.RequestID, "round", 4, "available", "partial", now)
	a.ErrorCode, a.ErrorSummary = "https://secret.example/?token=password", "private token password"
	registry.ObserveReports("mi_001", []ReportWrite{a}, now.Add(time.Second))
	first, _ := registry.Latest("mi_001", now.Add(2*time.Second))
	if first.FamilyResults[0].ErrorCode != "" || first.ErrorSummary != "" {
		t.Fatalf("unsafe diagnostics leaked: %#v", first)
	}
	first.FamilyResults[0].ReportStatus = "failure"
	*first.DispatchedAt = now.Add(time.Hour)
	reused, _ := registry.Request("mi_001", now.Add(3*time.Second))
	if reused.FamilyResults[0].ReportStatus != "partial" || !reused.DispatchedAt.Equal(now) {
		t.Fatalf("Latest exposed internal storage: %#v", reused)
	}
	reused.FamilyResults[0].IPVersion = 6
	b := pairedCollectReport(request.RequestID, "round", 6, "available", "success", now)
	registry.ObserveReports("mi_001", []ReportWrite{b}, now.Add(4*time.Second))
	completed, _ := registry.Latest("mi_001", now.Add(5*time.Second))
	if completed.ReportStatus != "partial" || completed.FamilyResults[0].IPVersion != 4 ||
		strings.Contains(completed.ErrorSummary, "password") {
		t.Fatalf("mutable Request result or unsafe summary leaked: %#v", completed)
	}
	*completed.CompletedAt = now
	frozen, _ := registry.Latest("mi_001", now.Add(6*time.Second))
	if !frozen.CompletedAt.Equal(now.Add(4 * time.Second)) {
		t.Fatalf("completion time was mutable: %#v", frozen)
	}
}

func TestCollectRequestsExpirePartialGroupsAndReplaceRequest(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	registry := newTestCollectRequests()
	request, _ := registry.Request("mi_001", now)
	a := pairedCollectReport(request.RequestID, "round", 4, "available", "success", now)
	b := pairedCollectReport(request.RequestID, "round", 6, "available", "success", now)
	registry.ObserveReports("mi_001", []ReportWrite{a}, now.Add(time.Second))
	registry.ObserveReports("mi_001", []ReportWrite{b}, now.Add(CollectRequestTTL))
	expired, _ := registry.Latest("mi_001", now.Add(CollectRequestTTL))
	if expired.Status != CollectRequestExpired || len(expired.FamilyResults) != 1 || registry.entries["mi_001"].groups != nil {
		t.Fatalf("TTL retained active groups or completed late: %#v", expired)
	}
	next, _ := registry.Request("mi_001", now.Add(CollectRequestTTL+time.Second))
	registry.ObserveReports("mi_001", []ReportWrite{a, b}, now.Add(CollectRequestTTL+2*time.Second))
	current, _ := registry.Latest("mi_001", now.Add(CollectRequestTTL+3*time.Second))
	if current.RequestID != next.RequestID || !current.Active() || len(current.FamilyResults) != 0 || current.CollectionScope != "" {
		t.Fatalf("old request reports polluted replacement: %#v", current)
	}
	pruneAt := next.ExpiresAt.Add(collectRequestRetention)
	if retained, ok := registry.Latest("mi_001", pruneAt.Add(-time.Nanosecond)); !ok || retained.Status != CollectRequestExpired || retained.CompletedAt != nil {
		t.Fatalf("replacement request must retain its own expired result: %#v, ok=%t", retained, ok)
	}
	if _, ok := registry.Latest("mi_001", pruneAt); ok {
		t.Fatal("retention must prune without requiring another Request call")
	}
}

func TestCollectRequestsRetainCompletedFromTerminalTime(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, status := range []string{"success", "partial", "failure"} {
		t.Run(status, func(t *testing.T) {
			registry := newTestCollectRequests()
			request, _ := registry.Request("mi_001", now)
			registry.PendingRequest("mi_001", now.Add(time.Second), nil)
			four := pairedCollectReport(request.RequestID, "round", 4, "available", status, now)
			six := pairedCollectReport(request.RequestID, "round", 6, "available", status, now)
			registry.ObserveReports("mi_001", []ReportWrite{four}, now.Add(time.Minute))
			completedAt := now.Add(9 * time.Minute)
			registry.ObserveReports("mi_001", []ReportWrite{six}, completedAt)
			completed, ok := registry.Latest("mi_001", completedAt)
			if !ok || completed.Status != CollectRequestCompleted || completed.ReportStatus != status ||
				completed.CompletedAt == nil || !completed.CompletedAt.Equal(completedAt) ||
				len(completed.FamilyResults) != 2 || registry.entries["mi_001"].groups != nil {
				t.Fatalf("paired completion = %#v, ok=%t, want retained terminal metadata without groups", completed, ok)
			}
			if retained, ok := registry.Latest("mi_001", now.Add(31*time.Minute)); !ok || !reflect.DeepEqual(retained, completed) {
				t.Fatalf("request completed at t9 must still be readable at t31: %#v, ok=%t", retained, ok)
			}
			registry.ObserveReports("mi_001", []ReportWrite{four, six}, now.Add(32*time.Minute))
			cutoff := completedAt.Add(collectRequestRetention)
			if retained, ok := registry.Latest("mi_001", cutoff.Add(-time.Nanosecond)); !ok || !reflect.DeepEqual(retained, completed) {
				t.Fatalf("late reports must not change or extend completed retention: %#v, ok=%t", retained, ok)
			}
			for _, at := range []time.Time{cutoff, cutoff.Add(time.Nanosecond)} {
				if _, ok := registry.Latest("mi_001", at); ok {
					t.Fatalf("completed request retained at or after t39 cutoff: %s", at)
				}
			}
		})
	}
}

func TestCollectRequestsRetainUnsupportedFailureFromTerminalTime(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	registry := newTestCollectRequests()
	request, _ := registry.Request("mi_001", now, []int{4})
	failedAt := now.Add(9 * time.Minute)
	incapable := 0
	if got := registry.PendingRequest("mi_001", failedAt, &incapable); got.RequestID != "" {
		t.Fatalf("unsupported agent dispatched request: %#v", got)
	}
	failed, ok := registry.Latest("mi_001", now.Add(31*time.Minute))
	if !ok || failed.Status != CollectRequestFailed || failed.ErrorCode != "unsupported_agent" ||
		failed.CompletedAt == nil || !failed.CompletedAt.Equal(failedAt) || failed.DispatchedAt != nil ||
		registry.entries["mi_001"].groups != nil {
		t.Fatalf("unsupported failure must remain readable from t9 to t39: %#v, ok=%t", failed, ok)
	}
	registry.ObserveReports("mi_001", []ReportWrite{legacyCollectReport(request.RequestID, "success", now)}, now.Add(32*time.Minute))
	cutoff := failedAt.Add(collectRequestRetention)
	if retained, ok := registry.Latest("mi_001", cutoff.Add(-time.Nanosecond)); !ok || !reflect.DeepEqual(retained, failed) {
		t.Fatalf("late report revived or modified failed request: %#v, ok=%t", retained, ok)
	}
	if _, ok := registry.Latest("mi_001", cutoff); ok {
		t.Fatal("unsupported failure must be pruned at t39 cutoff")
	}
}

func TestCollectRequestsUnvisitedExpiryRetainedUntilLogicalCutoff(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, dispatched := range []bool{false, true} {
		t.Run(fmt.Sprintf("dispatched=%t", dispatched), func(t *testing.T) {
			registry := newTestCollectRequests()
			request, _ := registry.Request("mi_001", now)
			if dispatched {
				registry.PendingRequest("mi_001", now, nil)
			}
			four := pairedCollectReport(request.RequestID, "round", 4, "available", "success", now)
			registry.ObserveReports("mi_001", []ReportWrite{four}, now.Add(time.Second))
			expired, ok := registry.Latest("mi_001", now.Add(35*time.Minute))
			if !ok || expired.Status != CollectRequestExpired || expired.CompletedAt != nil ||
				!expired.ExpiresAt.Equal(now.Add(CollectRequestTTL)) || len(expired.FamilyResults) != 1 ||
				registry.entries["mi_001"].groups != nil {
				t.Fatalf("first read at t35 must return logical t10 expiry and release groups: %#v, ok=%t", expired, ok)
			}
			six := pairedCollectReport(request.RequestID, "round", 6, "available", "success", now)
			registry.ObserveReports("mi_001", []ReportWrite{six}, now.Add(36*time.Minute))
			cutoff := request.ExpiresAt.Add(collectRequestRetention)
			if retained, ok := registry.Latest("mi_001", cutoff.Add(-time.Nanosecond)); !ok || !reflect.DeepEqual(retained, expired) {
				t.Fatalf("late paired report changed expired result or retention: %#v, ok=%t", retained, ok)
			}
			if _, ok := registry.Latest("mi_001", cutoff); ok {
				t.Fatal("unvisited request must be pruned at t40, not t65")
			}
		})
	}
}

func TestCollectRequestsExpiryBoundaries(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, status := range []string{CollectRequestPending, CollectRequestDispatched} {
		for _, delta := range []time.Duration{-time.Nanosecond, 0, time.Nanosecond} {
			t.Run(fmt.Sprintf("%s/%s", status, delta), func(t *testing.T) {
				registry := newTestCollectRequests()
				request, _ := registry.Request("mi_001", now)
				if status == CollectRequestDispatched {
					registry.PendingRequest("mi_001", now, nil)
				}
				four := pairedCollectReport(request.RequestID, "round", 4, "available", "success", now)
				registry.ObserveReports("mi_001", []ReportWrite{four}, now.Add(time.Second))
				at := request.ExpiresAt.Add(delta)
				current, ok := registry.Latest("mi_001", at)
				wantStatus := status
				if delta >= 0 {
					wantStatus = CollectRequestExpired
				}
				if !ok || current.Status != wantStatus || current.CompletedAt != nil {
					t.Fatalf("TTL boundary at %s = %#v, ok=%t, want %s", delta, current, ok, wantStatus)
				}
				if hasGroups := registry.entries["mi_001"].groups != nil; hasGroups != (delta < 0) {
					t.Fatalf("TTL boundary at %s retained groups=%t", delta, hasGroups)
				}
			})
		}
	}
}

func TestCollectRequestsPruneExpiresAllInstancesAndReleasesGroups(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	registry := newTestCollectRequests()
	for index, offset := range []time.Duration{0, 5 * time.Minute, 9 * time.Minute} {
		instanceID := fmt.Sprintf("mi_%03d", index+1)
		requestAt := now.Add(offset)
		request, _ := registry.Request(instanceID, requestAt)
		report := pairedCollectReport(request.RequestID, "round", 4, "available", "success", requestAt)
		report.MonitoringInstanceID = instanceID
		registry.ObserveReports(instanceID, []ReportWrite{report}, requestAt.Add(time.Second))
	}
	if _, ok := registry.Latest("mi_unknown", now.Add(CollectRequestTTL)); ok {
		t.Fatal("unknown instance unexpectedly has a request")
	}
	first := registry.entries["mi_001"]
	if first.request.Status != CollectRequestExpired || first.groups != nil || first.request.CompletedAt != nil {
		t.Fatalf("accessing unknown instance failed to expire the oldest request: %#v", first)
	}
	for _, instanceID := range []string{"mi_002", "mi_003"} {
		entry := registry.entries[instanceID]
		if !entry.request.Active() || entry.groups == nil {
			t.Fatalf("unexpired instance %s lost its active request or groups: %#v", instanceID, entry)
		}
	}
	dispatched := registry.PendingRequest("mi_003", now.Add(15*time.Minute), nil)
	second := registry.entries["mi_002"]
	if dispatched.Status != CollectRequestDispatched || registry.entries["mi_003"].groups == nil ||
		second.request.Status != CollectRequestExpired || second.groups != nil || second.request.CompletedAt != nil {
		t.Fatalf("interleaved access failed to expire only elapsed requests: dispatched=%#v second=%#v", dispatched, second)
	}
	registry.Latest("mi_unknown", now.Add(40*time.Minute))
	if _, ok := registry.entries["mi_001"]; ok {
		t.Fatal("oldest instance must be pruned at its t40 cutoff")
	}
	if len(registry.entries) != 2 || registry.entries["mi_002"].request.Status != CollectRequestExpired ||
		registry.entries["mi_003"].request.Status != CollectRequestExpired || registry.entries["mi_003"].groups != nil {
		t.Fatal("global cleanup must retain later terminal requests and release every expired group")
	}
	registry.Latest("mi_unknown", now.Add(45*time.Minute))
	if _, ok := registry.entries["mi_002"]; ok {
		t.Fatal("second instance must be pruned at its t45 cutoff")
	}
	if _, ok := registry.Latest("mi_003", now.Add(49*time.Minute-time.Nanosecond)); !ok {
		t.Fatal("third instance must remain until its own t49 cutoff")
	}
	registry.Latest("mi_unknown", now.Add(49*time.Minute))
	if len(registry.entries) != 0 {
		t.Fatalf("elapsed terminal requests remain: %#v", registry.entries)
	}
}

func TestCollectRequestsMissingCompletionUsesExpiryFallback(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, status := range []string{CollectRequestCompleted, CollectRequestFailed} {
		t.Run(status, func(t *testing.T) {
			registry := newTestCollectRequests()
			request, _ := registry.Request("mi_001", now)
			registry.entries["mi_001"].request.Status = status
			cutoff := request.ExpiresAt.Add(collectRequestRetention)
			if current, ok := registry.Latest("mi_001", now.Add(31*time.Minute)); !ok || current.Status != status || current.CompletedAt != nil {
				t.Fatalf("missing completion fell back to requested_at or fabricated a completion: %#v, ok=%t", current, ok)
			}
			if _, ok := registry.Latest("mi_001", cutoff.Add(-time.Nanosecond)); !ok {
				t.Fatal("missing completion must retain until expiry plus retention")
			}
			if _, ok := registry.Latest("mi_001", cutoff); ok {
				t.Fatal("missing completion must prune at expiry plus retention")
			}
		})
	}
}

func TestCollectRequestsNeverPruneActiveBeforeExpiry(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, status := range []string{CollectRequestPending, CollectRequestDispatched} {
		t.Run(status, func(t *testing.T) {
			registry := newTestCollectRequests()
			request, _ := registry.Request("mi_001", now)
			entry := registry.entries["mi_001"]
			entry.request.Status = status
			// A future expiry must govern active entries even if their age
			// already exceeds terminal retention.
			entry.request.ExpiresAt = now.Add(time.Hour)
			at := now.Add(collectRequestRetention + time.Minute)
			current, ok := registry.Latest("mi_001", at)
			if !ok || current.RequestID != request.RequestID || current.Status != status || current.CompletedAt != nil {
				t.Fatalf("active request was removed based on requested_at: %#v, ok=%t", current, ok)
			}
		})
	}
}
