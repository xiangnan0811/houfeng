package retention

import (
	"context"
	"time"

	centersettings "houfeng/internal/center/settings"
)

const DefaultWorkerInterval = time.Hour

type Policy struct {
	RawLayerDays       int
	AggregateLayerDays int
	LiveHeartbeatDays  int
}

func (p Policy) EffectiveLiveHeartbeatDays() int {
	if p.LiveHeartbeatDays == 0 {
		return p.RawLayerDays
	}
	return p.LiveHeartbeatDays
}

type Result struct {
	MonitoringInstanceAggregateRows     int64
	TargetAggregateRows                 int64
	DeletedHeartbeats                   int64
	DeletedLiveHeartbeatsEarly          int64
	DeletedHostSamples                  int64
	DeletedProbeObservations            int64
	DeletedMonitoringInstanceAggregates int64
	DeletedTargetAggregates             int64
	Skipped                             bool
}

type Repository interface {
	ApplyRetention(context.Context, Policy, time.Time) (Result, error)
	ClearExpiredCommandOutputs(context.Context, time.Time) (int64, error)
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
		RawLayerDays:       validated.RetentionPolicy.RawLayerDays,
		AggregateLayerDays: validated.RetentionPolicy.AggregateLayerDays,
		LiveHeartbeatDays:  validated.RetentionPolicy.LiveHeartbeatDays,
	}, nil
}
