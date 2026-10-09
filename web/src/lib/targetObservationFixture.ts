import type { TargetObservationFreshness, TargetObservationFreshnessState } from './types'

// 5m tier × 3, floored at 60s, plus a 10s probe timeout. Sample fixtures do not
// recompute the server threshold from historical timestamps.
const SAMPLE_STALE_AFTER_SECONDS = 910

const INACTIVE_RUN_STATUSES = new Set(['暂停', '维护中', '已归档'])

export type TargetObservationFixtureInput = {
  target_id: string
  run_status: string
  lifecycle_status?: string
  evaluated_at: string
  enabled_probe_count?: number
  last_success_at?: string | undefined
  last_failure_at?: string | undefined
  probe_item_id?: string
}

function plusSeconds(iso: string, seconds: number): string {
  return new Date(Date.parse(iso) + seconds * 1000).toISOString()
}

function laterTimestamp(left: string, right: string): string {
  return Date.parse(left) >= Date.parse(right) ? left : right
}

function laterObservation(success?: string, failure?: string): string | null {
  if (success && failure) return laterTimestamp(success, failure)
  return success ?? failure ?? null
}

function sampleState(input: TargetObservationFixtureInput, enabledProbeCount: number): TargetObservationFreshnessState {
  if (input.lifecycle_status === 'retired' || INACTIVE_RUN_STATUSES.has(input.run_status)) return 'inactive'
  if (enabledProbeCount === 0) return 'uncovered'
  if (!laterObservation(input.last_success_at, input.last_failure_at)) return 'unobserved'
  return 'fresh'
}

/**
 * Explicit sample projection. Paused, maintenance, archived, and retired targets
 * are inactive. Enabled targets with no success/failure history are unobserved.
 * Enabled targets that already have history are fresh. These fixtures do not
 * invent stale or partial probes from old timestamps.
 */
export function targetObservationFixture(input: TargetObservationFixtureInput): TargetObservationFreshness {
  const enabledProbeCount = input.enabled_probe_count ?? 1
  const state = sampleState(input, enabledProbeCount)
  if (state === 'inactive' || state === 'uncovered') {
    return {
      state,
      evaluated_at: input.evaluated_at,
      enabled_probe_count: enabledProbeCount,
      fresh_probe_count: 0,
      pending_probe_count: 0,
      stale_probe_count: 0,
      probes: [],
    }
  }
  const lastObservedAt = state === 'fresh'
    ? laterObservation(input.last_success_at, input.last_failure_at)
    : null
  const anchor = lastObservedAt ?? input.evaluated_at
  const probeState = state === 'fresh' ? 'fresh' : 'pending'
  return {
    state,
    evaluated_at: input.evaluated_at,
    enabled_probe_count: enabledProbeCount,
    fresh_probe_count: probeState === 'fresh' ? enabledProbeCount : 0,
    pending_probe_count: probeState === 'pending' ? enabledProbeCount : 0,
    stale_probe_count: 0,
    probes: Array.from({ length: enabledProbeCount }, (_, index) => ({
      probe_item_id: input.probe_item_id ?? `${input.target_id}_probe_${index + 1}`,
      state: probeState,
      effective_frequency_tier: '5m' as const,
      stale_after_seconds: SAMPLE_STALE_AFTER_SECONDS,
      last_observed_at: lastObservedAt,
      expected_since: anchor,
      deadline_at: plusSeconds(laterTimestamp(input.evaluated_at, anchor), SAMPLE_STALE_AFTER_SECONDS),
    })),
  }
}
