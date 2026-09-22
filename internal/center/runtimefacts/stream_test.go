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
	networkRatesValid := false
	if err := hub.AfterSuccessfulSync(context.Background(), syncing.Batch{
		MonitoringInstanceID: "mi_001",
		Observations: observations.BatchWrite{HostSamples: []observations.HostSampleWrite{{
			ObservedAt:        observedAt,
			ReceivedAt:        observedAt.Add(time.Second),
			AgentVersion:      "agent/v0.1.0",
			Fingerprint:       "fp-001",
			SyncBatchID:       "sync-001",
			CPUUsagePct:       42,
			MemUsedPct:        64,
			NetInBytesPerSec:  1024,
			NetOutBytesPerSec: 2048,
			NetworkRatesValid: &networkRatesValid,
		}}},
	}, syncing.Result{Disposition: syncing.ResultDispositionRecorded}); err != nil {
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
		if message.Sample.NetworkRatesValid == nil || *message.Sample.NetworkRatesValid {
			t.Fatalf("NetworkRatesValid = %v, want explicit false", message.Sample.NetworkRatesValid)
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
func TestStreamHubPublishesOnlyRecordedSyncResults(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 24, 9, 0, 0, 0, time.UTC)
	receivedAt := observedAt.Add(time.Second)
	acceptedAt := receivedAt.Add(time.Second)
	tests := []struct {
		name        string
		disposition syncing.ResultDisposition
		wantMessage bool
	}{
		{name: "recorded", disposition: syncing.ResultDispositionRecorded, wantMessage: true},
		{name: "suppressed", disposition: syncing.ResultDispositionSuppressed},
		{name: "exact duplicate", disposition: syncing.ResultDispositionExactDuplicate},
		{name: "empty", disposition: ""},
		{name: "unknown", disposition: syncing.ResultDisposition("unknown")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub := NewStreamHub()
			subscription := hub.SubscribeHostSamples("mi_001")
			defer subscription.Close()

			if err := hub.AfterSuccessfulSync(context.Background(), syncing.Batch{
				MonitoringInstanceID: "mi_001",
				Observations: observations.BatchWrite{HostSamples: []observations.HostSampleWrite{{
					MonitoringInstanceID: "mi_001",
					ObservedAt:           observedAt,
					ReceivedAt:           receivedAt,
					AgentVersion:         "agent/v0.1.0",
					Fingerprint:          "fp-001",
					SyncBatchID:          "sync-recorded",
					CPUUsagePct:          42,
				}}},
			}, syncing.Result{Disposition: tt.disposition, AcceptedAt: acceptedAt}); err != nil {
				t.Fatalf("AfterSuccessfulSync() error = %v", err)
			}

			select {
			case message := <-subscription.Messages:
				if !tt.wantMessage {
					t.Fatalf("unexpected message for disposition %q: %#v", tt.disposition, message)
				}
				if message.MonitoringInstanceID != "mi_001" || message.Sample.CPUUsagePct != 42 {
					t.Fatalf("message = %#v, want mi_001 CPU=42 sample", message)
				}
				if !message.Sample.ReceivedAt.Equal(receivedAt) || !message.ReceivedAt.Equal(receivedAt) {
					t.Fatalf("received timestamps = sample=%v message=%v, want %v", message.Sample.ReceivedAt, message.ReceivedAt, receivedAt)
				}
			default:
				if tt.wantMessage {
					t.Fatalf("no message for recorded result")
				}
			}
		})
	}
}

func TestStreamHubDropsCrossInstanceHostSamples(t *testing.T) {
	t.Parallel()

	hub := NewStreamHub()
	subscription := hub.SubscribeHostSamples("mi_001")
	defer subscription.Close()
	observedAt := time.Date(2026, time.April, 24, 9, 0, 0, 0, time.UTC)
	if err := hub.AfterSuccessfulSync(context.Background(), syncing.Batch{
		MonitoringInstanceID: "mi_001",
		Observations: observations.BatchWrite{HostSamples: []observations.HostSampleWrite{
			{
				MonitoringInstanceID: "mi_002",
				ObservedAt:           observedAt,
				ReceivedAt:           observedAt.Add(time.Second),
				AgentVersion:         "agent/v0.1.0",
				Fingerprint:          "fp-002",
				SyncBatchID:          "cross-instance",
			},
			{
				ObservedAt:           observedAt.Add(time.Minute),
				ReceivedAt:           observedAt.Add(time.Minute + time.Second),
				AgentVersion:         "agent/v0.1.0",
				Fingerprint:          "fp-001",
				SyncBatchID:          "same-instance",
				CPUUsagePct:          7,
				MonitoringInstanceID: "",
			},
		}},
	}, syncing.Result{Disposition: syncing.ResultDispositionRecorded}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}

	select {
	case message := <-subscription.Messages:
		if message.Sample.SyncBatchID != "same-instance" {
			t.Fatalf("message = %#v, want cross-instance sample omitted", message)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for same-instance sample")
	}
	select {
	case message := <-subscription.Messages:
		t.Fatalf("unexpected extra message: %#v", message)
	default:
	}
}

func TestStreamHubStampsZeroReceivedAtFromAcceptedAt(t *testing.T) {
	t.Parallel()

	hub := NewStreamHub()
	subscription := hub.SubscribeHostSamples("mi_001")
	defer subscription.Close()

	observedAt := time.Date(2026, time.April, 24, 9, 0, 0, 0, time.UTC)
	acceptedAt := observedAt.Add(2 * time.Second)
	if err := hub.AfterSuccessfulSync(context.Background(), syncing.Batch{
		MonitoringInstanceID: "mi_001",
		Observations: observations.BatchWrite{HostSamples: []observations.HostSampleWrite{{
			ObservedAt:   observedAt,
			AgentVersion: "agent/v0.1.0",
			Fingerprint:  "fp-001",
			SyncBatchID:  "sync-agent-shape",
			CPUUsagePct:  11,
		}}},
	}, syncing.Result{Disposition: syncing.ResultDispositionRecorded, AcceptedAt: acceptedAt}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}

	select {
	case message := <-subscription.Messages:
		if message.Type != "host_sample" || message.MonitoringInstanceID != "mi_001" {
			t.Fatalf("message = %#v, want mi_001 host_sample", message)
		}
		if !message.Sample.ReceivedAt.Equal(acceptedAt) || !message.ReceivedAt.Equal(acceptedAt) {
			t.Fatalf("received timestamps = sample=%v message=%v, want %v", message.Sample.ReceivedAt, message.ReceivedAt, acceptedAt)
		}
		if !message.Sample.ObservedAt.Equal(observedAt) {
			t.Fatalf("ObservedAt = %v, want %v", message.Sample.ObservedAt, observedAt)
		}
		if message.Sample.SyncBatchID != "sync-agent-shape" || message.Sample.CPUUsagePct != 11 {
			t.Fatalf("message sample = %#v, want agent-shaped payload fields", message.Sample)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stamped host sample")
	}
}

func TestStreamHubDoesNotPublishWhenReceivedAtAndAcceptedAtAreZero(t *testing.T) {
	t.Parallel()

	hub := NewStreamHub()
	subscription := hub.SubscribeHostSamples("mi_001")
	defer subscription.Close()

	if err := hub.AfterSuccessfulSync(context.Background(), syncing.Batch{
		MonitoringInstanceID: "mi_001",
		Observations: observations.BatchWrite{HostSamples: []observations.HostSampleWrite{{
			ObservedAt:   time.Date(2026, time.April, 24, 9, 0, 0, 0, time.UTC),
			AgentVersion: "agent/v0.1.0",
			Fingerprint:  "fp-001",
			SyncBatchID:  "sync-zero-receipt",
		}}},
	}, syncing.Result{Disposition: syncing.ResultDispositionRecorded}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}

	select {
	case message := <-subscription.Messages:
		t.Fatalf("unexpected message for zero receipt times: %#v", message)
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
