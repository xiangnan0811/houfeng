import { useMemo, useState } from 'react'

import { Button, Select } from '../../components/atoms'
import { PageState } from '../../components/PageState'
import { ApiError } from '../../lib/apiRequest'
import {
  createRecordExport,
  downloadRecordExportContent,
  previewRecordExport,
} from '../../lib/recordsApi'
import type { RecordExportKind, RecordExportPreview } from '../../lib/types'

type ExportSurface = 'form' | 'revoked' | 'deleted'

type RecordExportPanelProps = {
  recordId: string
  revisionId?: string
  snapshotIds?: readonly string[]
  recordLabel?: string
}

function describeExportFailure(error: unknown, hadPreview: boolean): string {
  if (error instanceof ApiError) {
    if (error.code === 'export_lease_revoked') return '下载租约已撤销，停止继续读取。'
    if (error.code === 'resource_not_found') {
      return hadPreview ? '导出目标已删除，当前预览已失效。' : '导出未开放或无权访问该材料。'
    }
    if (error.code === 'export_inventory_drift') return '导出清单已变化，请重新预览。'
    return error.message
  }
  if (error instanceof Error) return error.message
  return '导出失败'
}

function exportSurfaceFor(error: unknown, hadPreview: boolean): ExportSurface | null {
  if (!(error instanceof ApiError)) return null
  if (error.code === 'export_lease_revoked') return 'revoked'
  if (error.code === 'resource_not_found' && hadPreview) return 'deleted'
  return null
}

/** Remount with `key={recordId}` when the target record changes so preview state cannot leak. */
export function RecordExportPanel({ recordId, revisionId, snapshotIds = [], recordLabel }: RecordExportPanelProps) {
  const comparisonSnapshots = useMemo(
    () => snapshotIds.filter((id) => id.startsWith('evs_')),
    [snapshotIds],
  )
  const [kind, setKind] = useState<RecordExportKind>('markdown')
  const [snapshotId, setSnapshotId] = useState(comparisonSnapshots[0] ?? '')
  const [preview, setPreview] = useState<RecordExportPreview | null>(null)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const [surface, setSurface] = useState<ExportSurface>('form')

  function runPreview() {
    setBusy(true)
    setMessage('')
    void previewRecordExport({
      record_id: recordId,
      ...(revisionId ? { revision_id: revisionId } : {}),
      export_kind: kind,
      export_mode: 'safe',
      ...((kind === 'comparison_json' || kind === 'evidence_json' || kind === 'archive') && snapshotId
        ? { snapshot_id: snapshotId }
        : {}),
    }, crypto.randomUUID())
      .then((next) => {
        setPreview(next)
        if (next.unavailable.length > 0) {
          setMessage(`有 ${next.unavailable.length} 项材料不可用，已按名称列出。`)
        }
      })
      .catch((error: unknown) => {
        const next = exportSurfaceFor(error, preview != null)
        if (next) {
          setSurface(next)
          setPreview(null)
          return
        }
        setMessage(describeExportFailure(error, preview != null))
      })
      .finally(() => setBusy(false))
  }

  function runDownload() {
    if (!preview) return
    setBusy(true)
    setMessage('')
    void createRecordExport({
      preview_id: preview.preview_id,
      preview_token: preview.preview_token,
      inventory_digest: preview.inventory_digest,
    }, crypto.randomUUID())
      .then((created) => downloadRecordExportContent(created.export_id))
      .then((blob) => {
        const file = preview.expected_files[0]
        const url = URL.createObjectURL(blob)
        const link = document.createElement('a')
        link.href = url
        link.download = file?.name ?? 'record-export'
        link.click()
        URL.revokeObjectURL(url)
      })
      .catch((error: unknown) => {
        const next = exportSurfaceFor(error, true)
        if (next) {
          setSurface(next)
          setPreview(null)
          return
        }
        setMessage(describeExportFailure(error, true))
      })
      .finally(() => setBusy(false))
  }

  if (surface === 'revoked') {
    return (
      <section className="card" aria-label="记录导出">
        <PageState kind="empty" title="导出访问已撤销" description="下载租约已撤销，停止继续读取。" />
      </section>
    )
  }
  if (surface === 'deleted') {
    return (
      <section className="card" aria-label="记录导出">
        <PageState kind="empty" title="导出目标已删除" description="当前预览已失效，记录或导出材料已不存在。" />
      </section>
    )
  }

  return (
    <section className="card" aria-label="记录导出">
      <h2>导出</h2>
      <p>从记录中心、详情或修订页下载已授权材料。比较工作台不提供下载。</p>
      {recordLabel ? <p>当前导出：{recordLabel}</p> : null}
      <div className="vps-create-form__row">
        <Select
          label="导出类型"
          value={kind}
          onChange={(event) => {
            setKind(event.target.value as RecordExportKind)
            setPreview(null)
            setMessage('')
          }}
        >
          <option value="markdown">Markdown</option>
          <option value="pdf">PDF 派生展示</option>
          <option value="archive">机器归档 ZIP</option>
          {comparisonSnapshots.length > 0 ? <option value="comparison_json">比较结果 JSON</option> : null}
          {comparisonSnapshots.length > 0 ? <option value="evidence_json">证据 JSON</option> : null}
        </Select>
        {kind === 'comparison_json' || kind === 'evidence_json' || (kind === 'archive' && comparisonSnapshots.length > 0) ? (
          <Select
            label="证据快照"
            value={snapshotId}
            onChange={(event) => {
              setSnapshotId(event.target.value)
              setPreview(null)
              setMessage('')
            }}
          >
            {comparisonSnapshots.map((id) => (
              <option key={id} value={id}>{id}</option>
            ))}
          </Select>
        ) : null}
      </div>
      <div className="page-form-actions">
        <Button size="lg" variant="secondary" disabled={busy} onClick={runPreview}>预览导出</Button>
        <Button size="lg" disabled={busy || !preview} onClick={runDownload}>下载</Button>
      </div>
      {preview ? (
        <ul>
          {preview.expected_files.map((file) => (
            <li key={file.name}>{file.name} · {file.byte_size} 字节</li>
          ))}
          {preview.unavailable.map((item) => (
            <li key={`${item.kind}-${item.id}`}>{item.kind} {item.id}：{item.reason}</li>
          ))}
        </ul>
      ) : null}
      {message ? <p role="status">{message}</p> : null}
    </section>
  )
}
