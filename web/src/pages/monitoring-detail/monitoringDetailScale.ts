/**
 * Data-scaled axis helpers for the monitoring detail observation charts.
 *
 * Kept beside the observation component (and outside `MetricChart`) so the shared
 * chart atom keeps its own automatic padding and scale rules untouched.
 */

export type ObservationLayout = 'wide' | 'medium' | 'narrow'

/** Equal 4×2 tile plot height. Do not scale with the viewport. */
export const OBSERVATION_CHART_HEIGHT = 200

/**
 * Plot SVG height for one equal cell.
 *
 * Eight tiles share one plot height so the two rows match. 200px is a step
 * up from the old 160/168 band without turning plates into posters.
 */
export function observationChartHeight(_args?: {
  layout?: ObservationLayout
  gridWidth?: number
  gridTop?: number
  viewportHeight?: number
}): number {
  return OBSERVATION_CHART_HEIGHT
}

/**
 * Round a raw upper bound up to the 1–1.5–2–2.5–3–4–5–6–8 ladder.
 *
 * There is deliberately no `max(raw, 1)` floor: I/O wait sits around 0.5%, and a
 * floor of 1 stretches its axis to 0–1% and turns a constant series into a dead
 * line floating mid-plot. The axis follows the data instead.
 */
export function niceMax(raw: number): number {
  const target = Math.max(raw, 0)
  if (!(target > 0)) return 0.1
  const exp = Math.floor(Math.log10(target))
  const frac = target / 10 ** exp
  const niceFrac =
    frac <= 1 ? 1
    : frac <= 1.5 ? 1.5
    : frac <= 2 ? 2
    : frac <= 2.5 ? 2.5
    : frac <= 3 ? 3
    : frac <= 4 ? 4
    : frac <= 5 ? 5
    : frac <= 6 ? 6
    : frac <= 8 ? 8
    : 10
  return niceFrac * 10 ** exp
}
