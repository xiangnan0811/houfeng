package store_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/incidents"
	"houfeng/internal/center/runtimefacts"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/store"
	"houfeng/internal/center/syncing"
	"houfeng/internal/center/targets"
)

const resourcePressureLabel = "resource-pressure-cadence"

// TestPostgresIntegrationResourcePressureWindows is the strict production
// service regression for the resource-pressure evidence boundary. It keeps
// every scenario in an isolated temporary PostgreSQL database, drives the
// production service through AfterSuccessfulSync, and verifies the persisted
// projection, events, notifications, and captured logs rather than relying on
// AfterSuccessfulSync's deliberately best-effort nil return.
func TestPostgresIntegrationResourcePressureWindows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	t.Run("jitter active iowait precedence and idempotency", func(t *testing.T) {
		fixture := newRuntimeStreamAuthFixture(t)
		reference := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
		latest := seedResourcePressureSeries(t, ctx, fixture.pool, reference.Add(-45*time.Minute), reference, 5*time.Second+time.Millisecond, func(_ int, _ time.Time, _ bool) resourcePressureSampleValues {
			return resourcePressureSampleValues{cpu: 96, memory: 10, load5: 1, marker: new(true)}
		})
		if !latest.Equal(reference) {
			t.Fatalf("jitter series latest = %s, want %s", latest, reference)
		}

		service, notifier, logs := newResourcePressureService(t, fixture, nil)
		evaluateResourcePressure(t, ctx, service, latest.Add(100*time.Millisecond), logs)
		started := readResourcePressureState(t, ctx, fixture.pool)
		assertResourcePressureActive(t, started, incidents.SeverityCritical, "CPU")
		if started.events["incident_started"] != 1 || started.notifications != 1 {
			t.Fatalf("jitter start persisted events=%v notifications=%d, want one started and one notification", started.events, started.notifications)
		}
		if len(notifier.messages) != 1 || !strings.Contains(notifier.messages[0], "CPU") {
			t.Fatalf("jitter notification messages = %#v, want one CPU notification", notifier.messages)
		}
		if event := started.lastEvent["incident_started"]; event.severity != string(incidents.SeverityCritical) || !strings.Contains(event.summary, "CPU") {
			t.Fatalf("jitter started event = %#v, want critical CPU event", event)
		}

		// A 5.1s query delay remains inside G=2*T+10ms. The same fact must
		// remain active without a duplicate transition or notification.
		evaluateResourcePressure(t, ctx, service, latest.Add(5*time.Second+100*time.Millisecond), logs)
		acceptedLate := readResourcePressureState(t, ctx, fixture.pool)
		if acceptedLate.activeCount != 1 || acceptedLate.events["incident_started"] != 1 || acceptedLate.notifications != 1 || len(notifier.messages) != 1 {
			t.Fatalf("5.1s-late state = %#v notifier=%#v, want unchanged active/idempotent state", acceptedLate, notifier.messages)
		}

		// 11s exceeds G for the 5s tier. The previous active record must be
		// held with its existing evidence rather than being silently recovered.
		evaluateResourcePressure(t, ctx, service, latest.Add(11*time.Second), logs)
		stale := readResourcePressureState(t, ctx, fixture.pool)
		if stale.activeCount != 1 || stale.severity != string(incidents.SeverityCritical) || stale.events["incident_started"] != 1 || stale.notifications != 1 || len(notifier.messages) != 1 {
			t.Fatalf("11s-stale state = %#v notifier=%#v, want held critical without side effects", stale, notifier.messages)
		}
		if !stale.lastEvaluatedAt.Equal(acceptedLate.lastEvaluatedAt) {
			t.Fatalf("11s-stale last_evaluated_at = %s, want held value %s", stale.lastEvaluatedAt, acceptedLate.lastEvaluatedAt)
		}

	})

	t.Run("severe iowait outranks lighter memory", func(t *testing.T) {
		fixture := newRuntimeStreamAuthFixture(t)
		settingsRepo := store.NewPostgresSettingsRepository(fixture.pool)
		setResourcePressureSettings(t, ctx, settingsRepo, targets.FrequencyTier1m, nil)
		reference := time.Now().UTC().Truncate(time.Microsecond).Add(-2 * time.Hour)
		latest := seedResourcePressureSeries(t, ctx, fixture.pool, reference.Add(-30*time.Minute), reference, time.Minute+time.Millisecond, func(_ int, _ time.Time, _ bool) resourcePressureSampleValues {
			return resourcePressureSampleValues{
				cpu:    10,
				iowait: 60,
				memory: 93,
				load5:  7,
				marker: new(true),
			}
		})
		service, notifier, logs := newResourcePressureService(t, fixture, settingsRepo)
		evaluateResourcePressure(t, ctx, service, latest.Add(100*time.Millisecond), logs)
		started := readResourcePressureState(t, ctx, fixture.pool)
		assertResourcePressureActive(t, started, incidents.SeverityCritical, "iowait")
		if started.events["incident_started"] != 1 || started.events["incident_escalated"] != 0 || started.notifications != 1 || len(notifier.messages) != 1 {
			t.Fatalf("iowait start state = %#v notifier=%#v, want one started notification and no escalation", started, notifier.messages)
		}
		if event := started.lastEvent["incident_started"]; event.severity != string(incidents.SeverityCritical) || !strings.Contains(event.summary, "iowait") {
			t.Fatalf("iowait started event = %#v, want critical iowait event", event)
		}
		startedIncidentID, startedSummary := started.incidentID, started.summary

		// A complete subsequent window removes the lighter memory/load
		// contributors without changing the severe iowait evidence.
		withoutLighter := latest.Add(30*time.Minute + 100*time.Millisecond)
		withoutLighterLatest := seedResourcePressureSeries(t, ctx, fixture.pool, withoutLighter.Add(-30*time.Minute), withoutLighter, time.Minute+time.Millisecond, func(_ int, _ time.Time, _ bool) resourcePressureSampleValues {
			return resourcePressureSampleValues{cpu: 10, iowait: 60, memory: 10, load5: 1, marker: new(true)}
		})
		evaluateResourcePressure(t, ctx, service, withoutLighterLatest.Add(100*time.Millisecond), logs)
		withoutLighterState := readResourcePressureState(t, ctx, fixture.pool)
		assertResourcePressureIowaitPersistence(t, withoutLighterState, startedIncidentID, startedSummary, notifier)

		// Restoring the lighter memory/load facts in another complete window
		// must still not create an escalation or duplicate notification.
		withLighter := withoutLighterLatest.Add(30*time.Minute + 100*time.Millisecond)
		withLighterLatest := seedResourcePressureSeries(t, ctx, fixture.pool, withLighter.Add(-30*time.Minute), withLighter, time.Minute+time.Millisecond, func(_ int, _ time.Time, _ bool) resourcePressureSampleValues {
			return resourcePressureSampleValues{cpu: 10, iowait: 60, memory: 93, load5: 7, marker: new(true)}
		})
		evaluateResourcePressure(t, ctx, service, withLighterLatest.Add(100*time.Millisecond), logs)
		withLighterState := readResourcePressureState(t, ctx, fixture.pool)
		assertResourcePressureIowaitPersistence(t, withLighterState, startedIncidentID, startedSummary, notifier)
	})

	t.Run("short window and exact gap boundary", func(t *testing.T) {
		t.Run("shorter than fifteen minutes remains unknown", func(t *testing.T) {
			fixture := newRuntimeStreamAuthFixture(t)
			reference := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
			latest := seedResourcePressureSeries(t, ctx, fixture.pool, reference.Add(-10*time.Minute), reference, 5*time.Second+time.Millisecond, func(_ int, _ time.Time, _ bool) resourcePressureSampleValues {
				return resourcePressureSampleValues{cpu: 96, memory: 10, load5: 1, marker: new(true)}
			})
			service, _, logs := newResourcePressureService(t, fixture, nil)
			evaluateResourcePressure(t, ctx, service, latest.Add(100*time.Millisecond), logs)
			state := readResourcePressureState(t, ctx, fixture.pool)
			if state.activeCount != 0 || state.events["incident_started"] != 0 || state.notifications != 0 {
				t.Fatalf("short-window state = %#v, want no incident, event, or notification", state)
			}
		})
		t.Run("invalid CPU marker blocks CPU but permits load alert", func(t *testing.T) {
			fixture := newRuntimeStreamAuthFixture(t)
			settingsRepo := store.NewPostgresSettingsRepository(fixture.pool)
			setResourcePressureSettings(t, ctx, settingsRepo, targets.FrequencyTier1m, nil)
			reference := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
			latest := seedResourcePressureSeries(t, ctx, fixture.pool, reference.Add(-30*time.Minute), reference, time.Minute+time.Millisecond, func(_ int, _ time.Time, _ bool) resourcePressureSampleValues {
				return resourcePressureSampleValues{cpu: 96, memory: 10, load5: 7, marker: new(false)}
			})
			service, _, logs := newResourcePressureService(t, fixture, settingsRepo)
			evaluateResourcePressure(t, ctx, service, latest.Add(100*time.Millisecond), logs)
			state := readResourcePressureState(t, ctx, fixture.pool)
			assertResourcePressureActive(t, state, incidents.SeverityAlert, "Load5")
			if state.events["incident_started"] != 1 || state.notifications != 1 {
				t.Fatalf("invalid-CPU state events=%v notifications=%d, want one load alert and notification", state.events, state.notifications)
			}
		})

		for _, tc := range []struct {
			name       string
			gap        time.Duration
			wantActive bool
		}{
			{name: "gap exactly G passes", gap: 2*time.Minute + 10*time.Millisecond, wantActive: true},
			{name: "gap greater than G at PostgreSQL precision fails", gap: 2*time.Minute + 10*time.Millisecond + time.Microsecond, wantActive: false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				fixture := newRuntimeStreamAuthFixture(t)
				settingsRepo := store.NewPostgresSettingsRepository(fixture.pool)
				setResourcePressureSettings(t, ctx, settingsRepo, targets.FrequencyTier1m, nil)
				reference := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
				times := resourcePressureTimesWithGap(reference.Add(-15*time.Minute), reference, time.Minute, tc.gap)
				latest := seedResourcePressureTimes(t, ctx, fixture.pool, times, func(_ int, _ time.Time, _ bool) resourcePressureSampleValues {
					return resourcePressureSampleValues{cpu: 10, memory: 10, load5: 7, marker: new(true)}
				})
				service, _, logs := newResourcePressureService(t, fixture, settingsRepo)
				evaluateResourcePressure(t, ctx, service, latest.Add(100*time.Millisecond), logs)
				state := readResourcePressureState(t, ctx, fixture.pool)
				if (state.activeCount > 0) != tc.wantActive {
					t.Fatalf("gap=%s state = %#v, want active=%t", tc.gap, state, tc.wantActive)
				}
				if tc.wantActive {
					assertResourcePressureActive(t, state, incidents.SeverityAlert, "Load5")
					if state.events["incident_started"] != 1 || state.notifications != 1 {
						t.Fatalf("exact-gap state events=%v notifications=%d, want one each", state.events, state.notifications)
					}
				} else if state.events["incident_started"] != 0 || state.notifications != 0 {
					t.Fatalf("over-gap state events=%v notifications=%d, want none", state.events, state.notifications)
				}
			})
		}
	})

	t.Run("safe recovery, suppression, invalid evidence, and idempotency", func(t *testing.T) {
		t.Run("live safe recovery sends once and preserves incident identity", func(t *testing.T) {
			fixture := newRuntimeStreamAuthFixture(t)
			settingsRepo := store.NewPostgresSettingsRepository(fixture.pool)
			setResourcePressureSettings(t, ctx, settingsRepo, targets.FrequencyTier1m, nil)
			activeReference := time.Now().UTC().Truncate(time.Microsecond).Add(-40 * time.Minute)
			activeLatest := seedResourcePressureSeries(t, ctx, fixture.pool, activeReference.Add(-30*time.Minute), activeReference, time.Minute+time.Millisecond, func(_ int, _ time.Time, _ bool) resourcePressureSampleValues {
				return resourcePressureSampleValues{cpu: 96, memory: 10, load5: 1, marker: new(true)}
			})
			service, notifier, logs := newResourcePressureService(t, fixture, settingsRepo)
			evaluateResourcePressure(t, ctx, service, activeLatest.Add(100*time.Millisecond), logs)
			started := readResourcePressureState(t, ctx, fixture.pool)
			assertResourcePressureActive(t, started, incidents.SeverityCritical, "CPU")
			if started.notifications != 1 || len(notifier.messages) != 1 || started.events["incident_started"] != 1 {
				t.Fatalf("live recovery setup = %#v notifier=%#v, want one started event and notification", started, notifier.messages)
			}
			if started.incidentID == "" || started.lastEvent["incident_started"].incidentID != started.incidentID {
				t.Fatalf("started incident identity = active:%q event:%q, want one identity", started.incidentID, started.lastEvent["incident_started"].incidentID)
			}

			recoveryReference := activeLatest.Add(30*time.Minute + 100*time.Millisecond)
			recoveryLatest := seedResourcePressureSeries(t, ctx, fixture.pool, recoveryReference.Add(-30*time.Minute), recoveryReference, time.Minute+time.Millisecond, func(_ int, _ time.Time, _ bool) resourcePressureSampleValues {
				return resourcePressureSampleValues{cpu: 10, memory: 10, load5: 1, marker: new(true)}
			})
			evaluateResourcePressure(t, ctx, service, recoveryLatest.Add(100*time.Millisecond), logs)
			recovered := readResourcePressureState(t, ctx, fixture.pool)
			if recovered.activeCount != 0 || recovered.events["incident_recovered"] != 1 || recovered.notifications != 2 || len(notifier.messages) != 2 {
				t.Fatalf("live recovery state = %#v notifier=%#v, want one recovered event, two sent records, and two sends", recovered, notifier.messages)
			}
			if recovered.lastEvent["incident_recovered"].incidentID != started.incidentID {
				t.Fatalf("recovered incident identity = %q, want started identity %q", recovered.lastEvent["incident_recovered"].incidentID, started.incidentID)
			}
			if len(recovered.notificationStatuses) != 2 || recovered.notificationStatuses[0] != string(incidents.DeliveryStatusSent) || recovered.notificationStatuses[1] != string(incidents.DeliveryStatusSent) {
				t.Fatalf("live recovery notification statuses = %#v, want sent/sent", recovered.notificationStatuses)
			}
			if len(recovered.notificationIncidentIDs) != 2 || recovered.notificationIncidentIDs[0] != started.incidentID || recovered.notificationIncidentIDs[1] != started.incidentID {
				t.Fatalf("live recovery notification identities = %#v, want %q twice", recovered.notificationIncidentIDs, started.incidentID)
			}
			if recovered.objectSummary != "" {
				t.Fatalf("live recovery object summary = %q, want empty", recovered.objectSummary)
			}

			// Re-evaluating after the active row is gone must not manufacture a
			// second recovered event or notification.
			evaluateResourcePressure(t, ctx, service, recoveryLatest.Add(100*time.Millisecond), logs)
			repeated := readResourcePressureState(t, ctx, fixture.pool)
			if repeated.activeCount != 0 || repeated.events["incident_recovered"] != 1 || repeated.notifications != 2 || len(notifier.messages) != 2 {
				t.Fatalf("repeated live recovery state = %#v notifier=%#v, want idempotent recovery", repeated, notifier.messages)
			}
		})

		t.Run("latest maintenance recovery suppresses send", func(t *testing.T) {
			fixture := newRuntimeStreamAuthFixture(t)
			settingsRepo := store.NewPostgresSettingsRepository(fixture.pool)
			setResourcePressureSettings(t, ctx, settingsRepo, targets.FrequencyTier1m, nil)
			activeReference := time.Now().UTC().Truncate(time.Microsecond).Add(-40 * time.Minute)
			activeLatest := seedResourcePressureSeries(t, ctx, fixture.pool, activeReference.Add(-30*time.Minute), activeReference, time.Minute+time.Millisecond, func(_ int, _ time.Time, _ bool) resourcePressureSampleValues {
				return resourcePressureSampleValues{cpu: 96, memory: 10, load5: 1, marker: new(true)}
			})
			service, notifier, logs := newResourcePressureService(t, fixture, settingsRepo)
			evaluateResourcePressure(t, ctx, service, activeLatest.Add(100*time.Millisecond), logs)
			started := readResourcePressureState(t, ctx, fixture.pool)
			assertResourcePressureActive(t, started, incidents.SeverityCritical, "CPU")

			recoveryReference := activeLatest.Add(30*time.Minute + 100*time.Millisecond)
			recoveryLatest := seedResourcePressureSeries(t, ctx, fixture.pool, recoveryReference.Add(-30*time.Minute), recoveryReference, time.Minute+time.Millisecond, func(_ int, _ time.Time, last bool) resourcePressureSampleValues {
				return resourcePressureSampleValues{cpu: 10, memory: 10, load5: 1, marker: new(true), maintenance: last}
			})
			evaluateResourcePressure(t, ctx, service, recoveryLatest.Add(100*time.Millisecond), logs)
			recovered := readResourcePressureState(t, ctx, fixture.pool)
			if recovered.activeCount != 0 || recovered.events["incident_recovered"] != 1 {
				t.Fatalf("suppressed recovery state = %#v, want one recovered event and no active row", recovered)
			}
			if recovered.notifications != 2 || len(notifier.messages) != 1 {
				t.Fatalf("suppressed recovery notifications=%d notifier=%#v, want one sent start and one suppressed recovery record", recovered.notifications, notifier.messages)
			}
			if len(recovered.notificationStatuses) != 2 || recovered.notificationStatuses[0] != string(incidents.DeliveryStatusSent) || recovered.notificationStatuses[1] != string(incidents.DeliveryStatusSuppressed) {
				t.Fatalf("recovery notification statuses = %#v, want sent then suppressed", recovered.notificationStatuses)
			}
			if recovered.lastEvent["incident_recovered"].incidentID != started.incidentID {
				t.Fatalf("suppressed recovery incident identity = %q, want started identity %q", recovered.lastEvent["incident_recovered"].incidentID, started.incidentID)
			}
			if recovered.objectSummary != "" {
				t.Fatalf("suppressed recovery object summary = %q, want empty", recovered.objectSummary)
			}
		})

		for _, tc := range []struct {
			name       string
			invalidCPU bool
			gap        bool
		}{
			{name: "invalid CPU evidence holds critical", invalidCPU: true},
			{name: "recovery gap holds critical", gap: true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				fixture := newRuntimeStreamAuthFixture(t)
				settingsRepo := store.NewPostgresSettingsRepository(fixture.pool)
				setResourcePressureSettings(t, ctx, settingsRepo, targets.FrequencyTier1m, nil)
				activeReference := time.Now().UTC().Truncate(time.Microsecond).Add(-40 * time.Minute)
				activeLatest := seedResourcePressureSeries(t, ctx, fixture.pool, activeReference.Add(-30*time.Minute), activeReference, time.Minute+time.Millisecond, func(_ int, _ time.Time, _ bool) resourcePressureSampleValues {
					return resourcePressureSampleValues{cpu: 96, memory: 10, load5: 1, marker: new(true)}
				})
				service, notifier, logs := newResourcePressureService(t, fixture, settingsRepo)
				evaluateResourcePressure(t, ctx, service, activeLatest.Add(100*time.Millisecond), logs)
				started := readResourcePressureState(t, ctx, fixture.pool)
				assertResourcePressureActive(t, started, incidents.SeverityCritical, "CPU")

				recoveryReference := activeLatest.Add(30*time.Minute + 100*time.Millisecond)
				var recoveryLatest time.Time
				if tc.invalidCPU {
					recoveryLatest = seedResourcePressureSeries(t, ctx, fixture.pool, recoveryReference.Add(-30*time.Minute), recoveryReference, time.Minute+time.Millisecond, func(_ int, _ time.Time, _ bool) resourcePressureSampleValues {
						return resourcePressureSampleValues{cpu: 10, memory: 10, load5: 1, marker: new(false)}
					})
				} else {
					recoveryTimes := resourcePressureTimesWithGap(recoveryReference.Add(-30*time.Minute), recoveryReference, time.Minute, 2*time.Minute+10*time.Millisecond+time.Microsecond)
					recoveryLatest = seedResourcePressureTimes(t, ctx, fixture.pool, recoveryTimes, func(_ int, _ time.Time, _ bool) resourcePressureSampleValues {
						return resourcePressureSampleValues{cpu: 10, memory: 10, load5: 1, marker: new(true)}
					})
				}
				evaluateResourcePressure(t, ctx, service, recoveryLatest.Add(100*time.Millisecond), logs)
				held := readResourcePressureState(t, ctx, fixture.pool)
				if held.activeCount != 1 || held.severity != string(incidents.SeverityCritical) || held.events["incident_started"] != 1 || held.events["incident_recovered"] != 0 || held.notifications != 1 || len(notifier.messages) != 1 {
					t.Fatalf("held recovery state = %#v notifier=%#v, want original active incident with no recovery side effects", held, notifier.messages)
				}
				if held.incidentID != started.incidentID || held.lastEvent["incident_started"].incidentID != started.incidentID {
					t.Fatalf("held recovery incident identity = active:%q started:%q event:%q, want unchanged identity", held.incidentID, started.incidentID, held.lastEvent["incident_started"].incidentID)
				}
			})
		}
	})

	t.Run("maintenance and backfill cannot bridge active coverage", func(t *testing.T) {
		fixture := newRuntimeStreamAuthFixture(t)
		settingsRepo := store.NewPostgresSettingsRepository(fixture.pool)
		setResourcePressureSettings(t, ctx, settingsRepo, targets.FrequencyTier1m, nil)
		reference := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
		times := resourcePressureTimesWithSuppressedBreak(reference.Add(-15*time.Minute), reference, time.Minute, 7)
		latest := seedResourcePressureTimes(t, ctx, fixture.pool, times, func(index int, _ time.Time, _ bool) resourcePressureSampleValues {
			return resourcePressureSampleValues{cpu: 96, memory: 10, load5: 1, marker: new(true), maintenance: index == 7}
		})
		service, _, logs := newResourcePressureService(t, fixture, settingsRepo)
		evaluateResourcePressure(t, ctx, service, latest.Add(100*time.Millisecond), logs)
		state := readResourcePressureState(t, ctx, fixture.pool)
		if state.activeCount != 0 || state.events["incident_started"] != 0 || state.notifications != 0 {
			t.Fatalf("maintenance-break state = %#v, want no active incident or side effects", state)
		}

		// A window made entirely of suppressed rows is also not valid active
		// evidence, even when every raw value is severe.
		fixtureAllSuppressed := newRuntimeStreamAuthFixture(t)
		settingsRepoAllSuppressed := store.NewPostgresSettingsRepository(fixtureAllSuppressed.pool)
		setResourcePressureSettings(t, ctx, settingsRepoAllSuppressed, targets.FrequencyTier1m, nil)
		allSuppressedLatest := seedResourcePressureSeries(t, ctx, fixtureAllSuppressed.pool, reference.Add(-15*time.Minute), reference, time.Minute+time.Millisecond, func(_ int, _ time.Time, _ bool) resourcePressureSampleValues {
			return resourcePressureSampleValues{cpu: 96, memory: 10, load5: 1, marker: new(true), backfilled: true}
		})
		allSuppressedService, _, allSuppressedLogs := newResourcePressureService(t, fixtureAllSuppressed, settingsRepoAllSuppressed)
		evaluateResourcePressure(t, ctx, allSuppressedService, allSuppressedLatest.Add(100*time.Millisecond), allSuppressedLogs)
		allSuppressedState := readResourcePressureState(t, ctx, fixtureAllSuppressed.pool)
		if allSuppressedState.activeCount != 0 || allSuppressedState.events["incident_started"] != 0 || allSuppressedState.notifications != 0 {
			t.Fatalf("all-suppressed state = %#v, want no active incident or side effects", allSuppressedState)
		}
	})

	t.Run("resource SQL predecessor future duplicate and recent isolation", func(t *testing.T) {
		fixture := newRuntimeStreamAuthFixture(t)
		reader := incidents.NewPostgresSnapshotReader(fixture.pool)
		reference := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
		boundary := reference.Add(-30 * time.Minute)
		insertResourcePressureSample(t, ctx, fixture.pool, boundary.Add(-5*time.Second), boundary.Add(-4*time.Second), resourcePressureSampleValues{cpu: 10, memory: 10, load5: 1, marker: new(true)}, "resource-sql-predecessor-a")
		insertResourcePressureSample(t, ctx, fixture.pool, boundary.Add(-5*time.Second), boundary.Add(-3*time.Second), resourcePressureSampleValues{cpu: 10, memory: 10, load5: 1, marker: new(false)}, "resource-sql-predecessor-b")
		insertResourcePressureSample(t, ctx, fixture.pool, boundary.Add(2*time.Minute), boundary.Add(2*time.Minute), resourcePressureSampleValues{cpu: 10, memory: 10, load5: 1, marker: new(true)}, "resource-sql-inside")
		insertResourcePressureSample(t, ctx, fixture.pool, reference, reference, resourcePressureSampleValues{cpu: 10, memory: 10, load5: 1, marker: new(true)}, "resource-sql-anchor")
		insertResourcePressureSample(t, ctx, fixture.pool, reference.Add(time.Minute), reference.Add(time.Minute), resourcePressureSampleValues{cpu: 99, memory: 99, load5: 9, marker: new(true)}, "resource-sql-future")

		window, err := reader.ListResourceWindowHostSamples(ctx, runtimeStreamAuthMonitoringID, reference)
		if err != nil {
			t.Fatalf("ListResourceWindowHostSamples() predecessor query: %v", err)
		}
		assertResourcePressureSampleIDs(t, window, []string{
			"resource-sql-anchor",
			"resource-sql-inside",
			"resource-sql-predecessor-a",
			"resource-sql-predecessor-b",
		})
		if containsResourcePressureSampleID(window, "resource-sql-future") {
			t.Fatal("resource window included a future sample")
		}

		recent, err := reader.ListRecentHostSamples(ctx, runtimeStreamAuthMonitoringID, boundary)
		if err != nil {
			t.Fatalf("ListRecentHostSamples() isolation query: %v", err)
		}
		if containsResourcePressureSampleID(recent, "resource-sql-predecessor-a") || !containsResourcePressureSampleID(recent, "resource-sql-future") {
			t.Fatalf("generic recent samples = %#v, want lower-bound-only range without predecessor and with future row", resourcePressureSampleIDs(recent))
		}

		// An exact boundary row suppresses the predecessor fallback. Duplicate
		// rows at one observed timestamp remain visible as independent evidence.
		insertResourcePressureSample(t, ctx, fixture.pool, boundary, boundary, resourcePressureSampleValues{cpu: 10, memory: 10, load5: 1, marker: new(true)}, "resource-sql-boundary-exact")
		window, err = reader.ListResourceWindowHostSamples(ctx, runtimeStreamAuthMonitoringID, reference)
		if err != nil {
			t.Fatalf("ListResourceWindowHostSamples() exact-boundary query: %v", err)
		}
		assertResourcePressureSampleIDs(t, window, []string{
			"resource-sql-anchor",
			"resource-sql-inside",
			"resource-sql-boundary-exact",
		})
		if containsResourcePressureSampleID(window, "resource-sql-predecessor-a") || containsResourcePressureSampleID(window, "resource-sql-predecessor-b") || containsResourcePressureSampleID(window, "resource-sql-future") {
			t.Fatalf("exact-boundary resource window = %#v, want no predecessor/future", resourcePressureSampleIDs(window))
		}
	})

	t.Run("current tier and label override reevaluate existing facts", func(t *testing.T) {
		fixture := newRuntimeStreamAuthFixture(t)
		if _, err := fixture.pool.Exec(ctx, `update monitoring_instances set labels = $2 where monitoring_instance_id = $1`, runtimeStreamAuthMonitoringID, []string{resourcePressureLabel}); err != nil {
			t.Fatalf("set cadence label: %v", err)
		}
		settingsRepo := store.NewPostgresSettingsRepository(fixture.pool)
		reference := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
		latest := seedResourcePressureSeries(t, ctx, fixture.pool, reference.Add(-30*time.Minute), reference, time.Minute+time.Millisecond, func(_ int, _ time.Time, _ bool) resourcePressureSampleValues {
			return resourcePressureSampleValues{cpu: 96, memory: 10, load5: 1, marker: new(true)}
		})
		service, notifier, logs := newResourcePressureService(t, fixture, settingsRepo)

		// The base tier is 6h and the label has no override, so no bounded
		// resource window can be established and no incident is created.
		setResourcePressureSettings(t, ctx, settingsRepo, targets.FrequencyTier6h, nil)
		evaluateResourcePressure(t, ctx, service, latest.Add(100*time.Millisecond), logs)
		unknown := readResourcePressureState(t, ctx, fixture.pool)
		if unknown.activeCount != 0 || unknown.events["incident_started"] != 0 || unknown.notifications != 0 {
			t.Fatalf("6h base state = %#v, want unknown/no incident", unknown)
		}

		// Adding the label override changes only policy, not facts. The same
		// existing 1m+1ms evidence must now start a critical incident.
		setResourcePressureSettings(t, ctx, settingsRepo, targets.FrequencyTier6h, new(targets.FrequencyTier1m))
		evaluateResourcePressure(t, ctx, service, latest.Add(100*time.Millisecond), logs)
		override := readResourcePressureState(t, ctx, fixture.pool)
		assertResourcePressureActive(t, override, incidents.SeverityCritical, "CPU")
		if override.events["incident_started"] != 1 || override.notifications != 1 || len(notifier.messages) != 1 {
			t.Fatalf("label override state = %#v notifier=%#v, want one started notification", override, notifier.messages)
		}

		// Removing the override and moving the current base tier to 5s makes
		// the same rows too sparse. The existing incident is held, not falsely
		// recovered or escalated, and no new facts are inserted.
		setResourcePressureSettings(t, ctx, settingsRepo, targets.FrequencyTier5s, nil)
		evaluateResourcePressure(t, ctx, service, latest.Add(100*time.Millisecond), logs)
		fiveSecond := readResourcePressureState(t, ctx, fixture.pool)
		if fiveSecond.activeCount != 1 || fiveSecond.events["incident_started"] != 1 || fiveSecond.notifications != 1 || len(notifier.messages) != 1 {
			t.Fatalf("5s re-evaluation state = %#v notifier=%#v, want held active without side effects", fiveSecond, notifier.messages)
		}

		// Restoring the current tier to 1m accepts the same existing rows again.
		setResourcePressureSettings(t, ctx, settingsRepo, targets.FrequencyTier1m, nil)
		evaluateResourcePressure(t, ctx, service, latest.Add(100*time.Millisecond), logs)
		restored := readResourcePressureState(t, ctx, fixture.pool)
		if restored.activeCount != 1 || restored.severity != string(incidents.SeverityCritical) || restored.events["incident_started"] != 1 || restored.notifications != 1 || len(notifier.messages) != 1 {
			t.Fatalf("restored-tier state = %#v notifier=%#v, want same active/idempotent state", restored, notifier.messages)
		}
	})
}

type resourcePressureNotifier struct {
	messages []string
}

func (n *resourcePressureNotifier) Send(_ context.Context, message string) error {
	n.messages = append(n.messages, message)
	return nil
}

type resourcePressureSampleValues struct {
	cpu         float64
	iowait      float64
	steal       float64
	memory      float64
	load5       float64
	marker      any
	backfilled  bool
	maintenance bool
}

func newResourcePressureService(t *testing.T, fixture *runtimeStreamAuthFixture, settingsRepo incidents.SettingsRepository) (*incidents.Service, *resourcePressureNotifier, *bytes.Buffer) {
	t.Helper()
	notifier := &resourcePressureNotifier{}
	var logOutput bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logOutput, nil))
	if settingsRepo == nil {
		return incidents.NewService(
			store.NewPostgresMonitoringInstanceRepository(fixture.pool),
			store.NewPostgresTargetRepository(fixture.pool),
			incidents.NewPostgresSnapshotReader(fixture.pool),
			store.NewPostgresIncidentRepository(fixture.pool),
			notifier,
			logger,
			time.Hour,
			time.Hour,
		), notifier, &logOutput
	}
	return incidents.NewSettingsBackedService(
		store.NewPostgresMonitoringInstanceRepository(fixture.pool),
		store.NewPostgresTargetRepository(fixture.pool),
		incidents.NewPostgresSnapshotReader(fixture.pool),
		store.NewPostgresIncidentRepository(fixture.pool),
		notifier,
		settingsRepo,
		logger,
		time.Hour,
		time.Hour,
	), notifier, &logOutput
}

func evaluateResourcePressure(t *testing.T, ctx context.Context, service *incidents.Service, acceptedAt time.Time, logs *bytes.Buffer) {
	t.Helper()
	if err := service.AfterSuccessfulSync(ctx, syncing.Batch{MonitoringInstanceID: runtimeStreamAuthMonitoringID}, syncing.Result{AcceptedAt: acceptedAt}); err != nil {
		t.Fatalf("AfterSuccessfulSync(%s) error = %v", acceptedAt, err)
	}
	if strings.Contains(logs.String(), "evaluate monitoring instance incidents after sync failed") {
		t.Fatalf("AfterSuccessfulSync logged an evaluation failure: %s", logs.String())
	}
}

func setResourcePressureSettings(t *testing.T, ctx context.Context, repo *store.PostgresSettingsRepository, base string, override *string) {
	t.Helper()
	_, err := repo.MutateSettings(ctx, func(current centersettings.CenterSettings) (centersettings.CenterSettings, error) {
		current.HostSampleFrequencyTier = base
		rules := []centersettings.MonitoringInstanceLabelOverrideRule{}
		if override != nil {
			rules = append(rules, centersettings.MonitoringInstanceLabelOverrideRule{
				Label: resourcePressureLabel,
				Overrides: centersettings.SettingsOverrideFields{
					HostSampleFrequencyTier: override,
				},
			})
		}
		current.OverrideRules = centersettings.OverrideRules{MonitoringInstanceLabels: rules}
		return current, nil
	})
	if err != nil {
		t.Fatalf("MutateSettings(resource pressure tier %q) error = %v", base, err)
	}
}

func seedResourcePressureSeries(t *testing.T, ctx context.Context, pool *pgxpool.Pool, start, end time.Time, step time.Duration, values func(index int, observedAt time.Time, last bool) resourcePressureSampleValues) time.Time {
	t.Helper()
	if !start.Before(end) || step <= 0 {
		t.Fatalf("invalid resource pressure series start=%s end=%s step=%s", start, end, step)
	}
	times := make([]time.Time, 0, int(end.Sub(start)/step)+2)
	for at := start; ; {
		times = append(times, at)
		if at.Equal(end) {
			break
		}
		next := at.Add(step)
		if next.After(end) {
			next = end
		}
		at = next
	}
	return seedResourcePressureTimes(t, ctx, pool, times, values)
}

func seedResourcePressureTimes(t *testing.T, ctx context.Context, pool *pgxpool.Pool, times []time.Time, values func(index int, observedAt time.Time, last bool) resourcePressureSampleValues) time.Time {
	t.Helper()
	if len(times) == 0 {
		t.Fatal("resource pressure sample times are empty")
	}
	for index, observedAt := range times {
		if index > 0 && !times[index-1].Before(observedAt) {
			t.Fatalf("resource pressure times are not strictly increasing at index %d: %s then %s", index, times[index-1], observedAt)
		}
		value := values(index, observedAt, index == len(times)-1)
		insertResourcePressureSample(t, ctx, pool, observedAt, observedAt, value, fmt.Sprintf("resource-pressure-%d-%d", observedAt.UnixNano(), index))
	}
	return times[len(times)-1]
}

func insertResourcePressureSample(t *testing.T, ctx context.Context, pool *pgxpool.Pool, observedAt, receivedAt time.Time, values resourcePressureSampleValues, syncBatchID string) {
	t.Helper()
	insertCPUSamplingHostSample(t, ctx, pool, observedAt, receivedAt, values.cpu, values.iowait, values.steal, values.marker, values.memory, values.load5, values.backfilled, values.maintenance, syncBatchID)
	// insertCPUSamplingHostSample intentionally uses the historical CPU
	// sampling fixture's swap/disk/inode values. Resource-pressure scenarios
	// must make those non-resource metrics safe explicitly.
	if _, err := pool.Exec(ctx, `
		update host_samples
		set mem_available_bytes = $3,
			swap_used_pct = 0,
			disk_used_pct = 40,
			inode_used_pct = 40
		where monitoring_instance_id = $1 and sync_batch_id = $2`, runtimeStreamAuthMonitoringID, syncBatchID, int64(2*1024*1024*1024)); err != nil {
		t.Fatalf("normalize resource-pressure host sample %q: %v", syncBatchID, err)
	}
}

func resourcePressureTimesWithGap(start, end time.Time, regularStep, gap time.Duration) []time.Time {
	times := []time.Time{start}
	at := start
	index := 0
	for at.Before(end) {
		step := regularStep
		if index == 5 {
			step = gap
		}
		next := at.Add(step)
		if next.After(end) {
			next = end
		}
		if next.Equal(at) {
			break
		}
		times = append(times, next)
		at = next
		index++
	}
	return times
}

func resourcePressureTimesWithSuppressedBreak(start, end time.Time, step time.Duration, suppressedIndex int) []time.Time {
	times := []time.Time{start}
	for at := start.Add(step); !at.After(end); at = at.Add(step) {
		times = append(times, at)
	}
	if !times[len(times)-1].Equal(end) {
		times = append(times, end)
	}
	if suppressedIndex < 0 || suppressedIndex >= len(times) {
		panic(fmt.Sprintf("suppressed index %d outside %d resource pressure times", suppressedIndex, len(times)))
	}
	return times
}

type resourcePressurePersistedEvent struct {
	incidentID string
	severity   string
	summary    string
}

type resourcePressurePersistedState struct {
	activeCount             int
	incidentID              string
	severity                string
	summary                 string
	startedAt               time.Time
	lastEvaluatedAt         time.Time
	objectSummary           string
	events                  map[string]int
	lastEvent               map[string]resourcePressurePersistedEvent
	notifications           int
	notificationText        []string
	notificationStatuses    []string
	notificationIncidentIDs []string
}

func readResourcePressureState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) resourcePressurePersistedState {
	t.Helper()
	state := resourcePressurePersistedState{events: make(map[string]int), lastEvent: make(map[string]resourcePressurePersistedEvent)}
	if err := pool.QueryRow(ctx, `select count(*)::int from active_incidents where object_type = 'monitoring_instance' and object_id = $1`, runtimeStreamAuthMonitoringID).Scan(&state.activeCount); err != nil {
		t.Fatalf("count resource-pressure active incidents: %v", err)
	}
	if state.activeCount > 0 {
		if err := pool.QueryRow(ctx, `
			select incident_id, severity, source_summary, started_at, last_evaluated_at
			from active_incidents
			where object_type = 'monitoring_instance' and object_id = $1
			order by case severity when '严重' then 3 when '告警' then 2 when '关注' then 1 else 0 end desc, incident_id
			limit 1`, runtimeStreamAuthMonitoringID).Scan(&state.incidentID, &state.severity, &state.summary, &state.startedAt, &state.lastEvaluatedAt); err != nil {
			t.Fatalf("read resource-pressure active incident: %v", err)
		}
	}
	rows, err := pool.Query(ctx, `
		select payload->>'incident_id', event_type, severity, summary
		from state_change_events
		where object_type = 'monitoring_instance' and object_id = $1
		order by created_at, event_id`, runtimeStreamAuthMonitoringID)
	if err != nil {
		t.Fatalf("read resource-pressure events: %v", err)
	}
	for rows.Next() {
		var incidentID, eventType, severity, summary string
		if err := rows.Scan(&incidentID, &eventType, &severity, &summary); err != nil {
			rows.Close()
			t.Fatalf("scan resource-pressure event: %v", err)
		}
		state.events[eventType]++
		state.lastEvent[eventType] = resourcePressurePersistedEvent{incidentID: incidentID, severity: severity, summary: summary}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatalf("iterate resource-pressure events: %v", err)
	}
	rows.Close()

	rows, err = pool.Query(ctx, `
		select incident_id, delivery_status, summary
		from notification_records
		where object_type = 'monitoring_instance' and object_id = $1
		order by created_at, notification_id`, runtimeStreamAuthMonitoringID)
	if err != nil {
		t.Fatalf("read resource-pressure notifications: %v", err)
	}
	for rows.Next() {
		var incidentID, status, summary string
		if err := rows.Scan(&incidentID, &status, &summary); err != nil {
			rows.Close()
			t.Fatalf("scan resource-pressure notification: %v", err)
		}
		state.notificationIncidentIDs = append(state.notificationIncidentIDs, incidentID)
		state.notificationStatuses = append(state.notificationStatuses, status)
		state.notificationText = append(state.notificationText, summary)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatalf("iterate resource-pressure notifications: %v", err)
	}
	rows.Close()
	state.notifications = len(state.notificationText)

	if err := pool.QueryRow(ctx, `select current_primary_issue_summary from monitoring_instances where monitoring_instance_id = $1`, runtimeStreamAuthMonitoringID).Scan(&state.objectSummary); err != nil {
		t.Fatalf("read resource-pressure object summary: %v", err)
	}
	return state
}

func assertResourcePressureActive(t *testing.T, state resourcePressurePersistedState, severity incidents.Severity, summaryPart string) {
	t.Helper()
	if state.activeCount != 1 || state.severity != string(severity) || !strings.Contains(state.summary, summaryPart) || state.objectSummary != state.summary {
		t.Fatalf("resource-pressure active state = %#v, want one %s incident summary containing %q", state, severity, summaryPart)
	}
	if len(state.notificationText) == 0 || !strings.Contains(state.notificationText[len(state.notificationText)-1], summaryPart) {
		t.Fatalf("resource-pressure notification text = %#v, want summary containing %q", state.notificationText, summaryPart)
	}
}
func assertResourcePressureIowaitPersistence(t *testing.T, state resourcePressurePersistedState, incidentID, summary string, notifier *resourcePressureNotifier) {
	t.Helper()
	if state.activeCount != 1 ||
		state.incidentID != incidentID ||
		state.severity != string(incidents.SeverityCritical) ||
		state.summary != summary ||
		state.events["incident_started"] != 1 ||
		state.events["incident_escalated"] != 0 ||
		state.notifications != 1 ||
		len(notifier.messages) != 1 {
		t.Fatalf("persistent iowait state = %#v notifier=%#v, want unchanged critical incident", state, notifier.messages)
	}
	if event := state.lastEvent["incident_started"]; event.incidentID != incidentID || event.severity != string(incidents.SeverityCritical) || !strings.Contains(event.summary, "iowait") {
		t.Fatalf("persistent iowait started event = %#v, want original critical iowait identity and summary", event)
	}
	if len(state.notificationIncidentIDs) != 1 ||
		state.notificationIncidentIDs[0] != incidentID ||
		len(state.notificationStatuses) != 1 ||
		state.notificationStatuses[0] != string(incidents.DeliveryStatusSent) {
		t.Fatalf("persistent iowait notification identity/status = %#v/%#v, want one sent notification for original incident", state.notificationIncidentIDs, state.notificationStatuses)
	}
}

func resourcePressureSampleIDs(samples []runtimefacts.HostSample) []string {
	ids := make([]string, 0, len(samples))
	for _, sample := range samples {
		ids = append(ids, sample.SyncBatchID)
	}
	return ids
}

func containsResourcePressureSampleID(samples []runtimefacts.HostSample, want string) bool {
	for _, sample := range samples {
		if sample.SyncBatchID == want {
			return true
		}
	}
	return false
}

func assertResourcePressureSampleIDs(t *testing.T, samples []runtimefacts.HostSample, want []string) {
	t.Helper()
	got := resourcePressureSampleIDs(samples)
	gotSet := make(map[string]int, len(got))
	for _, id := range got {
		gotSet[id]++
	}
	wantSet := make(map[string]int, len(want))
	for _, id := range want {
		wantSet[id]++
	}
	if len(gotSet) != len(wantSet) || len(got) != len(want) {
		t.Fatalf("resource window IDs = %#v, want exactly %#v", got, want)
	}
	for id, count := range wantSet {
		if gotSet[id] != count {
			t.Fatalf("resource window IDs = %#v, want exactly %#v", got, want)
		}
	}
}
