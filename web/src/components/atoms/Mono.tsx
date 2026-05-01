import type { ReactNode } from 'react'

/* ========== MonoDigits ========== */

export interface MonoDigitsProps {
  children: ReactNode
  className?: string
}

export function MonoDigits({ children, className = '' }: MonoDigitsProps) {
  const cls = ['mono', 'tnum', 'mono-digits', className].filter(Boolean).join(' ')
  return <span className={cls}>{children}</span>
}

/* ========== Hostname ========== */

export interface HostnameProps {
  children: string
  truncate?: boolean
  /** Maximum visible chars before ellipsis. Only applies when truncate. */
  maxChars?: number
  className?: string
  title?: string
}

export function Hostname({
  children,
  truncate = false,
  maxChars,
  className = '',
  title,
}: HostnameProps) {
  const display =
    truncate && maxChars && children.length > maxChars
      ? `${children.slice(0, maxChars)}…`
      : children
  const cls = ['mono', 'hostname', truncate && 'hostname--truncate', className]
    .filter(Boolean)
    .join(' ')
  return (
    <span className={cls} title={title ?? children}>
      {display}
    </span>
  )
}

/* ========== Timestamp ========== */

export type TimestampMode = 'absolute' | 'relative' | 'both'

export interface TimestampProps {
  /** ISO string, Date, or null. Null renders an em-dash placeholder. */
  value: string | Date | null | undefined
  mode?: TimestampMode
  className?: string
  /** Reference time for "relative" mode. Defaults to Date.now(). */
  now?: Date | number
}

function pad2(n: number): string {
  return n < 10 ? `0${n}` : `${n}`
}

function formatAbsolute(d: Date): string {
  const y = d.getFullYear()
  const m = pad2(d.getMonth() + 1)
  const day = pad2(d.getDate())
  const h = pad2(d.getHours())
  const mi = pad2(d.getMinutes())
  return `${y}/${m}/${day} ${h}:${mi}`
}

function formatRelative(target: Date, ref: Date): string {
  const diffMs = ref.getTime() - target.getTime()
  const sec = Math.round(diffMs / 1000)
  if (sec < 5) return '刚刚'
  if (sec < 60) return `${sec} 秒前`
  const min = Math.round(sec / 60)
  if (min < 60) return `${min} 分钟前`
  const hr = Math.round(min / 60)
  if (hr < 24) return `${hr} 小时前`
  const day = Math.round(hr / 24)
  if (day < 30) return `${day} 天前`
  return formatAbsolute(target)
}

export function Timestamp({ value, mode = 'absolute', className = '', now }: TimestampProps) {
  const cls = ['mono', 'tnum', 'timestamp', `timestamp--${mode}`, className]
    .filter(Boolean)
    .join(' ')
  if (!value) return <span className={cls}>—</span>
  const d = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(d.getTime())) return <span className={cls}>—</span>
  const ref = now == null ? new Date() : now instanceof Date ? now : new Date(now)
  const absolute = formatAbsolute(d)
  const relative = formatRelative(d, ref)
  if (mode === 'absolute') {
    return (
      <span className={cls} title={relative}>
        {absolute}
      </span>
    )
  }
  if (mode === 'relative') {
    return (
      <span className={cls} title={absolute}>
        {relative}
      </span>
    )
  }
  return (
    <span className={cls} title={absolute}>
      {relative} · <span className="timestamp__abs">{absolute}</span>
    </span>
  )
}
