package ipquality

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"houfeng/internal/contracts/agentapi"
)

func scopedCollectReport(requestID, collectionID string, revision int, versions []int, family int, observedAt time.Time) ReportWrite {
	report := pairedCollectReport(requestID, collectionID, family, "available", "success", observedAt)
	report.MonitoringInstanceID = "mi"
	rawVersions := fmt.Sprint(versions[0])
	if len(versions) == 2 {
		rawVersions += ",6"
	}
	report.DiagnosticsJSON = []byte(fmt.Sprintf(`{"collection_revision":%d,"collection_id":%q,"expected_ip_versions":[%s],"ip_address_family":%d,"family_availability":"available","collect_request_id":%q}`, revision, collectionID, rawVersions, family, requestID))
	report.CollectionMetadata = agentapi.ParseCollectionMetadata(report.DiagnosticsJSON)
	return report
}

func TestCollectScopeConflictAndOwnedSnapshots(t *testing.T) {
	now := time.Now().UTC()
	registry := newTestCollectRequests()
	input := []int{4}
	request, err := registry.Request("mi", now, input)
	if err != nil {
		t.Fatal(err)
	}
	input[0], request.RequestedIPVersions[0] = 6, 6
	current, _ := registry.Latest("mi", now)
	if !slices.Equal(current.RequestedIPVersions, []int{4}) {
		t.Fatalf("scope shared memory: %#v", current)
	}
	conflict, err := registry.Request("mi", now, []int{6})
	if !errors.Is(err, ErrCollectionScopeConflict) || conflict.RequestID != current.RequestID || !slices.Equal(conflict.RequestedIPVersions, []int{4}) {
		t.Fatalf("conflict=%#v err=%v", conflict, err)
	}
	conflict.RequestedIPVersions[0] = 6
	revision := 5
	pending := registry.PendingRequest("mi", now, &revision)
	pending.RequestedIPVersions[0] = 6
	reused, err := registry.Request("mi", now, []int{4})
	if err != nil || !slices.Equal(reused.RequestedIPVersions, []int{4}) || reused.Status != CollectRequestDispatched {
		t.Fatalf("snapshot changed request: %#v %v", reused, err)
	}
	for _, bad := range [][]int{nil, {}, {6, 4}, {4, 4}, {5}} {
		if _, err := registry.Request("other", now, bad); !errors.Is(err, ErrInvalidCollectionScope) {
			t.Fatalf("invalid scope %v accepted: %v", bad, err)
		}
	}
}

func TestSingleScopeRequiresCurrentLiveCapability(t *testing.T) {
	now := time.Now().UTC()
	for _, family := range []int{4, 6} {
		t.Run(fmt.Sprint(family), func(t *testing.T) {
			registry := newTestCollectRequests()
			request, _ := registry.Request("mi", now, []int{family})
			if got := registry.PendingRequest("mi", now, nil); got.RequestID != "" {
				t.Fatal("no live evidence dispatched a single scope")
			}
			pending, _ := registry.Latest("mi", now)
			if pending.Status != CollectRequestPending || pending.DispatchedAt != nil {
				t.Fatalf("no live evidence changed status: %#v", pending)
			}
			incapable := 0
			if got := registry.PendingRequest("mi", now, &incapable); got.RequestID != "" {
				t.Fatal("old agent received single scope")
			}
			failed, _ := registry.Latest("mi", now)
			if failed.Status != CollectRequestFailed || failed.ErrorCode != "unsupported_agent" || failed.CompletedAt == nil || failed.DispatchedAt != nil {
				t.Fatalf("incapable agent did not fail safely: %#v", failed)
			}
			registry.ObserveReports("mi", []ReportWrite{scopedCollectReport(request.RequestID, "old", 5, []int{family}, family, now)}, now)
			unchanged, _ := registry.Latest("mi", now)
			if unchanged.Status != CollectRequestFailed {
				t.Fatal("late report revived failed request")
			}
			next, _ := registry.Request("mi", now.Add(time.Second), []int{family})
			capable := 5
			dispatched := registry.PendingRequest("mi", now.Add(time.Second), &capable)
			if dispatched.RequestID != next.RequestID || dispatched.Status != CollectRequestDispatched {
				t.Fatalf("capable dispatch=%#v", dispatched)
			}
			legacy := legacyCollectReport(next.RequestID, "success", now)
			legacy.MonitoringInstanceID = "mi"
			registry.ObserveReports("mi", []ReportWrite{legacy, scopedCollectReport(next.RequestID, "wrong", 5, []int{4, 6}, family, now)}, now.Add(time.Second))
			active, _ := registry.Latest("mi", now.Add(time.Second))
			if !active.Active() || len(active.FamilyResults) != 0 {
				t.Fatalf("wrong scope completed: %#v", active)
			}
			registry.ObserveReports("mi", []ReportWrite{scopedCollectReport(next.RequestID, "single", 5, []int{family}, family, now)}, now.Add(2*time.Second))
			completed, _ := registry.Latest("mi", now.Add(2*time.Second))
			if completed.Status != CollectRequestCompleted || completed.CollectionScope != CollectScopeSingleFamily || len(completed.FamilyResults) != 1 || completed.ReportStatus != "success" {
				t.Fatalf("single terminal did not complete: %#v", completed)
			}
		})
	}
}

func TestCollectionGroupsDoNotMixRevisionsOrObservedTimes(t *testing.T) {
	now := time.Now().UTC()
	for _, revision := range []int{4, 5} {
		registry := newTestCollectRequests()
		request, _ := registry.Request("mi", now)
		four := scopedCollectReport(request.RequestID, "round", revision, []int{4, 6}, 4, now)
		wrongRevision := 9 - revision
		sixWrongRevision := scopedCollectReport(request.RequestID, "round", wrongRevision, []int{4, 6}, 6, now)
		sixWrongTime := scopedCollectReport(request.RequestID, "round", revision, []int{4, 6}, 6, now.Add(time.Second))
		registry.ObserveReports("mi", []ReportWrite{four, sixWrongRevision, sixWrongTime, four}, now.Add(time.Second))
		active, _ := registry.Latest("mi", now.Add(time.Second))
		if !active.Active() || len(active.FamilyResults) != 1 {
			t.Fatalf("mixed groups completed: %#v", active)
		}
		registry.ObserveReports("mi", []ReportWrite{scopedCollectReport(request.RequestID, "round", revision, []int{4, 6}, 6, now)}, now.Add(2*time.Second))
		done, _ := registry.Latest("mi", now.Add(2*time.Second))
		if done.Status != CollectRequestCompleted || len(done.FamilyResults) != 2 {
			t.Fatalf("matching group incomplete: %#v", done)
		}
	}
}

func TestSingleScopeTTLAndConcurrentSnapshots(t *testing.T) {
	now := time.Now().UTC()
	registry := newTestCollectRequests()
	request, _ := registry.Request("mi", now, []int{6})
	var workers sync.WaitGroup
	for range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			capable := 5
			for range 20 {
				snapshot := registry.PendingRequest("mi", now, &capable)
				if snapshot.RequestID != request.RequestID || !slices.Equal(snapshot.RequestedIPVersions, []int{6}) {
					t.Errorf("incoherent snapshot: %#v", snapshot)
				}
				snapshot.RequestedIPVersions[0] = 4
				current, _ := registry.Latest("mi", now)
				current.RequestedIPVersions[0] = 4
			}
		}()
	}
	workers.Wait()
	capable := 5
	if got := registry.PendingRequest("mi", request.ExpiresAt, &capable); got.RequestID != "" {
		t.Fatal("request dispatched at TTL equality")
	}
	registry.ObserveReports("mi", []ReportWrite{scopedCollectReport(request.RequestID, "late", 5, []int{6}, 6, now)}, request.ExpiresAt)
	expired, _ := registry.Latest("mi", request.ExpiresAt)
	if expired.Status != CollectRequestExpired || len(expired.FamilyResults) != 0 {
		t.Fatalf("late single report revived request: %#v", expired)
	}
}
