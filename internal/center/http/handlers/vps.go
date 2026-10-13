package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/incidents"
	"houfeng/internal/center/ipquality"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/renewals"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

type vpsRunningTargetCounter interface {
	CountRunningTargetsForVPS(context.Context, string) (int, error)
}

type vpsIPQualitySummaryRepository interface {
	ListLatestSummariesForVPS(context.Context, []string) (map[string]ipquality.Summary, error)
}

type vpsHeartbeatPolicySource interface {
	GetEffectiveIncidentDefaults(context.Context) (centersettings.IncidentDefaults, bool, error)
}

type vpsExperienceLogRepository interface {
	ListExperienceLogsForVPS(context.Context, string) ([]renewals.ExperienceLogRecord, error)
	renewals.IdempotentExperienceLogRepository
}

type vpsPatchResponse struct {
	vpsassets.Record
	RenewalSubscriptionLinkage *vpsassets.RenewalSubscriptionLinkage `json:"renewal_subscription_linkage,omitempty"`
}

func VPSCollection(repo vpsassets.Repository, optionalDeps ...any) http.Handler {
	var linkRepo assetlinks.Repository
	var targetCounter vpsRunningTargetCounter
	var ipQualityRepo vpsIPQualitySummaryRepository
	var heartbeatPolicySource vpsHeartbeatPolicySource
	for _, dep := range optionalDeps {
		switch typed := dep.(type) {
		case assetlinks.Repository:
			linkRepo = typed
		case vpsRunningTargetCounter:
			targetCounter = typed
		case vpsIPQualitySummaryRepository:
			ipQualityRepo = typed
		case vpsHeartbeatPolicySource:
			heartbeatPolicySource = typed
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Has("usage_status") {
				writeError(w, http.StatusBadRequest, "usage_status was removed; use usage_tags")
				return
			}
			filters := vpsassets.NormalizeListFilters(vpsassets.ListFilters{
				UsageTag:        r.URL.Query().Get("usage_tag"),
				ProviderID:      r.URL.Query().Get("provider_id"),
				LifecycleStatus: vpsassets.LifecycleStatus(r.URL.Query().Get("lifecycle_status")),
				UsageStatus:     vpsassets.UsageStatus(r.URL.Query().Get("usage_status")),
				RenewalDecision: vpsassets.RenewalDecision(r.URL.Query().Get("renewal_decision")),
				AssetScope:      vpsassets.AssetScope(r.URL.Query().Get("asset_scope")),
			})
			if filters.AssetScope == "" {
				filters.AssetScope = vpsassets.AssetScopeCurrent
			}
			if err := vpsassets.ValidateListFilters(filters); err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			records, err := repo.ListVPSAssets(r.Context(), filters)
			if errors.Is(err, vpsassets.ErrInvalidVPSAssetInput) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			if linkRepo != nil {
				freshness := newVPSHeartbeatFreshness(heartbeatPolicySource)
				for i := range records {
					if err := enrichVPSAssetRuntimeSummary(r.Context(), linkRepo, targetCounter, freshness, &records[i]); err != nil {
						writeError(w, http.StatusInternalServerError, "internal server error")
						return
					}
				}
			}
			if ipQualityRepo != nil {
				if err := enrichVPSAssetIPQualitySummaries(r.Context(), ipQualityRepo, records); err != nil {
					writeError(w, http.StatusInternalServerError, "internal server error")
					return
				}
			}
			writeJSON(w, http.StatusOK, records)
		case http.MethodPost:
			var input vpsassets.CreateInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}

			input = vpsassets.NormalizeCreateInput(input)
			if err := vpsassets.ValidateCreateInput(input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			record, err := repo.CreateVPSAsset(r.Context(), input)
			if errors.Is(err, vpsassets.ErrInvalidVPSAssetInput) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			if linkRepo != nil {
				if err := enrichVPSAssetRuntimeSummary(r.Context(), linkRepo, targetCounter, newVPSHeartbeatFreshness(heartbeatPolicySource), &record); err != nil {
					writeError(w, http.StatusInternalServerError, "internal server error")
					return
				}
			}
			writeJSON(w, http.StatusCreated, record)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func VPSItem(repo vpsassets.Repository, optionalDeps ...any) http.Handler {
	var linkRepo assetlinks.Repository
	var targetCounter vpsRunningTargetCounter
	var ipQualityRepo vpsIPQualitySummaryRepository
	var heartbeatPolicySource vpsHeartbeatPolicySource
	for _, dep := range optionalDeps {
		switch typed := dep.(type) {
		case assetlinks.Repository:
			linkRepo = typed
		case vpsRunningTargetCounter:
			targetCounter = typed
		case vpsIPQualitySummaryRepository:
			ipQualityRepo = typed
		case vpsHeartbeatPolicySource:
			heartbeatPolicySource = typed
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID := strings.TrimPrefix(r.URL.Path, "/api/vps/")
		vpsID = strings.Trim(vpsID, "/")
		if vpsID == "" || strings.Contains(vpsID, "/") {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}

		switch r.Method {
		case http.MethodGet:
			record, err := repo.GetVPSAsset(r.Context(), vpsID)
			if errors.Is(err, vpsassets.ErrVPSAssetNotFound) {
				writeError(w, http.StatusNotFound, "vps asset not found")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			if ipQualityRepo != nil {
				if err := enrichVPSAssetIPQualitySummary(r.Context(), ipQualityRepo, &record); err != nil {
					writeError(w, http.StatusInternalServerError, "internal server error")
					return
				}
			}
			if linkRepo != nil {
				monitoringInstanceLinks, err := linkRepo.ListMonitoringInstancesForVPS(r.Context(), vpsID)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "internal server error")
					return
				}
				record.ActiveMonitoringInstanceLinkCount = countCurrentVPSMonitoringInstances(monitoringInstanceLinks)
				record.RunningMonitoringInstanceCount = countRunningVPSMonitoringInstances(record.LifecycleStatus, monitoringInstanceLinks)
				currentMonitoring, err := currentVPSMonitoringSummary(r.Context(), monitoringInstanceLinks, newVPSHeartbeatFreshness(heartbeatPolicySource))
				if err != nil {
					writeError(w, http.StatusInternalServerError, "internal server error")
					return
				}
				record.CurrentMonitoring = currentMonitoring
				runningTargetCount, countErr := countRunningVPSTargets(r.Context(), targetCounter, record.LifecycleStatus, record.VPSID)
				if countErr != nil {
					writeError(w, http.StatusInternalServerError, "internal server error")
					return
				}
				record.RunningTargetCount = runningTargetCount
				writeJSON(w, http.StatusOK, vpsDetailResponse{Record: record, MonitoringInstanceLinks: monitoringInstanceLinks})
				return
			}
			writeJSON(w, http.StatusOK, record)
		case http.MethodPatch:
			var input vpsassets.PatchInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}

			input = vpsassets.NormalizePatchInput(input)
			if err := vpsassets.ValidateOrdinaryPatchInput(input); err != nil {
				writeVPSInputError(w, err)
				return
			}
			if input.HasChanges() {
				current, err := repo.GetVPSAsset(r.Context(), vpsID)
				if errors.Is(err, vpsassets.ErrVPSAssetNotFound) {
					writeError(w, http.StatusNotFound, "vps asset not found")
					return
				}
				if err != nil {
					writeError(w, http.StatusInternalServerError, "internal server error")
					return
				}
				if current.LifecycleStatus == vpsassets.LifecycleCancelled || (current.LifecycleStatus == vpsassets.LifecycleArchived && !input.IsArchivedSupplement()) {
					writeCodedError(w, http.StatusConflict, "vps asset readonly", "vps_asset_readonly")
					return
				}
				expectedUpdatedAt, ok := parseMetadataPrecondition(r.Header.Get("If-Match"))
				if !ok || expectedUpdatedAt == nil {
					writeError(w, http.StatusBadRequest, "invalid input")
					return
				}
				input.ExpectedUpdatedAt = expectedUpdatedAt
			}

			record, err := repo.PatchVPSAsset(r.Context(), vpsID, input)
			if errors.Is(err, vpsassets.ErrVPSAssetNotFound) {
				writeError(w, http.StatusNotFound, "vps asset not found")
				return
			}
			if errors.Is(err, vpsassets.ErrVPSAssetConflict) {
				writeCodedError(w, http.StatusConflict, "vps asset conflict", "vps_asset_conflict")
				return
			}
			if errors.Is(err, vpsassets.ErrVPSAssetReadonly) {
				writeCodedError(w, http.StatusConflict, "vps asset readonly", "vps_asset_readonly")
				return
			}
			if errors.Is(err, vpsassets.ErrInvalidVPSAssetInput) || errors.Is(err, subscriptions.ErrInvalidSubscriptionInput) {
				writeVPSInputError(w, err)
				return
			}
			if errors.Is(err, subscriptions.ErrSubscriptionNotFound) {
				writeError(w, http.StatusNotFound, "subscription not found")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			if linkRepo != nil {
				if err := enrichVPSAssetRuntimeSummary(r.Context(), linkRepo, targetCounter, newVPSHeartbeatFreshness(heartbeatPolicySource), &record); err != nil {
					writeError(w, http.StatusInternalServerError, "internal server error")
					return
				}
			}
			writeJSON(w, http.StatusOK, record)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func writeVPSInputError(w http.ResponseWriter, err error) {
	var stateErr *vpsassets.StateCombinationError
	if errors.As(err, &stateErr) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid input", "field_errors": stateErr.FieldErrors})
		return
	}
	writeError(w, http.StatusBadRequest, "invalid input")
}

func enrichVPSAssetRuntimeSummary(ctx context.Context, linkRepo assetlinks.Repository, targetCounter vpsRunningTargetCounter, freshness *vpsHeartbeatFreshness, record *vpsassets.Record) error {
	monitoringInstanceLinks, err := linkRepo.ListMonitoringInstancesForVPS(ctx, record.VPSID)
	if err != nil {
		return err
	}
	record.ActiveMonitoringInstanceLinkCount = countCurrentVPSMonitoringInstances(monitoringInstanceLinks)
	record.RunningMonitoringInstanceCount = countRunningVPSMonitoringInstances(record.LifecycleStatus, monitoringInstanceLinks)
	record.CurrentMonitoring, err = currentVPSMonitoringSummary(ctx, monitoringInstanceLinks, freshness)
	if err != nil {
		return err
	}
	record.RunningTargetCount, err = countRunningVPSTargets(ctx, targetCounter, record.LifecycleStatus, record.VPSID)
	if err != nil {
		return err
	}
	return nil
}

func enrichVPSAssetIPQualitySummary(ctx context.Context, repo vpsIPQualitySummaryRepository, record *vpsassets.Record) error {
	if repo == nil || record == nil {
		return nil
	}
	summaries, err := repo.ListLatestSummariesForVPS(ctx, []string{record.VPSID})
	if err != nil {
		return err
	}
	if summary, ok := summaries[record.VPSID]; ok {
		value := summary
		record.IPQualitySummary = &value
	}
	return nil
}

func countRunningVPSMonitoringInstances(lifecycle vpsassets.LifecycleStatus, monitoringInstanceLinks []assetlinks.MonitoringInstanceSummary) int {
	if lifecycle != vpsassets.LifecycleActive {
		return 0
	}
	running := 0
	for _, link := range monitoringInstanceLinks {
		if link.IsCurrent && link.LifecycleStatus != monitoringinstances.LifecycleRetired && link.MonitoringStatus != monitoringinstances.MonitoringPaused {
			running++
		}
	}
	return running
}

// 每台 VPS 最多一个未退役实例（数据库部分唯一索引保证），取它的状态事实。
func currentVPSMonitoringSummary(ctx context.Context, links []assetlinks.MonitoringInstanceSummary, freshness *vpsHeartbeatFreshness) (*vpsassets.CurrentMonitoringSummary, error) {
	for _, link := range links {
		if !link.IsCurrent || link.LifecycleStatus == monitoringinstances.LifecycleRetired {
			continue
		}
		stale, err := freshness.stale(ctx, link.LastTrustedOnlineAt)
		if err != nil {
			return nil, err
		}
		return &vpsassets.CurrentMonitoringSummary{
			MonitoringInstanceID: link.MonitoringInstanceID,
			LifecycleStatus:      link.LifecycleStatus,
			MonitoringStatus:     link.MonitoringStatus,
			BindingStatus:        link.BindingStatus,
			CurrentHealthStatus:  link.CurrentHealthStatus,
			LastTrustedOnlineAt:  link.LastTrustedOnlineAt,
			HeartbeatStale:       stale,
		}, nil
	}
	return nil, nil
}

// vpsHeartbeatFreshness 与 VPS 总览、事件评估使用同一份持久化心跳策略（未保存时用默认值），
// 每个请求最多读取一次设置，并且只在确有心跳需要判定时才读取。
type vpsHeartbeatFreshness struct {
	source vpsHeartbeatPolicySource
	now    time.Time
	policy *incidents.HeartbeatIncidentPolicy
}

func newVPSHeartbeatFreshness(source vpsHeartbeatPolicySource) *vpsHeartbeatFreshness {
	return &vpsHeartbeatFreshness{source: source, now: time.Now().UTC()}
}

func (f *vpsHeartbeatFreshness) stale(ctx context.Context, lastTrustedOnlineAt *time.Time) (bool, error) {
	if lastTrustedOnlineAt == nil || lastTrustedOnlineAt.IsZero() {
		return false, nil
	}
	if f.policy == nil {
		defaults := centersettings.Default().IncidentDefaults
		if f.source != nil {
			persisted, exists, err := f.source.GetEffectiveIncidentDefaults(ctx)
			if err != nil {
				return false, err
			}
			if exists {
				defaults = persisted
			}
		}
		policy, err := incidents.HeartbeatIncidentPolicyFromSettings(centersettings.CenterSettings{IncidentDefaults: defaults})
		if err != nil {
			return false, err
		}
		f.policy = &policy
	}
	return incidents.HeartbeatIsStale(f.now, *lastTrustedOnlineAt, *f.policy), nil
}

func countCurrentVPSMonitoringInstances(links []assetlinks.MonitoringInstanceSummary) int {
	count := 0
	for _, link := range links {
		if link.IsCurrent && link.LifecycleStatus != monitoringinstances.LifecycleRetired {
			count++
		}
	}
	return count
}

func countRunningVPSTargets(ctx context.Context, targetCounter vpsRunningTargetCounter, lifecycle vpsassets.LifecycleStatus, vpsID string) (int, error) {
	if lifecycle != vpsassets.LifecycleActive {
		return 0, nil
	}
	if targetCounter == nil {
		return 0, nil
	}
	return targetCounter.CountRunningTargetsForVPS(ctx, vpsID)
}

func enrichVPSAssetIPQualitySummaries(ctx context.Context, repo vpsIPQualitySummaryRepository, records []vpsassets.Record) error {
	if repo == nil || len(records) == 0 {
		return nil
	}
	vpsIDs := make([]string, 0, len(records))
	for _, record := range records {
		vpsIDs = append(vpsIDs, record.VPSID)
	}
	summaries, err := repo.ListLatestSummariesForVPS(ctx, vpsIDs)
	if err != nil {
		return err
	}
	for i := range records {
		if summary, ok := summaries[records[i].VPSID]; ok {
			value := summary
			records[i].IPQualitySummary = &value
		}
	}
	return nil
}

type vpsDetailResponse struct {
	vpsassets.Record
	MonitoringInstanceLinks []assetlinks.MonitoringInstanceSummary `json:"monitoring_instance_links"`
}

func VPSTimeline(repo renewals.TimelineRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID, ok := parseVPSSubresourcePath(r.URL.Path, "timeline")
		if !ok {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		timeline, err := repo.GetVPSTimeline(r.Context(), vpsID)
		if errors.Is(err, renewals.ErrInvalidRenewalDecisionInput) {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		if errors.Is(err, renewals.ErrRenewalTimelineNotFound) {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, timeline)
	})
}

func VPSExperienceLogs(repo vpsExperienceLogRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID, ok := parseVPSSubresourcePath(r.URL.Path, "experience-logs")
		if !ok {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}

		switch r.Method {
		case http.MethodGet:
			records, err := repo.ListExperienceLogsForVPS(r.Context(), vpsID)
			if errors.Is(err, renewals.ErrInvalidAssetHistoryInput) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			if errors.Is(err, renewals.ErrAssetTimelineNotFound) {
				writeError(w, http.StatusNotFound, "vps asset not found")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, records)
		case http.MethodPost:
			var input renewals.CreateExperienceLogInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}

			input.VPSID = vpsID
			input = renewals.NormalizeCreateExperienceLogInput(input)
			if err := renewals.ValidateCreateExperienceLogInput(input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			key, ok := requestCreateIdempotencyKey(r)
			if !ok {
				writeInvalidCreateIdempotencyKey(w)
				return
			}

			record, replayed, err := repo.CreateExperienceLogIdempotent(r.Context(), input, key)
			if writeCreateIdempotencyError(w, err) {
				return
			}
			if errors.Is(err, renewals.ErrInvalidAssetHistoryInput) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			if errors.Is(err, renewals.ErrAssetTimelineNotFound) {
				writeError(w, http.StatusNotFound, "vps asset not found")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, idempotentCreateStatus(replayed), record)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}
