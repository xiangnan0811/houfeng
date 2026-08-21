import { useState } from 'react'

import { Button, Input } from '../../components/atoms'
import { ApiError } from '../../lib/apiRequest'
import { applyRecordImport, dryRunRecordImport } from '../../lib/recordsApi'
import type { RecordImportPlan } from '../../lib/types'

function describeImportFailure(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.code === 'origin_tombstoned') return '该来源已墓碑化，不能官方恢复或再导入。'
    if (error.code === 'import_origin_conflict') return '该归档已导入过，不能再次官方导入。'
    if (error.code === 'import_cas_conflict') return '导入计划已变化，请重新预检。'
    if (error.code === 'resource_not_found') return '导入未开放或无权访问。'
    return error.message
  }
  if (error instanceof Error) return error.message
  return '导入失败'
}

export function RecordImportPanel() {
  const [file, setFile] = useState<File | null>(null)
  const [plan, setPlan] = useState<RecordImportPlan | null>(null)
  const [busy, setBusy] = useState(false)
  const [progress, setProgress] = useState('')
  const [message, setMessage] = useState('')

  function runDryRun() {
    if (!file) return
    setBusy(true)
    setMessage('')
    setProgress('正在预检归档…')
    void dryRunRecordImport(file, crypto.randomUUID())
      .then((next) => {
        setPlan(next)
        setProgress('预检完成')
        if (next.quarantine.length > 0) {
          setMessage(`有 ${next.quarantine.length} 项证据已隔离，仅展示信封，不会当作可信证据。`)
        }
      })
      .catch((error: unknown) => {
        setPlan(null)
        setProgress('')
        setMessage(describeImportFailure(error))
      })
      .finally(() => setBusy(false))
  }

  function runApply() {
    if (!plan) return
    setBusy(true)
    setMessage('')
    setProgress('正在应用导入计划…')
    void applyRecordImport(plan.plan_id, plan.lock_version)
      .then((result) => {
        setProgress(`已导入 ${result.record_ids.length} 条记录`)
      })
      .catch((error: unknown) => {
        setProgress('')
        setMessage(describeImportFailure(error))
      })
      .finally(() => setBusy(false))
  }

  return (
    <section className="card" aria-label="记录导入">
      <h2>导入</h2>
      <p>先预检机器归档，再确认应用。比较工作台不提供导入。</p>
      <Input
        label="归档文件"
        type="file"
        accept="application/zip,.zip"
        onChange={(event) => {
          setFile(event.target.files?.[0] ?? null)
          setPlan(null)
          setProgress('')
          setMessage('')
        }}
      />
      <div className="page-form-actions">
        <Button size="sm" variant="secondary" disabled={busy || !file} onClick={runDryRun}>预检导入</Button>
        <Button size="sm" disabled={busy || !plan} onClick={runApply}>确认应用</Button>
      </div>
      {progress ? <p role="status">{progress}</p> : null}
      {plan ? (
        <ul>
          {plan.remaps.map((remap) => (
            <li key={`${remap.entity_kind}-${remap.source_id}`}>
              {remap.entity_kind} {remap.source_id} → {remap.target_id}
            </li>
          ))}
          {plan.quarantine.map((item) => (
            <li key={item.digest}>{item.kind} {item.schema}：{item.reason}</li>
          ))}
        </ul>
      ) : null}
      {message ? <p role="status">{message}</p> : null}
    </section>
  )
}
