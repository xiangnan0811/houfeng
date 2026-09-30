package ipquality

import (
	"fmt"
	"strings"
	"testing"
	"time"
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

	if got := requests.PendingRequestID("mi_unknown", now); got != "" {
		t.Fatalf("PendingRequestID(unknown) = %q, want empty", got)
	}
	if got := requests.PendingRequestID("mi_001", now.Add(5*time.Second)); got != request.RequestID {
		t.Fatalf("PendingRequestID() = %q, want %q", got, request.RequestID)
	}
	dispatched, _ := requests.Latest("mi_001", now.Add(6*time.Second))
	if dispatched.Status != CollectRequestDispatched || dispatched.DispatchedAt == nil {
		t.Fatalf("latest = %#v, want dispatched", dispatched)
	}
	// 再次下发保持首次下发时间。
	requests.PendingRequestID("mi_001", now.Add(10*time.Second))
	again, _ := requests.Latest("mi_001", now.Add(11*time.Second))
	if !again.DispatchedAt.Equal(now.Add(5 * time.Second)) {
		t.Fatalf("DispatchedAt = %s, want first dispatch time", again.DispatchedAt)
	}

	// 周期报告、离线补传或其他请求的报告，不论时间多新，都不能完成本次请求。
	requests.ObserveReports("mi_001", []ReportWrite{
		{Status: "success", ObservedAt: now.Add(15 * time.Second)},
		{Status: "success", ObservedAt: now.Add(15 * time.Second), CollectRequestID: "ipqc_other"},
	}, now.Add(20*time.Second))
	if latest, _ := requests.Latest("mi_001", now.Add(21*time.Second)); latest.Status != CollectRequestDispatched {
		t.Fatalf("latest = %#v, unmatched reports must not complete the request", latest)
	}

	// 被认领的周期采集可能在发起前很久就开始，只要带回同一 ID 就完成。
	requests.ObserveReports("mi_001", []ReportWrite{{
		Status:           "failure",
		ObservedAt:       now.Add(-5 * time.Minute),
		ErrorSummary:     strings.Repeat("超时", 150),
		CollectRequestID: request.RequestID,
	}}, now.Add(30*time.Second))
	completed, _ := requests.Latest("mi_001", now.Add(31*time.Second))
	if completed.Status != CollectRequestCompleted || completed.ReportStatus != "failure" || completed.CompletedAt == nil {
		t.Fatalf("completed = %#v, want completed failure", completed)
	}
	if len([]rune(completed.ErrorSummary)) != maxCollectErrorSummaryRunes+1 {
		t.Fatalf("ErrorSummary runes = %d, want truncated to %d plus ellipsis", len([]rune(completed.ErrorSummary)), maxCollectErrorSummaryRunes)
	}
	if got := requests.PendingRequestID("mi_001", now.Add(32*time.Second)); got != "" {
		t.Fatalf("PendingRequestID(after completion) = %q, want empty", got)
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
	requests.PendingRequestID("mi_001", now)

	expiredAt := now.Add(CollectRequestTTL)
	if got := requests.PendingRequestID("mi_001", expiredAt); got != "" {
		t.Fatalf("PendingRequestID(expired) = %q, want empty", got)
	}
	expired, ok := requests.Latest("mi_001", expiredAt)
	if !ok || expired.Status != CollectRequestExpired {
		t.Fatalf("latest = %#v, want expired", expired)
	}
	requests.ObserveReports("mi_001", []ReportWrite{{Status: "success", CollectRequestID: request.RequestID}}, expiredAt.Add(time.Second))
	if late, _ := requests.Latest("mi_001", expiredAt.Add(2*time.Second)); late.Status != CollectRequestExpired {
		t.Fatalf("late report must not revive an expired request: %#v", late)
	}

	requests.Request("mi_002", now.Add(collectRequestRetention+time.Second))
	if _, ok := requests.Latest("mi_001", now.Add(collectRequestRetention+time.Second)); ok {
		t.Fatal("request older than retention must be pruned")
	}
}

func TestCollectRequestIDFromDiagnostics(t *testing.T) {
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
		if got := CollectRequestIDFromDiagnostics([]byte(raw)); got != want {
			t.Fatalf("CollectRequestIDFromDiagnostics(%s) = %q, want %q", raw, got, want)
		}
	}
}
