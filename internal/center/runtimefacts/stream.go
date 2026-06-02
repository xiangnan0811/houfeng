package runtimefacts

import (
	"context"
	"sync"
	"time"

	"houfeng/internal/center/observations"
	"houfeng/internal/center/syncing"
)

const hostSampleStreamBuffer = 16

type HostSampleStreamMessage struct {
	Type                 string     `json:"type"`
	MonitoringInstanceID string     `json:"monitoring_instance_id"`
	Sample               HostSample `json:"sample"`
	ReceivedAt           time.Time  `json:"received_at"`
}

type HostSampleSubscription struct {
	Messages    <-chan HostSampleStreamMessage
	unsubscribe func()
}

func (s HostSampleSubscription) Close() {
	if s.unsubscribe != nil {
		s.unsubscribe()
	}
}

type StreamHub struct {
	mu          sync.Mutex
	subscribers map[string]map[chan HostSampleStreamMessage]struct{}
}

func NewStreamHub() *StreamHub {
	return &StreamHub{subscribers: map[string]map[chan HostSampleStreamMessage]struct{}{}}
}

func (h *StreamHub) SubscribeHostSamples(monitoringInstanceID string) HostSampleSubscription {
	ch := make(chan HostSampleStreamMessage, hostSampleStreamBuffer)
	h.mu.Lock()
	if h.subscribers == nil {
		h.subscribers = map[string]map[chan HostSampleStreamMessage]struct{}{}
	}
	if h.subscribers[monitoringInstanceID] == nil {
		h.subscribers[monitoringInstanceID] = map[chan HostSampleStreamMessage]struct{}{}
	}
	h.subscribers[monitoringInstanceID][ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	return HostSampleSubscription{
		Messages: ch,
		unsubscribe: func() {
			once.Do(func() {
				h.mu.Lock()
				defer h.mu.Unlock()
				delete(h.subscribers[monitoringInstanceID], ch)
				if len(h.subscribers[monitoringInstanceID]) == 0 {
					delete(h.subscribers, monitoringInstanceID)
				}
			})
		},
	}
}

func (h *StreamHub) AfterSuccessfulSync(_ context.Context, batch syncing.Batch, _ syncing.Result) error {
	for _, sample := range batch.Observations.HostSamples {
		if sample.MonitoringInstanceID == "" {
			sample.MonitoringInstanceID = batch.MonitoringInstanceID
		}
		h.publishHostSample(hostSampleFromWrite(sample))
	}
	return nil
}

func (h *StreamHub) publishHostSample(sample HostSample) {
	if h == nil || sample.MonitoringInstanceID == "" {
		return
	}

	message := HostSampleStreamMessage{
		Type:                 "host_sample",
		MonitoringInstanceID: sample.MonitoringInstanceID,
		Sample:               sample,
		ReceivedAt:           sample.ReceivedAt,
	}

	h.mu.Lock()
	subscribers := make([]chan HostSampleStreamMessage, 0, len(h.subscribers[sample.MonitoringInstanceID]))
	for ch := range h.subscribers[sample.MonitoringInstanceID] {
		subscribers = append(subscribers, ch)
	}
	h.mu.Unlock()

	for _, ch := range subscribers {
		select {
		case ch <- message:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- message:
			default:
			}
		}
	}
}

func hostSampleFromWrite(sample observations.HostSampleWrite) HostSample {
	return HostSample{
		MonitoringInstanceID: sample.MonitoringInstanceID,
		ObservedAt:           sample.ObservedAt,
		ReceivedAt:           sample.ReceivedAt,
		AgentVersion:         sample.AgentVersion,
		Fingerprint:          sample.Fingerprint,
		CPUUsagePct:          sample.CPUUsagePct,
		Load1:                sample.Load1,
		Load5:                sample.Load5,
		Load15:               sample.Load15,
		MemUsedPct:           sample.MemUsedPct,
		MemAvailableBytes:    sample.MemAvailableBytes,
		MemTotalBytes:        sample.MemTotalBytes,
		SwapUsedPct:          sample.SwapUsedPct,
		DiskUsedPct:          sample.DiskUsedPct,
		DiskTotalBytes:       sample.DiskTotalBytes,
		InodeUsedPct:         sample.InodeUsedPct,
		NetInBytesPerSec:     sample.NetInBytesPerSec,
		NetOutBytesPerSec:    sample.NetOutBytesPerSec,
		CPUIOWaitPct:         sample.CPUIOWaitPct,
		CPUStealPct:          sample.CPUStealPct,
		DiskReadBytesPerSec:  sample.DiskReadBytesPerSec,
		DiskWriteBytesPerSec: sample.DiskWriteBytesPerSec,
		DiskBusyPct:          sample.DiskBusyPct,
		UptimeSeconds:        sample.UptimeSeconds,
		MaintenanceContext:   sample.MaintenanceContext,
		IsBackfilled:         sample.IsBackfilled,
		SyncBatchID:          sample.SyncBatchID,
		Containers:           sample.Containers,
	}
}
