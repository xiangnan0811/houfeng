package runtimefacts

import (
	"context"
	"testing"
	"time"

	"houfeng/internal/center/observations"
	"houfeng/internal/center/syncing"
)

func TestStreamHubPublishesHostSamplesToMatchingSubscribers(t *testing.T) {
	t.Parallel()

	hub := NewStreamHub()
	matching := hub.SubscribeHostSamples("mi_001")
	defer matching.Close()
	other := hub.SubscribeHostSamples("mi_002")
	defer other.Close()

	observedAt := time.Date(2026, time.April, 24, 9, 0, 0, 0, time.UTC)
	if err := hub.AfterSuccessfulSync(context.Background(), syncing.Batch{
		MonitoringInstanceID: "mi_001",
		Observations: observations.BatchWrite{HostSamples: []observations.HostSampleWrite{{
			ObservedAt:        observedAt,
			ReceivedAt:        observedAt.Add(time.Second),
			AgentVersion:      "agent/v0.1.0",
			CPUUsagePct:       42,
			MemUsedPct:        64,
			NetInBytesPerSec:  1024,
			NetOutBytesPerSec: 2048,
		}}},
	}, syncing.Result{}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}

	select {
	case message := <-matching.Messages:
		if message.Type != "host_sample" || message.MonitoringInstanceID != "mi_001" {
			t.Fatalf("message = %#v, want mi_001 host_sample", message)
		}
		if message.Sample.CPUUsagePct != 42 {
			t.Fatalf("CPUUsagePct = %v, want 42", message.Sample.CPUUsagePct)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for matching subscriber message")
	}

	select {
	case message := <-other.Messages:
		t.Fatalf("unexpected message for other subscriber: %#v", message)
	default:
	}
}

func TestStreamHubDoesNotBlockWhenSubscriberIsSlow(t *testing.T) {
	t.Parallel()

	hub := NewStreamHub()
	subscription := hub.SubscribeHostSamples("mi_001")
	defer subscription.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < hostSampleStreamBuffer*4; i++ {
			hub.publishHostSample(HostSample{
				MonitoringInstanceID: "mi_001",
				ObservedAt:           time.Date(2026, time.April, 24, 9, 0, i, 0, time.UTC),
			})
		}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publish blocked on slow subscriber")
	}
}
