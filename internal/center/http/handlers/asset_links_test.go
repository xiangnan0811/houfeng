package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/vpsassets"
)

type fakeLinkedMonitoringInstanceCreator struct {
	result             monitoringinstances.Record
	linkResult         assetlinks.Record
	err                error
	vpsID              string
	input              monitoringinstances.CreateInput
	linkNote           string
	legacyCalls        int
	idempotentCalls    int
	idempotentWire     monitoringinstances.LinkedCreateWireIdentity
	idempotentKey      string
	idempotentReplayed bool
}

type statefulLinkedMonitoringInstanceResult struct {
	record monitoringinstances.Record
	link   assetlinks.Record
}

type statefulLinkedMonitoringInstanceCreator struct {
	creates testIdempotentCreateState[statefulLinkedMonitoringInstanceResult]
	vpsRepo *sequentialVPSAssetRepository
}

type sequentialVPSAssetRepository struct {
	fakeVPSAssetRepository
	results  []vpsassets.Record
	errAfter int
}

func (r *sequentialVPSAssetRepository) GetVPSAsset(_ context.Context, vpsID string) (vpsassets.Record, error) {
	r.getVPSAssetCalls++
	r.getVPSAssetID = vpsID
	if r.getVPSAssetErr != nil {
		return vpsassets.Record{}, r.getVPSAssetErr
	}
	if r.errAfter > 0 && r.getVPSAssetCalls > r.errAfter {
		return vpsassets.Record{}, errors.New("later vps lookup failed")
	}
	if r.getVPSAssetCalls > len(r.results) {
		return vpsassets.Record{}, errors.New("unexpected vps lookup")
	}
	return r.results[r.getVPSAssetCalls-1], nil
}

func (c *statefulLinkedMonitoringInstanceCreator) CreateLinkedMonitoringInstanceIdempotent(ctx context.Context, vpsID string, wire monitoringinstances.LinkedCreateWireIdentity, key string) (monitoringinstances.Record, assetlinks.Record, bool, error) {
	identity := struct {
		VPSID        string
		WireIdentity monitoringinstances.LinkedCreateWireIdentity
	}{VPSID: vpsID, WireIdentity: wire}
	var lookupErr error
	result, replayed, err := c.creates.create(key, identity, func() statefulLinkedMonitoringInstanceResult {
		vps, err := c.vpsRepo.GetVPSAsset(ctx, vpsID)
		if err != nil {
			lookupErr = err
			return statefulLinkedMonitoringInstanceResult{}
		}
		record := monitoringinstances.Record{
			MonitoringInstanceID: "mi_sequence_001",
			DisplayName:          vps.DisplayName,
			Group:                wire.Group,
			Region:               vps.Region,
			City:                 vps.City,
			Provider:             vps.ProviderName,
			LifecycleStatus:      monitoringinstances.LifecyclePendingEnrollment,
		}
		return statefulLinkedMonitoringInstanceResult{
			record: record,
			link: assetlinks.Record{
				LinkID:               "vnl_sequence_001",
				VPSID:                vpsID,
				MonitoringInstanceID: record.MonitoringInstanceID,
			},
		}
	})
	if lookupErr != nil {
		return monitoringinstances.Record{}, assetlinks.Record{}, false, lookupErr
	}
	return result.record, result.link, replayed, err
}

func (f *fakeLinkedMonitoringInstanceCreator) CreateLinkedMonitoringInstance(_ context.Context, vpsID string, input monitoringinstances.CreateInput, linkNote string) (monitoringinstances.Record, assetlinks.Record, error) {
	f.legacyCalls++
	f.vpsID = vpsID
	f.input = input
	f.linkNote = linkNote
	if f.err != nil {
		return monitoringinstances.Record{}, assetlinks.Record{}, f.err
	}
	return f.result, f.linkResult, nil
}

func (f *fakeLinkedMonitoringInstanceCreator) CreateLinkedMonitoringInstanceIdempotent(_ context.Context, vpsID string, wire monitoringinstances.LinkedCreateWireIdentity, key string) (monitoringinstances.Record, assetlinks.Record, bool, error) {
	f.idempotentCalls++
	f.vpsID = vpsID
	f.idempotentWire = wire
	f.idempotentKey = key
	if f.err != nil {
		return monitoringinstances.Record{}, assetlinks.Record{}, false, f.err
	}
	return f.result, f.linkResult, f.idempotentReplayed, nil
}

type fakeAssetLinkRepository struct {
	linkMonitoringInstanceResult        assetlinks.Record
	linkMonitoringInstanceErr           error
	linkMonitoringInstanceVPSID         string
	linkMonitoringInstanceInput         assetlinks.LinkInput
	unlinkMonitoringInstanceResult      assetlinks.Record
	unlinkMonitoringInstanceErr         error
	unlinkMonitoringInstanceVPSID       string
	unlinkMonitoringInstanceInput       assetlinks.UnlinkInput
	listMonitoringInstancesForVPSResult []assetlinks.MonitoringInstanceSummary
	listMonitoringInstancesForVPSErr    error
	listMonitoringInstancesForVPSID     string
	listVPSForMonitoringInstanceResult  []assetlinks.VPSSummary
	listVPSForMonitoringInstanceErr     error
	listVPSForMonitoringInstanceID      string
	countActiveLinksForVPSVal           int
	countActiveLinksForVPSErr           error
	countActiveLinksForVPSID            string
}

func (f *fakeAssetLinkRepository) LinkMonitoringInstance(_ context.Context, vpsID string, input assetlinks.LinkInput) (assetlinks.Record, error) {
	f.linkMonitoringInstanceVPSID = vpsID
	f.linkMonitoringInstanceInput = input
	if f.linkMonitoringInstanceErr != nil {
		return assetlinks.Record{}, f.linkMonitoringInstanceErr
	}
	return f.linkMonitoringInstanceResult, nil
}

func (f *fakeAssetLinkRepository) UnlinkMonitoringInstance(_ context.Context, vpsID string, input assetlinks.UnlinkInput) (assetlinks.Record, error) {
	f.unlinkMonitoringInstanceVPSID = vpsID
	f.unlinkMonitoringInstanceInput = input
	if f.unlinkMonitoringInstanceErr != nil {
		return assetlinks.Record{}, f.unlinkMonitoringInstanceErr
	}
	return f.unlinkMonitoringInstanceResult, nil
}

func (f *fakeAssetLinkRepository) ListMonitoringInstancesForVPS(_ context.Context, vpsID string) ([]assetlinks.MonitoringInstanceSummary, error) {
	f.listMonitoringInstancesForVPSID = vpsID
	return f.listMonitoringInstancesForVPSResult, f.listMonitoringInstancesForVPSErr
}

func (f *fakeAssetLinkRepository) ListVPSForMonitoringInstance(_ context.Context, monitoringInstanceID string) ([]assetlinks.VPSSummary, error) {
	f.listVPSForMonitoringInstanceID = monitoringInstanceID
	return f.listVPSForMonitoringInstanceResult, f.listVPSForMonitoringInstanceErr
}

func (f *fakeAssetLinkRepository) CountActiveLinksForVPS(_ context.Context, vpsID string) (int, error) {
	f.countActiveLinksForVPSID = vpsID
	return f.countActiveLinksForVPSVal, f.countActiveLinksForVPSErr
}

func TestVPSLinkMonitoringInstanceCreatesLink(t *testing.T) {
	now := time.Date(2026, time.May, 9, 16, 0, 0, 0, time.UTC)
	repo := &fakeAssetLinkRepository{linkMonitoringInstanceResult: assetlinks.Record{
		LinkID:               "vnl_001",
		VPSID:                "vps_001",
		MonitoringInstanceID: "mi_001",
		LinkedAt:             now,
		Note:                 "primary",
	}}

	handler := handlers.VPSLinkMonitoringInstance(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/link-monitoring-instance", strings.NewReader(`{"monitoring_instance_id":" mi_001 ","note":" primary "}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusCreated)
	}
	if repo.linkMonitoringInstanceVPSID != "vps_001" || repo.linkMonitoringInstanceInput.MonitoringInstanceID != "mi_001" || repo.linkMonitoringInstanceInput.Note != "primary" {
		t.Fatalf("link input VPS/monitoring ID/note matches = %t/%t/%t", repo.linkMonitoringInstanceVPSID == "vps_001", repo.linkMonitoringInstanceInput.MonitoringInstanceID == "mi_001", repo.linkMonitoringInstanceInput.Note == "primary")
	}
	var body assetlinks.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body error type = %T", err)
	}
	if body.LinkID != "vnl_001" {
		t.Fatalf("link_id = %q, want vnl_001", body.LinkID)
	}
}

func TestVPSUnlinkMonitoringInstanceEndsActiveLink(t *testing.T) {
	now := time.Date(2026, time.May, 9, 16, 0, 0, 0, time.UTC)
	unlinkedAt := now.Add(time.Minute)
	repo := &fakeAssetLinkRepository{unlinkMonitoringInstanceResult: assetlinks.Record{
		LinkID:               "vnl_001",
		VPSID:                "vps_001",
		MonitoringInstanceID: "mi_001",
		LinkedAt:             now,
		UnlinkedAt:           &unlinkedAt,
		Note:                 "rotated",
	}}

	handler := handlers.VPSUnlinkMonitoringInstance(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/unlink-monitoring-instance", strings.NewReader(`{"monitoring_instance_id":" mi_001 ","note":" rotated "}`))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.unlinkMonitoringInstanceVPSID != "vps_001" || repo.unlinkMonitoringInstanceInput.MonitoringInstanceID != "mi_001" || repo.unlinkMonitoringInstanceInput.Note != "rotated" {
		t.Fatalf("unlink input VPS/monitoring ID/note matches = %t/%t/%t", repo.unlinkMonitoringInstanceVPSID == "vps_001", repo.unlinkMonitoringInstanceInput.MonitoringInstanceID == "mi_001", repo.unlinkMonitoringInstanceInput.Note == "rotated")
	}
	var body assetlinks.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body error type = %T", err)
	}
	if body.UnlinkedAt == nil {
		t.Fatalf("unlinked_at = nil, want historical unlink timestamp")
	}
}

func TestVPSMonitoringInstancesListsActiveMonitoringInstanceSummaries(t *testing.T) {
	now := time.Date(2026, time.May, 9, 16, 0, 0, 0, time.UTC)
	repo := &fakeAssetLinkRepository{listMonitoringInstancesForVPSResult: []assetlinks.MonitoringInstanceSummary{{
		MonitoringInstanceID:       "mi_001",
		DisplayName:                "Tokyo MonitoringInstance",
		Provider:                   "MonitoringInstance Hint",
		MonitoringStatus:           "启用",
		CurrentHealthStatus:        "关注",
		CurrentActiveIncidentCount: 1,
		LinkedAt:                   now,
	}}}

	handler := handlers.VPSMonitoringInstances(repo, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/vps/vps_001/monitoring-instances", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if repo.listMonitoringInstancesForVPSID != "vps_001" {
		t.Fatalf("list vps id = %q, want vps_001", repo.listMonitoringInstancesForVPSID)
	}
	var body []assetlinks.MonitoringInstanceSummary
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body error type = %T", err)
	}
	if len(body) != 1 || body[0].MonitoringInstanceID != "mi_001" || body[0].CurrentHealthStatus != "关注" {
		t.Fatalf("response count/ID/health matches = %d/%t/%t", len(body), len(body) == 1 && body[0].MonitoringInstanceID == "mi_001", len(body) == 1 && body[0].CurrentHealthStatus == "关注")
	}
}

func TestVPSMonitoringInstancesDelegatesEmptyWireIdentityToRepository(t *testing.T) {
	now := time.Date(2026, time.May, 9, 16, 0, 0, 0, time.UTC)
	creator := &fakeLinkedMonitoringInstanceCreator{
		result: monitoringinstances.Record{
			MonitoringInstanceID: "mi_001",
			DisplayName:          "Tokyo Edge",
			Provider:             "Acme Cloud",
			Region:               "ap-northeast-1",
			City:                 "Tokyo",
			LifecycleStatus:      monitoringinstances.LifecyclePendingEnrollment,
			MonitoringStatus:     monitoringinstances.MonitoringEnabled,
			BindingStatus:        monitoringinstances.BindingUnbound,
			CurrentHealthStatus:  monitoringinstances.HealthNormal,
			Labels:               []string{"edge", "prod"},
			Note:                 "primary asset note",
			CreatedAt:            now,
			UpdatedAt:            now,
		},
		linkResult: assetlinks.Record{
			LinkID:               "vnl_001",
			VPSID:                "vps_path",
			MonitoringInstanceID: "mi_001",
			LinkedAt:             now,
			Note:                 "created from vps detail",
		},
	}

	handler := handlers.VPSMonitoringInstances(&fakeAssetLinkRepository{}, creator)
	req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_path/monitoring-instances", strings.NewReader(`{}`))
	req.Header.Set("Idempotency-Key", "monitoring-create-001")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusCreated)
	}
	if creator.vpsID != "vps_path" {
		t.Fatalf("creator vps id = %q, want path vps id", creator.vpsID)
	}
	wire := creator.idempotentWire
	if wire.DisplayName != "" || wire.Group != "" || wire.Region != "" || wire.City != "" || wire.Provider != "" || len(wire.Labels) != 0 || wire.Note != "" || wire.LinkNote != "" {
		t.Fatal("wire identity did not preserve the exact empty request fields")
	}
	if creator.legacyCalls != 0 || creator.idempotentCalls != 1 || creator.idempotentKey != "monitoring-create-001" {
		t.Fatalf("create calls = legacy:%d idempotent:%d, want one idempotent call", creator.legacyCalls, creator.idempotentCalls)
	}
	var body struct {
		MonitoringInstanceID string            `json:"monitoring_instance_id"`
		Link                 assetlinks.Record `json:"link"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body error type = %T", err)
	}
	if body.MonitoringInstanceID != "mi_001" || body.Link.LinkID != "vnl_001" {
		t.Fatalf("response ID matches = %t/%t, want true/true", body.MonitoringInstanceID == "mi_001", body.Link.LinkID == "vnl_001")
	}
}

func TestVPSMonitoringInstancesPreservesNormalizedWireIdentity(t *testing.T) {
	creator := &fakeLinkedMonitoringInstanceCreator{}
	handler := handlers.VPSMonitoringInstances(&fakeAssetLinkRepository{}, creator)
	req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/monitoring-instances", strings.NewReader(`{
		"display_name":" Explicit name ",
		"group":" Edge ",
		"region":" Explicit region ",
		"city":" Explicit city ",
		"provider":" Explicit provider ",
		"labels":[" explicit ","explicit"],
		"note":" Explicit note ",
		"link_note":" Explicit link note "
	}`))
	req.Header.Set("Idempotency-Key", "monitoring-wire-001")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusCreated)
	}
	wire := creator.idempotentWire
	if wire.DisplayName != "Explicit name" || wire.Group != "Edge" || wire.Region != "Explicit region" || wire.City != "Explicit city" || wire.Provider != "Explicit provider" || len(wire.Labels) != 1 || wire.Labels[0] != "explicit" || wire.Note != "Explicit note" || wire.LinkNote != "Explicit link note" {
		t.Fatal("wire identity did not preserve all eight normalized request fields")
	}
}

func TestVPSMonitoringInstancesRequiresSingleValidIdempotencyKeyBeforeRepositories(t *testing.T) {
	tests := []struct {
		name string
		keys []string
	}{
		{name: "missing"},
		{name: "empty", keys: []string{""}},
		{name: "multiple", keys: []string{"monitoring-key-001", "monitoring-key-002"}},
		{name: "comma joined multiple", keys: []string{"monitoring-key-001,monitoring-key-002"}},
		{name: "too short", keys: []string{"short"}},
		{name: "too long", keys: []string{strings.Repeat("a", 129)}},
		{name: "invalid characters", keys: []string{"private/key"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creator := &fakeLinkedMonitoringInstanceCreator{}
			req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/monitoring-instances", strings.NewReader(`{}`))
			for _, key := range tt.keys {
				req.Header.Add("Idempotency-Key", key)
			}
			recorder := httptest.NewRecorder()

			handlers.VPSMonitoringInstances(&fakeAssetLinkRepository{}, creator).ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}
			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal error body error type = %T", err)
			}
			if body["code"] != "invalid_idempotency_key" {
				t.Fatalf("code = %q, want invalid_idempotency_key", body["code"])
			}
			if strings.Contains(recorder.Body.String(), "private/key") {
				t.Fatal("response disclosed rejected idempotency key")
			}
			if creator.legacyCalls != 0 || creator.idempotentCalls != 0 {
				t.Fatalf("repository calls = legacy:%d idempotent:%d, want none", creator.legacyCalls, creator.idempotentCalls)
			}
		})
	}
}

func TestVPSMonitoringInstancesValidatesBodyBeforeIdempotencyKey(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantError string
	}{
		{name: "invalid json", body: `{"note":`, wantError: "invalid json"},
		{name: "invalid input", body: `{"note":"` + strings.Repeat("x", monitoringinstances.LinkedCreateMaxNoteRunes+1) + `"}`, wantError: "invalid input"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creator := &fakeLinkedMonitoringInstanceCreator{}
			recorder := httptest.NewRecorder()
			handlers.VPSMonitoringInstances(&fakeAssetLinkRepository{}, creator).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/monitoring-instances", strings.NewReader(tt.body)))

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}
			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal error body error type = %T", err)
			}
			if body["error"] != tt.wantError || body["code"] != "" {
				t.Fatalf("error/code matches = %t/%t", body["error"] == tt.wantError, body["code"] == "")
			}
			if creator.legacyCalls != 0 || creator.idempotentCalls != 0 {
				t.Fatalf("repository calls = legacy:%d idempotent:%d, want none", creator.legacyCalls, creator.idempotentCalls)
			}
		})
	}
}

func TestVPSMonitoringInstancesMapsIdempotentCreateOutcomes(t *testing.T) {
	tests := []struct {
		name       string
		replayed   bool
		err        error
		wantStatus int
		wantCode   string
		wantError  string
	}{
		{name: "first create", wantStatus: http.StatusCreated},
		{name: "replay", replayed: true, wantStatus: http.StatusOK},
		{name: "reused key", err: monitoringinstances.ErrIdempotencyKeyReused, wantStatus: http.StatusConflict, wantCode: "idempotency_key_reused"},
		{name: "repository rejects key", err: monitoringinstances.ErrInvalidIdempotencyKey, wantStatus: http.StatusBadRequest, wantCode: "invalid_idempotency_key"},
		{name: "monitoring invalid input", err: monitoringinstances.ErrInvalidCreateInput, wantStatus: http.StatusBadRequest, wantError: "invalid input"},
		{name: "link invalid input", err: assetlinks.ErrInvalidVPSMonitoringInstanceLinkInput, wantStatus: http.StatusBadRequest, wantError: "invalid input"},
		{name: "link missing", err: assetlinks.ErrVPSMonitoringInstanceLinkNotFound, wantStatus: http.StatusNotFound, wantError: "vps asset not found"},
		{name: "link conflict", err: assetlinks.ErrVPSMonitoringInstanceLinkConflict, wantStatus: http.StatusConflict, wantError: "vps monitoring instance link conflict"},
		{name: "active exists", err: assetlinks.ErrVPSActiveMonitoringInstanceExists, wantStatus: http.StatusConflict, wantError: "vps active monitoring instance exists"},
		{name: "internal", err: errors.New("private repository detail"), wantStatus: http.StatusInternalServerError, wantError: "internal server error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creator := &fakeLinkedMonitoringInstanceCreator{
				result:             monitoringinstances.Record{MonitoringInstanceID: "mi_stable"},
				linkResult:         assetlinks.Record{LinkID: "vnl_stable", VPSID: "vps_001", MonitoringInstanceID: "mi_stable"},
				err:                tt.err,
				idempotentReplayed: tt.replayed,
			}
			req := httptest.NewRequest(http.MethodPost, "/api/vps/vps_001/monitoring-instances", strings.NewReader(`{}`))
			req.Header.Set("Idempotency-Key", "monitoring-outcome-001")
			recorder := httptest.NewRecorder()

			handlers.VPSMonitoringInstances(&fakeAssetLinkRepository{}, creator).ServeHTTP(recorder, req)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			if creator.legacyCalls != 0 || creator.idempotentCalls != 1 {
				t.Fatalf("create calls = legacy:%d idempotent:%d, want one idempotent call", creator.legacyCalls, creator.idempotentCalls)
			}
			if tt.wantCode != "" || tt.wantError != "" {
				var body map[string]string
				if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
					t.Fatalf("unmarshal error body error type = %T", err)
				}
				if body["code"] != tt.wantCode || (tt.wantError != "" && body["error"] != tt.wantError) {
					t.Fatalf("error code/message matches = %t/%t", body["code"] == tt.wantCode, tt.wantError == "" || body["error"] == tt.wantError)
				}
				if strings.Contains(recorder.Body.String(), "private repository detail") {
					t.Fatal("response disclosed repository error detail")
				}
				return
			}
			var body struct {
				MonitoringInstanceID string            `json:"monitoring_instance_id"`
				Link                 assetlinks.Record `json:"link"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal response error type = %T", err)
			}
			if body.MonitoringInstanceID != "mi_stable" || body.Link.LinkID != "vnl_stable" {
				t.Fatalf("response monitoring/link ID matches = %t/%t", body.MonitoringInstanceID == "mi_stable", body.Link.LinkID == "vnl_stable")
			}
		})
	}
}

func TestVPSMonitoringInstancesReceiptReplayDoesNotDependOnLaterVPSLookup(t *testing.T) {
	vpsRepo := &sequentialVPSAssetRepository{results: []vpsassets.Record{
		{
			VPSID:        "vps_repository_first",
			DisplayName:  "First fallback",
			ProviderName: "First provider",
			Region:       "First region",
			City:         "First city",
		},
	}, errAfter: 1}
	creator := &statefulLinkedMonitoringInstanceCreator{vpsRepo: vpsRepo}
	handler := handlers.VPSMonitoringInstances(&fakeAssetLinkRepository{}, creator)
	const path = "/api/vps/vps_sequence/monitoring-instances"
	const key = "monitoring-sequence-001"
	type response struct {
		MonitoringInstanceID string            `json:"monitoring_instance_id"`
		Link                 assetlinks.Record `json:"link"`
	}

	first := serveTestIdempotentCreate(handler, path, `{}`, key)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want %d", first.Code, http.StatusCreated)
	}
	firstRecord := decodeTestResponse[response](t, first)
	if firstRecord.MonitoringInstanceID != "mi_sequence_001" || firstRecord.Link.LinkID != "vnl_sequence_001" || firstRecord.Link.VPSID != "vps_sequence" {
		t.Fatal("first response did not contain the path-scoped materialized identities")
	}
	assertTestIdempotentCreateCounts(t, &creator.creates, 1, 1)

	replay := serveTestIdempotentCreate(handler, path, `{}`, key)
	if replay.Code != http.StatusOK {
		t.Fatalf("replay status = %d, want %d", replay.Code, http.StatusOK)
	}
	replayRecord := decodeTestResponse[response](t, replay)
	if replayRecord.MonitoringInstanceID != firstRecord.MonitoringInstanceID || replayRecord.Link.LinkID != firstRecord.Link.LinkID {
		t.Fatal("replay did not return the original monitoring identities")
	}
	assertTestIdempotentCreateCounts(t, &creator.creates, 2, 1)

	reused := serveTestIdempotentCreate(handler, path, `{"group":"Secondary"}`, key)
	if reused.Code != http.StatusConflict {
		t.Fatalf("reused-key status = %d, want %d", reused.Code, http.StatusConflict)
	}
	reusedError := decodeTestResponse[map[string]string](t, reused)
	if reusedError["code"] != "idempotency_key_reused" {
		t.Fatalf("reused-key code = %q, want idempotency_key_reused", reusedError["code"])
	}
	assertTestIdempotentCreateCounts(t, &creator.creates, 3, 1)
	if vpsRepo.getVPSAssetCalls != 1 || vpsRepo.getVPSAssetID != "vps_sequence" {
		t.Fatalf("vps lookup calls = %d, want one path-scoped materialization lookup", vpsRepo.getVPSAssetCalls)
	}
}

func TestMonitoringInstanceVPSListsActiveVPSSummaries(t *testing.T) {
	now := time.Date(2026, time.May, 9, 16, 0, 0, 0, time.UTC)
	repo := &fakeAssetLinkRepository{listVPSForMonitoringInstanceResult: []assetlinks.VPSSummary{{
		VPSID:           "vps_001",
		DisplayName:     "Tokyo VPS",
		ProviderName:    "Asset Provider",
		LifecycleStatus: "active",
		UsageStatus:     "in_use",
		RenewalDecision: "keep",
		LinkedAt:        now,
	}}}

	handler := handlers.MonitoringInstanceVPS(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/mi_001/vps", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if repo.listVPSForMonitoringInstanceID != "mi_001" {
		t.Fatalf("list monitoringInstance id = %q, want mi_001", repo.listVPSForMonitoringInstanceID)
	}
	var body []assetlinks.VPSSummary
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body error type = %T", err)
	}
	if len(body) != 1 || body[0].VPSID != "vps_001" || body[0].ProviderName != "Asset Provider" {
		t.Fatalf("response count/ID matches = %d/%t", len(body), len(body) == 1 && body[0].VPSID == "vps_001")
	}
}

func TestAssetLinkHandlersRejectInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
		body    string
	}{
		{name: "link blank monitoringInstance", handler: handlers.VPSLinkMonitoringInstance(&fakeAssetLinkRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/link-monitoring-instance", body: `{"monitoring_instance_id":" "}`},
		{name: "unlink blank monitoringInstance", handler: handlers.VPSUnlinkMonitoringInstance(&fakeAssetLinkRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/unlink-monitoring-instance", body: `{"monitoring_instance_id":" "}`},
		{name: "link unknown field", handler: handlers.VPSLinkMonitoringInstance(&fakeAssetLinkRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/link-monitoring-instance", body: `{"monitoring_instance_id":"mi_001","extra":true}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.method == http.MethodPost {
				req.Header.Set("Idempotency-Key", "asset-link-error-001")
			}
			recorder := httptest.NewRecorder()

			tt.handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
		})
	}
}

func TestAssetLinkHandlersMapDomainErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
		body    string
		want    int
	}{
		{name: "link conflict", handler: handlers.VPSLinkMonitoringInstance(&fakeAssetLinkRepository{linkMonitoringInstanceErr: assetlinks.ErrVPSMonitoringInstanceLinkConflict}), method: http.MethodPost, path: "/api/vps/vps_001/link-monitoring-instance", body: `{"monitoring_instance_id":"mi_001"}`, want: http.StatusConflict},
		{name: "link existing active monitoring instance", handler: handlers.VPSLinkMonitoringInstance(&fakeAssetLinkRepository{linkMonitoringInstanceErr: assetlinks.ErrVPSActiveMonitoringInstanceExists}), method: http.MethodPost, path: "/api/vps/vps_001/link-monitoring-instance", body: `{"monitoring_instance_id":"mi_001"}`, want: http.StatusConflict},
		{name: "link missing vps or monitoring instance", handler: handlers.VPSLinkMonitoringInstance(&fakeAssetLinkRepository{linkMonitoringInstanceErr: assetlinks.ErrVPSMonitoringInstanceLinkNotFound}), method: http.MethodPost, path: "/api/vps/vps_001/link-monitoring-instance", body: `{"monitoring_instance_id":"mi_001"}`, want: http.StatusNotFound},
		{name: "unlink missing active link", handler: handlers.VPSUnlinkMonitoringInstance(&fakeAssetLinkRepository{unlinkMonitoringInstanceErr: assetlinks.ErrVPSMonitoringInstanceLinkNotFound}), method: http.MethodPost, path: "/api/vps/vps_001/unlink-monitoring-instance", body: `{"monitoring_instance_id":"mi_001"}`, want: http.StatusNotFound},
		{name: "list monitoringInstances repo failure", handler: handlers.VPSMonitoringInstances(&fakeAssetLinkRepository{listMonitoringInstancesForVPSErr: errors.New("query failed")}, nil), method: http.MethodGet, path: "/api/vps/vps_001/monitoring-instances", want: http.StatusInternalServerError},
		{name: "create monitoringInstance missing vps", handler: handlers.VPSMonitoringInstances(&fakeAssetLinkRepository{}, &fakeLinkedMonitoringInstanceCreator{err: assetlinks.ErrVPSMonitoringInstanceLinkNotFound}), method: http.MethodPost, path: "/api/vps/vps_001/monitoring-instances", body: `{}`, want: http.StatusNotFound},
		{name: "create monitoringInstance link conflict", handler: handlers.VPSMonitoringInstances(&fakeAssetLinkRepository{}, &fakeLinkedMonitoringInstanceCreator{err: assetlinks.ErrVPSMonitoringInstanceLinkConflict}), method: http.MethodPost, path: "/api/vps/vps_001/monitoring-instances", body: `{}`, want: http.StatusConflict},
		{name: "create monitoringInstance existing active monitoring instance", handler: handlers.VPSMonitoringInstances(&fakeAssetLinkRepository{}, &fakeLinkedMonitoringInstanceCreator{err: assetlinks.ErrVPSActiveMonitoringInstanceExists}), method: http.MethodPost, path: "/api/vps/vps_001/monitoring-instances", body: `{}`, want: http.StatusConflict},
		{name: "list vps repo failure", handler: handlers.MonitoringInstanceVPS(&fakeAssetLinkRepository{listVPSForMonitoringInstanceErr: errors.New("query failed")}), method: http.MethodGet, path: "/api/monitoring-instances/mi_001/vps", want: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.method == http.MethodPost {
				req.Header.Set("Idempotency-Key", "asset-link-domain-error-001")
			}
			recorder := httptest.NewRecorder()

			tt.handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tt.want, recorder.Body.String())
			}
		})
	}
}

func TestAssetLinkHandlersRejectWrongMethodsAndMalformedPaths(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
		want    int
	}{
		{name: "vps monitoringInstances wrong method", handler: handlers.VPSMonitoringInstances(&fakeAssetLinkRepository{}, nil), method: http.MethodDelete, path: "/api/vps/vps_001/monitoring-instances", want: http.StatusMethodNotAllowed},
		{name: "monitoringInstance vps wrong method", handler: handlers.MonitoringInstanceVPS(&fakeAssetLinkRepository{}), method: http.MethodPost, path: "/api/monitoring-instances/mi_001/vps", want: http.StatusMethodNotAllowed},
		{name: "malformed vps path", handler: handlers.VPSLinkMonitoringInstance(&fakeAssetLinkRepository{}), method: http.MethodPost, path: "/api/vps/vps_001/link-monitoring-instance/extra", want: http.StatusNotFound},
		{name: "malformed monitoringInstance path", handler: handlers.MonitoringInstanceVPS(&fakeAssetLinkRepository{}), method: http.MethodGet, path: "/api/monitoring-instances/mi_001/vps/extra", want: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			recorder := httptest.NewRecorder()

			tt.handler.ServeHTTP(recorder, req)

			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tt.want, recorder.Body.String())
			}
		})
	}
}
