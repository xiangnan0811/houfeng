export interface TrendArrowProps {
  delta: number
  unit?: string
  /** When true, "up" reads as bad and "down" reads as good (e.g. error rate). Default false. */
  inverse?: boolean
  /** Hide the numeric delta, only show the arrow glyph. */
  arrowOnly?: boolean
  /** Threshold for treating as flat. Default 0. */
  flatThreshold?: number
  className?: string
}

type Direction = 'up' | 'down' | 'flat'

function classify(delta: number, threshold: number): Direction {
  if (Math.abs(delta) <= threshold) return 'flat'
  return delta > 0 ? 'up' : 'down'
}

function arrowGlyph(dir: Direction): string {
  if (dir === 'up') return '↗'
  if (dir === 'down') return '↘'
  return '→'
}

function semanticTone(dir: Direction, inverse: boolean): 'normal' | 'critical' | 'muted' {
  if (dir === 'flat') return 'muted'
  const goodWhenUp = !inverse
  const isGood = (dir === 'up' && goodWhenUp) || (dir === 'down' && !goodWhenUp)
  return isGood ? 'normal' : 'critical'
}

function formatDelta(delta: number, unit?: string): string {
  const abs = Math.abs(delta)
  const numericPart =
    abs >= 100 ? abs.toFixed(0) : abs >= 10 ? abs.toFixed(1) : abs.toFixed(2)
  const sign = delta > 0 ? '+' : delta < 0 ? '-' : ''
  return unit ? `${sign}${numericPart}${unit}` : `${sign}${numericPart}`
}

export function TrendArrow({
  delta,
  unit,
  inverse = false,
  arrowOnly = false,
  flatThreshold = 0,
  className = '',
}: TrendArrowProps) {
  const dir = classify(delta, flatThreshold)
  const tone = semanticTone(dir, inverse)
  const cls = ['trend', `trend--${dir}`, `trend--tone-${tone}`, className]
    .filter(Boolean)
    .join(' ')
  const label =
    dir === 'flat' ? '持平' : dir === 'up' ? `上升 ${formatDelta(delta, unit)}` : `下降 ${formatDelta(delta, unit)}`
  return (
    <span className={cls} aria-label={label}>
      <span className="trend__arrow" aria-hidden>
        {arrowGlyph(dir)}
      </span>
      {!arrowOnly && (
        <span className="trend__delta" aria-hidden>
          {dir === 'flat' ? '持平' : formatDelta(delta, unit)}
        </span>
      )}
    </span>
  )
}
