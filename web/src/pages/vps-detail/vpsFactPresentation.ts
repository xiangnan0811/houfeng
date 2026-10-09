import type { VPSOverviewFact as ModernFact, VPSOverviewIdentity } from '../../lib/types'
import { formatDateTime } from '../../lib/format'
import { overviewImportanceLabel } from '../../lib/vpsOverviewPresentation'
export type VPSFactDisplay = {
  key: string
  label: string
  value: string
  meta?: string
  copyValue?: string | null
  tone?: string
  layout: 'short' | 'full'
}

const COPYABLE_FACT_KEYS: Record<string, true> = { ipv4: true, ipv6: true, ssh: true }
const PRODUCT_LABELS: Record<string, true> = { 产品: true, 规格: true }
const OS_LABELS: Record<string, true> = { 系统: true, 操作系统: true }

export function factLayoutFor(input: { key?: string; label: string }): 'short' | 'full' {
  const key = input.key?.trim() ?? ''
  const label = input.label.trim()
  if (key === 'ssh' || key === 'note' || label === 'SSH' || label === '备注') return 'full'
  return 'short'
}

function copyableValue(key: string, value: string, explicit?: string | null): string | null {
  if (explicit) {
    const raw = explicit.trim()
    return !raw || raw === '—' ? null : raw
  }
  if (!COPYABLE_FACT_KEYS[key]) return null
  const raw = value.trim()
  if (!raw || raw === '—') return null
  return raw
}

function presentationFactLabel(key: string, label: string): string {
  if (key === 'product_name' || PRODUCT_LABELS[label]) return '规格'
  if (key === 'os_name' || OS_LABELS[label]) return '系统'
  return label
}

function presentationFactValue(key: string, label: string, value: string): string {
  if (key === 'importance' || label === '重要性') return overviewImportanceLabel(value)
  return value
}

function hasProductFact(facts: Array<{ key: string; label: string }>): boolean {
  return facts.some((fact) => fact.key === 'product_name' || PRODUCT_LABELS[fact.label])
}

function hasLabeledFact(facts: Array<{ key: string; label: string }>, key: string, label: string): boolean {
  return facts.some((fact) => fact.key === key || fact.label === label)
}

export function modernOverviewFactRows(
  facts: ModernFact[],
  identity?: VPSOverviewIdentity,
): VPSFactDisplay[] {
  const rows: VPSFactDisplay[] = facts.map((fact) => ({
    key: fact.key,
    label: presentationFactLabel(fact.key, fact.label),
    value: presentationFactValue(fact.key, fact.label, fact.value),
    copyValue: copyableValue(fact.key, fact.value),
    layout: factLayoutFor(fact),
  }))

  if (identity) {
    const independentFacts: Array<[string, string, string | undefined]> = [
      ['validity_mode', '资源有效期', identity.validity_mode ? (identity.validity_mode === 'fixed' ? (identity.expires_at || '到期日未知') : identity.validity_mode === 'unlimited' ? '无固定期限' : '未知') : undefined],
      ['auto_renew_check', '服务商自动续费', identity.auto_renew_check ? ({ unchecked: '尚未核对', enabled: '已开启', disabled: '已关闭', never_enabled: '从未开启', unsupported: '不支持' }[identity.auto_renew_check] ?? '尚未核对') : undefined],
      ['auto_renew_checked_at', '自动续费核对时间', identity.auto_renew_checked_at ? formatDateTime(identity.auto_renew_checked_at) : undefined],
      ['renewal_reason', '续费意向原因', identity.renewal_reason || undefined],
      // 复核时间按日期录入，只显示 UTC 日期部分，不暴露原始 ISO 字符串。
      ['renewal_review_at', '续费意向复核时间', identity.renewal_review_at ? identity.renewal_review_at.slice(0, 10) : undefined],
      ['acquisition_source', '获取来源', identity.acquisition_source || undefined],
    ]
    for (const [key, label, value] of independentFacts) {
      if (value && !hasLabeledFact(rows, key, label)) rows.push({ key, label, value, layout: 'short' })
    }
    const productName = identity.product_name.trim()
    if (productName && productName !== '—' && !hasProductFact(rows)) {
      rows.unshift({
        key: 'product_name',
        label: '规格',
        value: productName,
        layout: 'short',
      })
    }

    if (!hasLabeledFact(rows, 'importance', '重要性') && identity.importance.trim()) {
      rows.push({
        key: 'importance',
        label: '重要性',
        value: overviewImportanceLabel(identity.importance),
        layout: 'short',
      })
    }

    if (!hasLabeledFact(rows, 'labels', '标签')) {
      rows.push({
        key: 'labels',
        label: '标签',
        value: identity.labels.length > 0 ? identity.labels.join(' · ') : '无标签',
        layout: 'short',
      })
    }
  }

  return rows
}
