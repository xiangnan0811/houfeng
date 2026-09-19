export function formatDateTime(value?: string | null) {
  if (!value) return '尚无'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '尚无'
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

export function formatDate(value?: string | null) {
  if (!value) return '—'
  return value
}

export function formatOptional(value?: string | number | null) {
  if (value == null || value === '') return '—'
  return String(value)
}

export function formatMoney(value: number, currency: string) {
  const amount = Number.isFinite(value) ? value.toFixed(2) : '0.00'
  return `${currency || '---'} ${amount}`
}

export function formatPercent(value?: number | null, digits = 1) {
  if (value == null || Number.isNaN(value)) return '—'
  return `${value.toFixed(digits)}%`
}

export function formatNumber(value?: number | null, digits = 1) {
  if (value == null || Number.isNaN(value)) return '—'
  return value.toFixed(digits)
}

export function formatBytes(value?: number | null) {
  if (value == null || Number.isNaN(value)) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let current = Math.abs(value)
  let unit = 0
  while (current >= 1024 && unit < units.length - 1) {
    current /= 1024
    unit += 1
  }
  const sign = value < 0 ? '-' : ''
  return `${sign}${current.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`
}

export function formatBytesPerSecond(value?: number | null) {
  const formatted = formatBytes(value)
  return formatted === '—' ? formatted : `${formatted}/s`
}

export function formatLatency(value?: number | null) {
  if (value == null || Number.isNaN(value)) return '—'
  return `${value} ms`
}

export function formatUptime(seconds?: number | null) {
  if (seconds == null || Number.isNaN(seconds)) return '—'
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  if (days > 0) return `${days}天 ${hours}小时`
  if (hours > 0) return `${hours}小时 ${minutes}分钟`
  if (minutes > 0) return `${minutes}分钟`
  return '不足 1 分钟'
}

/** Elapsed time from `value` to `now`, without a trailing 前. Invalid input is —. */
export function formatElapsedSince(value?: string | Date | null, now: Date | number = Date.now()): string {
  if (!value) return '—'
  const start = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(start.getTime())) return '—'
  const ref = now instanceof Date ? now : new Date(now)
  const sec = Math.max(0, Math.round((ref.getTime() - start.getTime()) / 1000))
  if (sec < 60) return '不足 1 分钟'
  const min = Math.round(sec / 60)
  if (min < 60) return `${min} 分钟`
  const hr = Math.round(min / 60)
  if (hr < 24) return `${hr} 小时`
  const day = Math.round(hr / 24)
  return `${day} 天`
}

export function formatLabelList(values?: string[] | null) {
  if (!values || values.length === 0) return '—'
  return values.join(' · ')
}

export function formatConfigSummary(config: Record<string, unknown>) {
  const entries = Object.entries(config)
  if (entries.length === 0) return '默认配置'
  return entries
    .map(([key, value]) =>
      `${key}: ${Array.isArray(value) ? value.join('~') : String(value)}`,
    )
    .join(' · ')
}
