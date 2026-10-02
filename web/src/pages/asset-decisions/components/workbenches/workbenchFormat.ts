import type { VPSAssetRecord } from '../../../../lib/types'

function pad2(value: number): string {
  return String(value).padStart(2, '0')
}

/** 时间戳按本地日历日显示（YYYY-MM-DD），扫描行只需要日期。 */
export function localDay(iso?: string | null): string {
  if (!iso) return '—'
  const parsed = Date.parse(iso)
  if (Number.isNaN(parsed)) return '—'
  const date = new Date(parsed)
  return `${date.getFullYear()}-${pad2(date.getMonth() + 1)}-${pad2(date.getDate())}`
}

/** 位置去重：国家 / 地区 / 城市相同的层级只写一次，避免 "JP · Tokyo · Tokyo"。 */
export function compactLocation(vps: Pick<VPSAssetRecord, 'country' | 'region' | 'city'>): string {
  const parts: string[] = []
  for (const value of [vps.country, vps.region, vps.city]) {
    const text = value?.trim()
    if (text && !parts.some((part) => part.toLowerCase() === text.toLowerCase())) parts.push(text)
  }
  return parts.join(' · ') || '位置缺失'
}
