package retention

import (
	"context"
	"time"

	centersettings "houfeng/internal/center/settings"
)

const DefaultWorkerInterval = time.Hour

type Policy struct {
	RawLayerDays          int
	AggregateLayerDays    int
	EventLayerDays        int
	NotificationLayerDays int
}

type Result struct {
	MonitoringInstanceAggregateRows     int64
	TargetAggregateRows                 int64
	DeletedHeartbeats                   int64
	DeletedHostSamples                  int64
	DeletedProbeObservations            int64
	DeletedMonitoringInstanceAggregates int64
	DeletedTargetAggregates             int64
	DeletedEvents                       int64
	DeletedNotifications                int64
}

type Repository interface {
	ApplyRetention(context.Context, Policy, time.Time) (Result, error)
}

type SettingsRepository interface {
	GetSettings(context.Context) (centersettings.CenterSettings, error)
}

func PolicyFromSettings(record centersettings.CenterSettings) (Policy, error) {
	validated, err := centersettings.Validate(record)
	if err != nil {
		return Policy{}, err
	}
	return Policy{
		RawLayerDays:          validated.RetentionPolicy.RawLayerDays,
		AggregateLayerDays:    validated.RetentionPolicy.AggregateLayerDays,
		EventLayerDays:        validated.RetentionPolicy.EventLayerDays,
		NotificationLayerDays: validated.RetentionPolicy.NotificationLayerDays,
	}, nil
}
