import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'

import { Button, Input } from '../../components/atoms'
import { ApiError } from '../../lib/apiRequest'
import { applyRecordImport, dryRunRecordImport } from '../../lib/recordsApi'
import type {
  RecordImportDestinationSubject,
  RecordImportPlan,
  RecordSubjectKind,
} from '../../lib/types'
import {
  RecordImportDestinationPicker,
  type RecordImportCatalog,
} from './RecordImportDestinationPicker'
import { RECORD_SUBJECT_KIND_LABELS } from './recordLabels'

function isAbortError(error: unknown): boolean {
  return error instanceof Error && error.name === 'AbortError'
}

function describeImportFailure(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.code === 'origin_tombstoned') return '该来源已墓碑化，不能官方恢复或再导入。'
    if (error.code === 'import_origin_conflict') return '该归档已导入过，不能再次官方导入。'
    if (error.code === 'import_cas_conflict') return '导入计划已变化，请重新预检。'
    if (error.status === 404 || error.code === 'resource_not_found') return '无权访问或主体不可用。'
    if (error.status === 503) return '导入服务暂不可用，请重试。'
    return error.message
  }
  return '导入失败，请重试。'
}

function recordDetailHref(recordId: string): string {
  return `/records/${encodeURIComponent(recordId)}`
}

function confirmedSubject(
  plan: RecordImportPlan,
  kind: RecordSubjectKind | '',
  subjectId: string,
  label: string,
): string {
  const echoed = plan.destination_subject
  const kindLabel = RECORD_SUBJECT_KIND_LABELS[echoed.subject_kind]
  const matched = kind === echoed.subject_kind && subjectId === echoed.subject_id && label.trim() !== ''
  const name = matched ? label.trim() : kindLabel
  return `服务器确认：${name}（${kindLabel} ${echoed.subject_id}）`
}

export function RecordImportPanel() {
  const [file, setFile] = useState<File | null>(null)
  const [kind, setKind] = useState<RecordSubjectKind | ''>('')
  const [subjectId, setSubjectId] = useState('')
  const [subjectLabel, setSubjectLabel] = useState('')
  const [catalog, setCatalog] = useState<RecordImportCatalog | null>(null)
  const [plan, setPlan] = useState<RecordImportPlan | null>(null)
  const [appliedIds, setAppliedIds] = useState<string[] | null>(null)
  const [needsNewPreview, setNeedsNewPreview] = useState(false)
  const [busy, setBusy] = useState(false)
  const [progress, setProgress] = useState('')
  const [message, setMessage] = useState('')
  const keyRef = useRef(crypto.randomUUID())
  const mustRotateRef = useRef(false)
  const generationRef = useRef(0)
  const abortRef = useRef<AbortController | null>(null)

  function releaseOutcome() {
    setPlan(null)
    setProgress('')
    setMessage('')
    setAppliedIds(null)
    setNeedsNewPreview(false)
  }

  function rotateKey() {
    keyRef.current = crypto.randomUUID()
    mustRotateRef.current = false
  }

  function invalidateInFlight() {
    generationRef.current += 1
    abortRef.current?.abort()
    abortRef.current = null
    setBusy(false)
  }

  function discard(rotate: boolean) {
    invalidateInFlight()
    releaseOutcome()
    if (rotate) rotateKey()
  }

  // A ready catalog that no longer lists the current id is what drops it.
  // Clearing in that callback retires the plan without a follow-up render.
  function onCatalogChange(next: RecordImportCatalog | null) {
    setCatalog(next)
    if (!next || next.kind !== kind || subjectId === '' || next.ids.includes(subjectId)) return
    setSubjectId('')
    setSubjectLabel('')
    discard(true)
  }

  useEffect(() => () => {
    generationRef.current += 1
    abortRef.current?.abort()
  }, [])

  const destination: RecordImportDestinationSubject | null = kind && subjectId
    ? { subject_kind: kind, subject_id: subjectId }
    : null
  const canPreview = Boolean(
    file
    && destination
    && catalog?.kind === kind
    && catalog.ids.includes(subjectId),
  )
  const messageIsFailure = message !== '' && !message.includes('隔离')

  function takeKey(forceNew: boolean): string {
    if (forceNew || mustRotateRef.current) rotateKey()
    return keyRef.current
  }

  function runDryRun(forceNewKey: boolean) {
    if (busy || !canPreview || !file || !destination || appliedIds) return
    const generation = generationRef.current + 1
    generationRef.current = generation
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    const key = takeKey(forceNewKey)
    setBusy(true)
    setNeedsNewPreview(false)
    setMessage('')
    setProgress('正在预检归档…')
    setPlan(null)
    setAppliedIds(null)
    void dryRunRecordImport(file, destination, key, controller.signal)
      .then((next) => {
        if (generation !== generationRef.current) return
        setPlan(next)
        setProgress('预检完成')
        if (next.quarantine.length > 0) {
          setMessage(`有 ${next.quarantine.length} 项证据已隔离，仅展示信封，不会当作可信证据。`)
        }
      })
      .catch((error: unknown) => {
        if (generation !== generationRef.current || isAbortError(error)) return
        setPlan(null)
        setProgress('')
        if (error instanceof ApiError && error.code === 'import_cas_conflict') {
          mustRotateRef.current = true
          setNeedsNewPreview(true)
        }
        setMessage(describeImportFailure(error))
      })
      .finally(() => {
        if (generation !== generationRef.current) return
        setBusy(false)
      })
  }

  function runApply() {
    if (busy || !plan || appliedIds) return
    const generation = generationRef.current + 1
    generationRef.current = generation
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    const planId = plan.plan_id
    const lockVersion = plan.lock_version
    setBusy(true)
    setMessage('')
    setProgress('正在应用导入计划…')
    void applyRecordImport(planId, lockVersion, controller.signal)
      .then((result) => {
        if (generation !== generationRef.current) return
        setAppliedIds(result.record_ids)
        setProgress('')
      })
      .catch((error: unknown) => {
        if (generation !== generationRef.current || isAbortError(error)) return
        setProgress('')
        const opaque = error instanceof ApiError
          && (error.code === 'resource_not_found' || error.status === 404)
        const replaced = error instanceof ApiError && error.code === 'import_cas_conflict'
        if (opaque || replaced) {
          mustRotateRef.current = true
          setNeedsNewPreview(true)
          setPlan(null)
        }
        setMessage(describeImportFailure(error))
      })
      .finally(() => {
        if (generation !== generationRef.current) return
        setBusy(false)
      })
  }

  return (
    <section className="record-tool" aria-label="记录导入" aria-busy={busy}>
      <RecordImportDestinationPicker
        disabled={busy}
        kind={kind}
        subjectId={subjectId}
        onKindChange={(next) => {
          if (next === kind) return
          setKind(next)
          setSubjectId('')
          setSubjectLabel('')
          discard(true)
        }}
        onSubjectChange={(subject) => {
          const nextId = subject?.id ?? ''
          if (nextId === subjectId) return
          setSubjectId(nextId)
          setSubjectLabel(subject?.label ?? '')
          discard(true)
        }}
        onCatalogChange={onCatalogChange}
      />
      <Input
        label="归档文件"
        type="file"
        accept="application/zip,.zip"
        disabled={busy}
        onChange={(event) => {
          setFile(event.target.files?.[0] ?? null)
          discard(true)
        }}
      />
      <div className="record-tool__actions">
        <Button
          size="lg"
          variant="secondary"
          disabled={busy || !canPreview || appliedIds !== null || plan !== null}
          onClick={() => runDryRun(false)}
        >
          预检导入
        </Button>
        <Button
          size="lg"
          variant="secondary"
          disabled={busy || !canPreview || appliedIds !== null || (plan === null && !needsNewPreview)}
          onClick={() => runDryRun(true)}
        >
          重新预检
        </Button>
        <Button size="lg" disabled={busy || plan === null || appliedIds !== null} onClick={runApply}>
          确认应用
        </Button>
      </div>
      {progress ? <p className="record-tool__message" role="status">{progress}</p> : null}
      {plan ? (
        <>
          <p className="record-tool__target">{confirmedSubject(plan, kind, subjectId, subjectLabel)}</p>
          <ul className="record-tool__list">
            {plan.remaps.map((remap) => (
              <li key={`${remap.entity_kind}-${remap.source_id}`}>
                {remap.entity_kind} {remap.source_id} → {remap.target_id}
              </li>
            ))}
            {plan.quarantine.map((item) => (
              <li key={item.digest}>{item.kind} {item.schema}：{item.reason}</li>
            ))}
          </ul>
        </>
      ) : null}
      {appliedIds ? (
        <div>
          <p className="record-tool__message" role="status">{`已导入 ${appliedIds.length} 条记录`}</p>
          {appliedIds.length > 0 ? (
            <ul className="record-tool__list">
              {appliedIds.map((recordId) => (
                <li key={recordId}>
                  <Link to={recordDetailHref(recordId)}>{`查看记录 ${recordId}`}</Link>
                </li>
              ))}
            </ul>
          ) : null}
        </div>
      ) : null}
      {message ? (
        <p className="record-tool__message" role={messageIsFailure ? 'alert' : 'status'}>{message}</p>
      ) : null}
    </section>
  )
}
