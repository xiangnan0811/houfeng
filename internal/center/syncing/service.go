package syncing

import (
	"context"
	"errors"
	"time"

	"houfeng/internal/center/agentplan"
	"houfeng/internal/center/enrollment"
	"houfeng/internal/center/ipquality"
	"houfeng/internal/center/observations"
	"houfeng/internal/contracts/agentapi"
)

// MaxBatchItems is the admitted per-collection item limit for one agent sync
// request. The HTTP boundary also requires every heartbeat in a request to use
// one sync batch ID, so one admitted heartbeat batch contributes at most this
// many rows. Persistence and bounded recovery queries may rely on these ingress
// invariants; callers that bypass the HTTP handler must enforce both.
const MaxBatchItems = 256

var (
	ErrBindingNotAccepted = enrollment.ErrBindingNotAccepted
	ErrInvalidSyncToken   = enrollment.ErrInvalidSyncToken
	ErrHeartbeatRequired  = errors.New("heartbeat carrier required for sync batch")
)

type HeartbeatPayload = enrollment.HeartbeatPayload

// CommandResult carries the output of an executed pending action back from the agent.
type CommandResult struct {
	ActionID  string
	CommandID string
	Stdout    string
	Stderr    string
	ExitCode  int
}

type Batch struct {
	SessionID            string
	LiveSignal           *agentapi.LiveSignal
	MonitoringInstanceID string
	SyncToken            string
	Heartbeats           []HeartbeatPayload
	Observations         observations.BatchWrite
	IPQualityReports     []ipquality.ReportWrite
	CommandResults       []CommandResult
}

type ResultDisposition string

const (
	ResultDispositionRecorded       ResultDisposition = "recorded"
	ResultDispositionExactDuplicate ResultDisposition = "exact_duplicate"
	ResultDispositionSuppressed     ResultDisposition = "suppressed"
)

type Result struct {
	StopCollection bool
	Disposition    ResultDisposition
	AcceptedAt     time.Time
	Plan           agentplan.SyncPlan
}

type PostSyncProcessor interface {
	AfterSuccessfulSync(context.Context, Batch, Result) error
}

type CompositePostSyncProcessor struct {
	primary    PostSyncProcessor
	bestEffort []PostSyncProcessor
}

func NewCompositePostSyncProcessor(primary PostSyncProcessor, bestEffort ...PostSyncProcessor) PostSyncProcessor {
	return CompositePostSyncProcessor{primary: primary, bestEffort: bestEffort}
}

func (p CompositePostSyncProcessor) AfterSuccessfulSync(ctx context.Context, batch Batch, result Result) error {
	var primaryErr error
	if p.primary != nil {
		primaryErr = p.primary.AfterSuccessfulSync(ctx, batch, result)
	}
	for _, processor := range p.bestEffort {
		if processor == nil {
			continue
		}
		_ = processor.AfterSuccessfulSync(ctx, batch, result)
	}
	return primaryErr
}

type Repository interface {
	ApplyBatch(context.Context, Batch) (Result, error)
}

// IPQualityCollectCoordinator 在 sync 边界上把立即采集请求下发给 agent，并用回传报告完成请求。
type IPQualityCollectCoordinator interface {
	ObserveReports(monitoringInstanceID string, reports []ipquality.ReportWrite, now time.Time)
	PendingRequestID(monitoringInstanceID string, now time.Time) string
}

type Service struct {
	repo     Repository
	postSync PostSyncProcessor
	collect  IPQualityCollectCoordinator
}

func NewService(repo Repository, postSync ...PostSyncProcessor) *Service {
	service := &Service{repo: repo}
	if len(postSync) > 0 {
		service.postSync = postSync[0]
	}
	return service
}

// WithIPQualityCollectCoordinator 返回挂接立即采集协调器的新 Service。
func (s *Service) WithIPQualityCollectCoordinator(collect IPQualityCollectCoordinator) *Service {
	next := *s
	next.collect = collect
	return &next
}

func (s *Service) SyncBatch(ctx context.Context, batch Batch) (Result, error) {
	result, err := s.repo.ApplyBatch(ctx, batch)
	if err != nil {
		return Result{}, err
	}
	result = s.coordinateIPQualityCollect(batch, result)
	if s.postSync != nil && result.Disposition == ResultDispositionRecorded {
		if err := s.postSync.AfterSuccessfulSync(ctx, batch, result); err != nil {
			return Result{}, err
		}
	}
	return result, nil
}

func (s *Service) coordinateIPQualityCollect(batch Batch, result Result) Result {
	if s.collect == nil {
		return result
	}
	now := time.Now().UTC()
	// 只有本次真正入库的报告才能完成请求；被抑制（暂停/退役等）的批次没有保存报告。
	// 精确重复的批次在首次入库时已处理过。
	if len(batch.IPQualityReports) > 0 && result.Disposition == ResultDispositionRecorded {
		s.collect.ObserveReports(batch.MonitoringInstanceID, batch.IPQualityReports, now)
	}
	plan := result.Plan.IPQualityPlan
	if result.StopCollection || plan == nil || !plan.Enabled {
		return result
	}
	requestID := s.collect.PendingRequestID(batch.MonitoringInstanceID, now)
	if requestID == "" {
		return result
	}
	nextPlan := *plan
	nextPlan.Services = append([]string(nil), plan.Services...)
	nextPlan.CollectRequestID = requestID
	result.Plan.IPQualityPlan = &nextPlan
	return result
}
