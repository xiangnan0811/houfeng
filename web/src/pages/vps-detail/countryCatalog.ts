import {
  COMMON_COUNTRY_GROUPS,
  ISO3166_1_COUNTRIES,
  type CommonCountryGroup,
} from './iso3166Countries'

const COMMON_ZH: Record<string, string> = {
  US: '美国',
  HK: '香港',
  SG: '新加坡',
  JP: '日本',
  TW: '台湾',
  KR: '韩国',
  GB: '英国',
  DE: '德国',
  FR: '法国',
  NL: '荷兰',
  CA: '加拿大',
  AU: '澳大利亚',
  CN: '中国大陆',
  MY: '马来西亚',
  TH: '泰国',
  IN: '印度',
  BR: '巴西',
}

export type CountryListItem = {
  value: string
  label: string
  search: string
  hint: string
  zh: string
  en: string
  group: string
  custom?: boolean
  commitLabel?: string
}

function displayNames(locale: string) {
  try {
    if (typeof Intl !== 'undefined' && typeof Intl.DisplayNames === 'function') {
      return new Intl.DisplayNames([locale], { type: 'region' })
    }
  } catch {
    /* fall back to iso-codes names */
  }
  return null
}

function buildCountryItems() {
  if (!Array.isArray(ISO3166_1_COUNTRIES) || ISO3166_1_COUNTRIES.length !== 249) {
    throw new Error('ISO 3166-1 country list is missing or incomplete')
  }
  if (!Array.isArray(COMMON_COUNTRY_GROUPS) || COMMON_COUNTRY_GROUPS.length === 0) {
    throw new Error('COMMON_COUNTRY_GROUPS is missing')
  }

  const zhDN = displayNames('zh-CN')
  const enDN = displayNames('en')
  const items: CountryListItem[] = ISO3166_1_COUNTRIES.map((row) => {
    let zh = row.zhOfficial
    let en = row.commonName || row.name
    try {
      const z = zhDN?.of(row.code)
      if (z) zh = z
      const e = enDN?.of(row.code)
      if (e) en = e
    } catch {
      /* keep iso-codes names */
    }
    const shortZh = COMMON_ZH[row.code] || zh
    const search = [
      row.code,
      row.alpha3,
      row.numeric,
      row.name,
      row.officialName,
      row.commonName,
      row.zhOfficial,
      zh,
      en,
      shortZh,
    ]
      .filter(Boolean)
      .join(' ')
      .toLowerCase()
    return {
      value: row.code,
      label: shortZh,
      search,
      hint: row.code,
      zh: shortZh,
      en,
      group: 'other',
    }
  })

  const byCode = new Map(items.map((item) => [item.value, item]))
  const commonCodes = new Set<string>()
  for (const group of COMMON_COUNTRY_GROUPS) {
    for (const code of group.codes) {
      const item = byCode.get(code)
      if (!item) throw new Error(`common country code missing: ${code}`)
      item.group = group.id
      commonCodes.add(code)
    }
  }

  return { items, groups: COMMON_COUNTRY_GROUPS, commonCodes }
}

const COUNTRY = buildCountryItems()

export const COUNTRY_ITEMS = COUNTRY.items
export const COUNTRY_GROUPS: readonly CommonCountryGroup[] = COUNTRY.groups
export const COMMON_COUNTRY_CODES = COUNTRY.commonCodes
export const OTHER_COUNTRY_COUNT = COUNTRY_ITEMS.length - COMMON_COUNTRY_CODES.size

export function countryCommitted(value: string): { value: string; label: string } {
  if (!value) return { value: '', label: '' }
  const found = COUNTRY_ITEMS.find((item) => (
    item.value === value
    || item.label === value
    || item.zh === value
  ))
  if (found) return { value: found.value, label: found.zh || found.label }
  return { value, label: value }
}

export function matchesCountryQuery(item: CountryListItem, raw: string) {
  const q = raw.trim().toLowerCase()
  if (!q) return false
  if (item.value.toLowerCase() === q) return true
  if (item.hint && item.hint.toLowerCase() === q) return true
  if (item.zh.includes(raw.trim()) || item.label.includes(raw.trim())) return true
  const en = (item.en || '').toLowerCase()
  if (en === q) return true
  if (q.length <= 2) {
    return en.split(/[\s'-]+/).some((word) => word.startsWith(q))
  }
  return en.includes(q) || item.search.includes(q)
}

export function matchCountryExact(raw: string) {
  const q = raw.trim().toLowerCase()
  if (!q) return null
  return (
    COUNTRY_ITEMS.find((item) => item.value.toLowerCase() === q)
    || COUNTRY_ITEMS.find((item) => item.label.toLowerCase() === q)
    || COUNTRY_ITEMS.find((item) => item.zh === raw.trim())
    || COUNTRY_ITEMS.find((item) => item.en?.toLowerCase() === q)
    || COUNTRY_ITEMS.find((item) => item.hint && item.hint.toLowerCase() === q)
    || null
  )
}

export function resolveCountryInput(raw: string): { value: string; label: string } {
  const trimmed = raw.trim()
  if (!trimmed) return { value: '', label: '' }
  const exact = matchCountryExact(trimmed)
  if (exact) return { value: exact.value, label: exact.zh || exact.label }
  return { value: trimmed, label: trimmed }
}

export function browseCountryItems(othersOpen: boolean) {
  const byCode = new Map(COUNTRY_ITEMS.map((item) => [item.value, item]))
  const ordered: CountryListItem[] = []
  for (const group of COUNTRY_GROUPS) {
    for (const code of group.codes) {
      const item = byCode.get(code)
      if (item) ordered.push(item)
    }
  }
  if (othersOpen) {
    for (const item of COUNTRY_ITEMS) {
      if (!COMMON_COUNTRY_CODES.has(item.value)) ordered.push(item)
    }
  }
  return ordered
}

export function searchCountryItems(raw: string): CountryListItem[] {
  const q = raw.trim().toLowerCase()
  if (!q) return browseCountryItems(false)
  const filtered = COUNTRY_ITEMS.filter((item) => matchesCountryQuery(item, raw))
  filtered.sort((a, b) => {
    const rank = (item: CountryListItem) => {
      if (item.value.toLowerCase() === q) return 0
      if (item.zh === raw.trim() || item.label === raw.trim()) return 1
      return 2
    }
    return rank(a) - rank(b)
  })
  const hasExact = COUNTRY_ITEMS.some((item) => (
    item.label === raw.trim()
    || item.value.toLowerCase() === q
    || item.zh === raw.trim()
    || item.en?.toLowerCase() === q
  ))
  if (raw.trim() && !hasExact) {
    filtered.push({
      value: raw.trim(),
      label: `使用「${raw.trim()}」`,
      search: q,
      hint: '自定义',
      custom: true,
      commitLabel: raw.trim(),
      zh: raw.trim(),
      en: '',
      group: 'other',
    })
  }
  return filtered
}
