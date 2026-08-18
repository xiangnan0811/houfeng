import { useState } from 'react'

import { Button, Modal } from '../../../components/atoms'
import type { RecordDraftPayload, RecordRevision } from '../../../lib/types'
import {
  differingRecordFields,
  mergeRecordFields,
  recordComparablePayload,
  recordFieldText,
  type RecordComparableField,
} from '../recordFields'
import { RevisionDiff } from './RevisionDiff'

type RecordConflictResolverProps = {
  open: boolean
  local: RecordDraftPayload
  server: RecordDraftPayload | RecordRevision
  base?: RecordDraftPayload | RecordRevision | null
  onClose: () => void
  onResolve: (payload: RecordDraftPayload) => void
}

export function RecordConflictResolver({
  open,
  local,
  server,
  base,
  onClose,
  onResolve,
}: RecordConflictResolverProps) {
  const localPayload = recordComparablePayload(local)
  const serverPayload = recordComparablePayload(server)
  const basePayload = base ? recordComparablePayload(base) : null
  const changed = differingRecordFields(localPayload, serverPayload)
  const changedKey = changed.map(({ field }) => field).join(',')
  const [selection, setSelection] = useState<{ key: string; fields: readonly RecordComparableField[] }>({
    key: changedKey,
    fields: [],
  })

  // A new conflict describes different fields, so a selection recorded against an
  // earlier one must not silently take the server value for a field the operator
  // never saw. Keying the selection expires it without a reset effect.
  const serverFields = selection.key === changedKey ? selection.fields : []

  const chooseServer = (field: RecordComparableField, useServer: boolean) => {
    const remaining = serverFields.filter((item) => item !== field)
    setSelection({ key: changedKey, fields: useServer ? [...remaining, field] : remaining })
  }

  return (
    <Modal open={open} onClose={onClose} title="修订冲突" size="lg" dialogRole="alertdialog">
      <p>服务端已推进。请逐字段选择要保留的来源后再保存，未选择不会发送正式修订。</p>
      <RevisionDiff base={serverPayload} local={localPayload} />
      {changed.length === 0 ? <p className="text-muted">两侧字段一致，可直接保留本地内容。</p> : (
        <ul className="metadata-list" aria-label="逐字段选择">
          {changed.map(({ field, label }) => {
            const useServer = serverFields.includes(field)
            return (
              <li key={field}>
                <span className="metadata-list__label">{label}</span>
                <fieldset className="page-form-actions">
                  <legend className="sr-only">{label}</legend>
                  <label>
                    <input
                      type="radio"
                      name={`conflict-${field}`}
                      checked={!useServer}
                      onChange={() => chooseServer(field, false)}
                    />
                    本地：{truncate(recordFieldText(localPayload, field))}
                  </label>
                  <label>
                    <input
                      type="radio"
                      name={`conflict-${field}`}
                      checked={useServer}
                      onChange={() => chooseServer(field, true)}
                    />
                    服务端：{truncate(recordFieldText(serverPayload, field))}
                  </label>
                </fieldset>
              </li>
            )
          })}
        </ul>
      )}
      <div className="archive-detail-two-col">
        <ConflictColumn title="本地" payload={localPayload} />
        <ConflictColumn title="服务端" payload={serverPayload} />
        {basePayload ? <ConflictColumn title="基线" payload={basePayload} /> : null}
      </div>
      <div className="page-form-actions">
        <Button onClick={() => onResolve(mergeRecordFields(localPayload, serverPayload, serverFields))}>
          应用所选合并
        </Button>
        <Button variant="secondary" onClick={() => onResolve(localPayload)}>全部保留本地</Button>
        <Button variant="secondary" onClick={() => onResolve(serverPayload)}>全部采用服务端</Button>
        {basePayload ? <Button variant="ghost" onClick={() => onResolve(basePayload)}>恢复基线</Button> : null}
      </div>
    </Modal>
  )
}

function ConflictColumn({ title, payload }: { title: string; payload: RecordDraftPayload }) {
  return (
    <article className="card">
      <h3>{title}</h3>
      <p>{payload.title || '（无标题）'}</p>
      <pre>{payload.body_markdown}</pre>
    </article>
  )
}

function truncate(value: string): string {
  const single = value.replace(/\s+/gu, ' ').trim()
  if (single === '') return '（空）'
  return single.length > 60 ? `${single.slice(0, 60)}…` : single
}
