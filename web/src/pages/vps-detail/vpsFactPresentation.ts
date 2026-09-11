import type { VPSOverviewFact as ModernFact, VPSOverviewIdentity } from '../../lib/types'
import { overviewImportanceLabel } from '../../lib/vpsOverviewPresentation'
import type { VPSOverviewFact as LegacyFact } from './vpsDetailOverviewModel'

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

export function legacyOverviewFactRows(facts: LegacyFact[]): VPSFactDisplay[] {
  return facts.map((fact, index) => ({
    key: `${fact.domain}:${fact.label}:${index}`,
    label: presentationFactLabel('', fact.label),
    value: presentationFactValue('', fact.label, fact.value),
    ...(fact.meta ? { meta: fact.meta } : {}),
    copyValue: copyableValue('', fact.value, fact.copyValue ?? null),
    ...(fact.tone ? { tone: fact.tone } : {}),
    layout: factLayoutFor(fact),
  }))
}
