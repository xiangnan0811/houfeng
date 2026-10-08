import { useEffect, useEffectEvent, useId, useState } from 'react'

import { Button, Select } from '../../components/atoms'
import { PageState } from '../../components/PageState'
import { listMonitoringInstances, listTargets, listVPSAssets } from '../../lib/api'
import { ApiError } from '../../lib/apiRequest'
import type {
  MonitoringInstanceRecord,
  RecordSubjectKind,
  TargetRecord,
  VPSAssetRecord,
} from '../../lib/types'
import { labelOptions, RECORD_SUBJECT_KIND_LABELS } from './recordLabels'

export type RecordImportCatalog = {
  kind: RecordSubjectKind
  ids: readonly string[]
}

type CatalogSubject = {
  id: string
  label: string
  detail: string
}

type CatalogStatus = 'idle' | 'loading' | 'ready' | 'empty' | 'unreadable' | 'error'

type CatalogPage = {
  kind: RecordSubjectKind
  attempt: number
  status: Exclude<CatalogStatus, 'idle' | 'loading'>
  message: string | null
  items: CatalogSubject[]
}

type RecordImportDestinationPickerProps = {
  disabled: boolean
  kind: RecordSubjectKind | ''
  subjectId: string
  onKindChange: (kind: RecordSubjectKind | '') => void
  onSubjectChange: (subject: { id: string; label: string } | null) => void
  onCatalogChange: (catalog: RecordImportCatalog | null) => void
}

function named(value: string | undefined, fallback: string): string {
  const trimmed = value?.trim() ?? ''
  return trimmed || fallback
}

function vpsSubject(record: VPSAssetRecord): CatalogSubject {
  return {
    id: record.vps_id,
    label: named(record.display_name, '未命名 VPS'),
    detail: [record.provider_name, record.region].filter(Boolean).join(' · '),
  }
}

function instanceSubject(record: MonitoringInstanceRecord): CatalogSubject {
  return {
    id: record.monitoring_instance_id,
    label: named(record.display_name, '未命名监控实例'),
    detail: [record.provider, record.region].filter(Boolean).join(' · '),
  }
}

function targetSubject(record: TargetRecord): CatalogSubject {
  return {
    id: record.target_id,
    label: named(record.name, '未命名探测目标'),
    detail: named(record.host, ''),
  }
}

function emptyCatalogMessage(kind: RecordSubjectKind): string {
  if (kind === 'vps') return '暂无 VPS。'
  if (kind === 'monitoring_instance') return '暂无监控实例。'
  return '暂无探测目标。'
}

async function loadSubjects(kind: RecordSubjectKind): Promise<CatalogSubject[]> {
  if (kind === 'vps') return (await listVPSAssets()).map(vpsSubject)
  if (kind === 'monitoring_instance') return (await listMonitoringInstances()).map(instanceSubject)
  return (await listTargets()).map(targetSubject)
}

function describeCatalogFailure(error: unknown): { status: 'unreadable' | 'error'; message: string } {
  if (error instanceof ApiError && (error.status === 404 || error.status === 403)) {
    return { status: 'unreadable', message: '这些资产无法读取。' }
  }
  return { status: 'error', message: '无法读取资产，请重试' }
}

function optionLabel(item: CatalogSubject, items: readonly CatalogSubject[]): string {
  const duplicated = items.some((other) => other.id !== item.id && other.label === item.label)
  return duplicated ? `${item.label}（${item.id}）` : item.label
}

function parseKind(value: string): RecordSubjectKind | '' {
  if (value === 'vps' || value === 'monitoring_instance' || value === 'target') return value
  return ''
}

export function RecordImportDestinationPicker({
  disabled,
  kind,
  subjectId,
  onKindChange,
  onSubjectChange,
  onCatalogChange,
}: RecordImportDestinationPickerProps) {
  const noticeId = useId()
  const [attempt, setAttempt] = useState(0)
  const [page, setPage] = useState<CatalogPage | null>(null)
  const visible = page && page.kind === kind && page.attempt === attempt ? page : null
  const status: CatalogStatus = !kind ? 'idle' : visible ? visible.status : 'loading'
  const items = visible?.items ?? []
  const selected = items.find((item) => item.id === subjectId) ?? null

  // Kind and retry each start a new catalog generation. A late response is
  // dropped unless it still matches both, so it cannot overwrite the new list.
  useEffect(() => {
    if (!kind) return undefined
    const requestedKind = kind
    const requestedAttempt = attempt
    let active = true
    void loadSubjects(requestedKind)
      .then((next) => {
        if (!active) return
        setPage({
          kind: requestedKind,
          attempt: requestedAttempt,
          status: next.length === 0 ? 'empty' : 'ready',
          message: next.length === 0 ? emptyCatalogMessage(requestedKind) : null,
          items: next,
        })
      })
      .catch((error: unknown) => {
        if (!active) return
        const failure = describeCatalogFailure(error)
        setPage({
          kind: requestedKind,
          attempt: requestedAttempt,
          status: failure.status,
          message: failure.message,
          items: [],
        })
      })
    return () => {
      active = false
    }
  }, [kind, attempt])

  const reportCatalog = useEffectEvent(onCatalogChange)

  useEffect(() => {
    reportCatalog(visible?.status === 'ready' && kind !== ''
      ? { kind, ids: visible.items.map((item) => item.id) }
      : null)
  }, [visible, kind])

  return (
    <div className="record-import-destination">
      <p id={noticeId} className="record-import-destination__notice">归档内所有记录将关联此主体。</p>
      <div className="record-tool__fields">
        <Select
          label="主体类型"
          aria-describedby={noticeId}
          value={kind}
          disabled={disabled}
          onChange={(event) => onKindChange(parseKind(event.target.value))}
        >
          <option value="">请选择主体类型</option>
          {labelOptions(RECORD_SUBJECT_KIND_LABELS).map((option) => (
            <option key={option.value} value={option.value}>{option.label}</option>
          ))}
        </Select>
        {status === 'ready' ? (
          <Select
            label="目标主体"
            aria-describedby={noticeId}
            value={selected ? selected.id : ''}
            disabled={disabled}
            onChange={(event) => {
              const id = event.target.value
              if (!id) {
                onSubjectChange(null)
                return
              }
              const match = items.find((item) => item.id === id)
              if (!match) return
              onSubjectChange({ id: match.id, label: match.label })
            }}
          >
            <option value="">请选择对象</option>
            {items.map((item) => (
              <option key={item.id} value={item.id}>{optionLabel(item, items)}</option>
            ))}
          </Select>
        ) : null}
      </div>
      {status === 'idle' ? <p className="record-tool__message">选择主体类型后列出可关联的对象。</p> : null}
      {status === 'loading' ? <PageState compact kind="loading" title="正在读取资产" /> : null}
      {status === 'empty' ? <PageState compact kind="empty" title={visible?.message ?? '暂无资产'} /> : null}
      {status === 'unreadable' ? (
        <PageState
          compact
          kind="error"
          title="资产无法读取"
          description={visible?.message ?? '这些资产无法读取。'}
          action={<Button size="sm" variant="secondary" onClick={() => setAttempt((value) => value + 1)} disabled={disabled}>重试</Button>}
        />
      ) : null}
      {status === 'error' ? (
        <PageState
          compact
          kind="error"
          title="无法读取资产"
          description={visible?.message ?? undefined}
          action={<Button size="sm" variant="secondary" onClick={() => setAttempt((value) => value + 1)} disabled={disabled}>重试</Button>}
        />
      ) : null}
      {selected ? (
        <p className="record-import-destination__id">
          {selected.detail ? `${selected.detail} · ` : null}
          技术标识 <code>{selected.id}</code>
        </p>
      ) : null}
    </div>
  )
}
