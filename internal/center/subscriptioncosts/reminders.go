package subscriptioncosts

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"houfeng/internal/center/incidents"
)

const DefaultReminderWorkerInterval = 6 * time.Hour
const reminderDispatchChannel = "dispatch"

type NotificationDispatcher interface {
	Dispatch(context.Context, string) []incidents.NotificationDelivery
}

type NotificationAuditWriter interface {
	AppendNotificationRecords(context.Context, []incidents.NotificationRecordWrite) error
}

type ReminderService struct {
	repo       Repository
	settings   SettingsRepository
	dispatcher NotificationDispatcher
	audit      NotificationAuditWriter
	now        func() time.Time
	logger     *slog.Logger
}

func NewReminderService(repo Repository, settings SettingsRepository, dispatcher NotificationDispatcher, audit NotificationAuditWriter, logger *slog.Logger) *ReminderService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ReminderService{
		repo:       repo,
		settings:   settings,
		dispatcher: dispatcher,
		audit:      audit,
		now:        time.Now,
		logger:     logger,
	}
}

func (s *ReminderService) Scan(ctx context.Context) error {
	settings, err := s.settings.GetSettings(ctx)
	if err != nil {
		return fmt.Errorf("get subscription reminder settings: %w", err)
	}
	offsets := settings.SubscriptionCost.DefaultReminderOffsetsDays
	candidates, err := s.repo.ListReminderCandidates(ctx, settings.SubscriptionCost, offsets)
	if err != nil {
		return fmt.Errorf("list subscription reminder candidates: %w", err)
	}
	for _, candidate := range candidates {
		if err := s.deliverCandidate(ctx, candidate); err != nil {
			s.logger.Warn("subscription reminder delivery failed", "subscription_id", candidate.SubscriptionID, "vps_id", candidate.VPSID, "err", err)
		}
	}
	return nil
}

func (s *ReminderService) deliverCandidate(ctx context.Context, candidate ReminderCandidate) error {
	summary := candidateSummary(candidate)
	deliveryID, inserted, err := s.repo.TryCreateReminderDelivery(ctx, ReminderDeliveryInput{
		SubscriptionID: candidate.SubscriptionID,
		VPSID:          candidate.VPSID,
		RenewAt:        candidate.RenewAt,
		OffsetDays:     candidate.OffsetDays,
		Kind:           candidate.Kind,
		Channel:        reminderDispatchChannel,
		Status:         string(incidents.DeliveryStatusSuppressed),
		Summary:        summary,
	})
	if err != nil {
		return fmt.Errorf("reserve subscription reminder delivery: %w", err)
	}
	if !inserted {
		return nil
	}

	deliveries := []incidents.NotificationDelivery(nil)
	if s.dispatcher != nil {
		deliveries = s.dispatcher.Dispatch(ctx, summary)
	}
	if len(deliveries) == 0 {
		deliveries = []incidents.NotificationDelivery{{
			Channel: incidents.NotificationChannelTelegram,
			Status:  incidents.DeliveryStatusSuppressed,
		}}
	}

	status, sentAt := summarizeDeliveries(deliveries, s.now)
	if err := s.repo.UpdateReminderDelivery(ctx, deliveryID, ReminderDeliveryUpdate{
		Status:  string(status),
		Summary: summary,
		SentAt:  sentAt,
	}); err != nil {
		return fmt.Errorf("update subscription reminder delivery: %w", err)
	}

	auditRecords := make([]incidents.NotificationRecordWrite, 0, len(deliveries))
	for _, delivery := range deliveries {
		recordSentAt := (*time.Time)(nil)
		if delivery.Status == incidents.DeliveryStatusSent {
			now := s.now().UTC()
			recordSentAt = &now
		}
		auditRecords = append(auditRecords, incidents.NotificationRecordWrite{
			ObjectType:     incidents.ObjectTypeSubscription,
			ObjectID:       candidate.SubscriptionID,
			Channel:        delivery.Channel,
			DeliveryStatus: delivery.Status,
			Summary:        summary,
			SentAt:         recordSentAt,
		})
	}
	if len(auditRecords) > 0 && s.audit != nil {
		if err := s.audit.AppendNotificationRecords(ctx, auditRecords); err != nil {
			return fmt.Errorf("append subscription notification audit records: %w", err)
		}
	}
	return nil
}

func summarizeDeliveries(deliveries []incidents.NotificationDelivery, now func() time.Time) (incidents.DeliveryStatus, *time.Time) {
	status := incidents.DeliveryStatusSuppressed
	for _, delivery := range deliveries {
		if delivery.Status == incidents.DeliveryStatusSent {
			sentAt := now().UTC()
			return incidents.DeliveryStatusSent, &sentAt
		}
		if delivery.Status == incidents.DeliveryStatusFailed {
			status = incidents.DeliveryStatusFailed
		}
	}
	return status, nil
}

type ReminderWorker struct {
	service  *ReminderService
	interval time.Duration
}

func NewReminderWorker(service *ReminderService, interval time.Duration) *ReminderWorker {
	if interval <= 0 {
		interval = DefaultReminderWorkerInterval
	}
	return &ReminderWorker{service: service, interval: interval}
}

func (w *ReminderWorker) Run(ctx context.Context) error {
	if w.service == nil {
		return nil
	}
	if err := w.service.Scan(ctx); err != nil {
		w.service.logger.Warn("subscription reminder scan failed", "err", err)
	}
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := w.service.Scan(ctx); err != nil {
				w.service.logger.Warn("subscription reminder scan failed", "err", err)
			}
		}
	}
}

func candidateSummary(candidate ReminderCandidate) string {
	name := strings.TrimSpace(candidate.DisplayName)
	if name == "" {
		name = candidate.VPSDisplayName
	}
	if name == "" {
		name = candidate.SubscriptionID
	}
	prefix := "订阅续费提醒"
	if candidate.Kind == ReminderKindDecisionAttention {
		prefix = "订阅决策关注"
	}
	cost := ""
	if candidate.MonthlyPriceBase != nil {
		cost = fmt.Sprintf("，月成本约 %.2f %s", *candidate.MonthlyPriceBase, candidate.BaseCurrency)
	}
	return fmt.Sprintf("%s：%s 将在 %s 续费，提前 %d 天%s。VPS 决策：%s，生命周期：%s。",
		prefix,
		name,
		candidate.RenewAt.Time.Format("2006-01-02"),
		candidate.OffsetDays,
		cost,
		emptyAs(candidate.RenewalDecision, "", "未记录"),
		emptyAs(candidate.LifecycleStatus, "", "未记录"),
	)
}
