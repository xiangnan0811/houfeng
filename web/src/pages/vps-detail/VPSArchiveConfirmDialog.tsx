import { useId } from 'react'

import { ArchiveBlockerDetails } from '../../components/ArchiveBlockerDetails'
import { Button, Input, Modal } from '../../components/atoms'
import type { ArchiveBlockerDetail, ArchiveReview } from '../../lib/types'
import {
  ARCHIVE_IMPACT_NOTE,
  NEVER_CONNECTED_CHECKBOX_LABEL,
  NEVER_CONNECTED_EXPLANATION,
  archiveImpactRows,
  archiveInformationalWarnings,
  archiveOutcomeCopy,
  canConfirmArchive,
  connectedEvidenceLines,
  skipsArchiveObservation,
} from './archiveConfirmModel'

type InlineBlocker = (detail: ArchiveBlockerDetail, kind: 'service-status' | 'domain-status' | 'residual' | 'restore') => void

export type VPSArchiveConfirmDialogProps = {
  open: boolean
  title: string
  confirmLabel: string
  submitting: boolean
  displayName: string
  review: ArchiveReview | null
  loading: boolean
  error: string | null
  reason: string
  confirmationName: string
  neverConnectedConfirmed: boolean
  confirmDisabled: boolean
  onReasonChange: (value: string) => void
  onConfirmationNameChange: (value: string) => void
  onNeverConnectedChange: (checked: boolean) => void
  onConfirm: () => void
  onCancel: () => void
  onRetry: () => void
  vpsId: string
  onInlineBlocker: InlineBlocker
}

export function VPSArchiveConfirmDialog({
  open,
  title,
  confirmLabel,
  submitting,
  displayName,
  review,
  loading,
  error,
  reason,
  confirmationName,
  neverConnectedConfirmed,
  confirmDisabled,
  onReasonChange,
  onConfirmationNameChange,
  onNeverConnectedChange,
  onConfirm,
  onCancel,
  onRetry,
  vpsId,
  onInlineBlocker,
}: VPSArchiveConfirmDialogProps) {
  const explanationId = useId()
  const showForm = canConfirmArchive(review, loading)
  const neverConnected = skipsArchiveObservation(review?.online_evidence)
  const showAttest = showForm && Boolean(review?.online_evidence?.manual_confirmation_required)
  const outcome = archiveOutcomeCopy(review?.vps.display_name?.trim() || displayName)
  const warnings = review ? archiveInformationalWarnings(review.warnings, neverConnected) : []
  const evidence = review ? connectedEvidenceLines(review.online_evidence, review.monitoring_instance_links) : []
  const noteExplanation = neverConnected && !showAttest ? NEVER_CONNECTED_EXPLANATION : null
  const showNotes = Boolean(noteExplanation) || warnings.length > 0 || evidence.length > 0
  const noteTitle = warnings.length === 0 && evidence.length > 0 && !noteExplanation ? '安全观察' : '需要知道'
  const confirmName = review?.vps.display_name ?? displayName

  return (
    <Modal
      open={open}
      onClose={onCancel}
      title={title}
      dialogRole="alertdialog"
      size="md"
      contentClassName="asset-archive-dialog-modal"
      footer={(
        <div className="asset-archive-dialog__footer">
          {error ? <p className="asset-archive-dialog__error" role="alert">{error}</p> : null}
          <div className="asset-archive-dialog__actions">
            <Button variant="secondary" disabled={submitting} onClick={onCancel}>取消</Button>
            <Button variant="primary" disabled={confirmDisabled} onClick={onConfirm}>{confirmLabel}</Button>
          </div>
        </div>
      )}
    >
      <div className="asset-archive-dialog">
        {review && !loading ? <ArchiveStop review={review} error={error} vpsId={vpsId} onInline={onInlineBlocker} /> : null}

        <section className="asset-archive-dialog__section" aria-labelledby="archive-outcome-title">
          <h4 id="archive-outcome-title" className="asset-archive-dialog__title">会发生什么</h4>
          <p className="asset-archive-dialog__lead">{outcome.lead}</p>
          <p className="asset-archive-dialog__aside">{outcome.aside}</p>
        </section>

        {loading ? <p className="asset-archive-dialog__status" role="status">正在检查归档资格…</p> : null}

        {!loading && !review ? (
          <div className="asset-archive-dialog__section">
            <p className="asset-archive-dialog__status">归档资格暂未加载成功，请重试或关闭。</p>
            <Button onClick={onRetry}>重试加载</Button>
          </div>
        ) : null}

        {review && !loading ? (
          <section className="asset-archive-dialog__section" aria-labelledby="archive-impact-title">
            <h4 id="archive-impact-title" className="asset-archive-dialog__title">会影响</h4>
            <dl className="asset-archive-dialog__facts">
              {archiveImpactRows(review).map((row) => (
                <div key={row.label}>
                  <dt>{row.label}</dt>
                  <dd>{row.value}</dd>
                </div>
              ))}
            </dl>
            <p className="asset-archive-dialog__footnote">{ARCHIVE_IMPACT_NOTE}</p>
          </section>
        ) : null}

        {review && !loading && showNotes ? (
          <section className="asset-archive-dialog__section" aria-labelledby="archive-notes-title">
            <h4 id="archive-notes-title" className="asset-archive-dialog__title">{noteTitle}</h4>
            {noteExplanation ? <p className="asset-archive-dialog__explain">{noteExplanation}</p> : null}
            {warnings.length > 0 ? (
              <ul className="asset-archive-dialog__notes">
                {warnings.map((warning) => <li key={warning}>{warning}</li>)}
              </ul>
            ) : null}
            {warnings.length > 0 && evidence.length > 0 ? <h5 className="asset-archive-dialog__subtitle">安全观察</h5> : null}
            {evidence.length > 0 ? (
              <ul className="asset-archive-dialog__notes">
                {evidence.map((line) => (
                  <li key={line}>
                    <span className="asset-archive-dialog__note"><EvidenceLine line={line} /></span>
                  </li>
                ))}
              </ul>
            ) : null}
          </section>
        ) : null}

        {showForm && review ? (
          <section className="asset-archive-dialog__section" aria-labelledby="archive-confirm-title">
            <h4 id="archive-confirm-title" className="asset-archive-dialog__title">请确认</h4>
            {showAttest ? (
              <div className="asset-archive-dialog__attest">
                <p id={explanationId} className="asset-archive-dialog__explain">{NEVER_CONNECTED_EXPLANATION}</p>
                <label className="asset-archive-dialog__check">
                  <input
                    type="checkbox"
                    checked={neverConnectedConfirmed}
                    disabled={submitting}
                    aria-required="true"
                    aria-describedby={explanationId}
                    onChange={(event) => onNeverConnectedChange(event.target.checked)}
                  />
                  <span className="asset-archive-dialog__check-text">
                    {NEVER_CONNECTED_CHECKBOX_LABEL}
                    <span className="asset-archive-dialog__required" aria-hidden="true">
                      {neverConnectedConfirmed ? '已确认' : '必填'}
                    </span>
                  </span>
                </label>
              </div>
            ) : null}
            <Input
              label="归档原因"
              required
              value={reason}
              disabled={submitting}
              onChange={(event) => onReasonChange(event.target.value)}
            />
            <Input
              label="输入 VPS 名称确认归档"
              required
              value={confirmationName}
              hint={`需要完整匹配：${confirmName}`}
              disabled={submitting}
              onChange={(event) => onConfirmationNameChange(event.target.value)}
            />
          </section>
        ) : null}
      </div>
    </Modal>
  )
}

function EvidenceLine({ line }: { line: string }) {
  const parts = line.split(/(\d{4}\/\d{2}\/\d{2}\s+\d{2}:\d{2})/)
  return parts.map((part, index) => (
    /^\d{4}\/\d{2}\/\d{2}/.test(part)
      ? <span key={index} className="asset-archive-dialog__time">{part}</span>
      : part
  ))
}

function ArchiveStop({
  review,
  error,
  vpsId,
  onInline,
}: {
  review: ArchiveReview
  error: string | null
  vpsId: string
  onInline: InlineBlocker
}) {
  if ((review.blocker_details?.length ?? 0) > 0) {
    return (
      <section className="asset-archive-dialog__stop" aria-labelledby="archive-stop-title">
        <h4 id="archive-stop-title" className="asset-archive-dialog__title">归档前仍有需要处理的事项。</h4>
        <ArchiveBlockerDetails details={review.blocker_details} vpsId={vpsId} onInline={onInline} />
      </section>
    )
  }
  if (review.blockers.length > 0) {
    return (
      <section className="asset-archive-dialog__stop" aria-labelledby="archive-stop-title">
        <h4 id="archive-stop-title" className="asset-archive-dialog__title">归档前仍有需要处理的事项。</h4>
        <ul className="asset-lifecycle-confirm__blockers">
          {review.blockers.map((blocker) => <li key={blocker}>{blocker}</li>)}
        </ul>
      </section>
    )
  }
  if (!review.eligible) {
    return (
      <p className="asset-archive-dialog__stop" role={error ? undefined : 'alert'}>
        当前不能归档。请根据审查处理对象，不要把这次拒绝理解成归档已经失败。
      </p>
    )
  }
  return null
}
