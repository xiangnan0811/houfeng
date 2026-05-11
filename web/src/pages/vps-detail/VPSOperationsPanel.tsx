import type { FormEvent } from 'react'

import type { VPSAssetDetail, VPSTimeline } from '../../lib/types'
import type { DecisionDraftState, ExperienceDraftState, LinkDraftState } from './types'
import { VPSExperienceLogForm } from './VPSExperienceLogForm'
import { VPSLifecycleCard } from './VPSLifecycleCard'
import { VPSNodeLinkForm } from './VPSNodeLinkForm'
import { VPSRenewalDecisionForm } from './VPSRenewalDecisionForm'

type VPSOperationsPanelProps = {
  detail: VPSAssetDetail
  timeline: VPSTimeline
  decisionDraft: DecisionDraftState
  decisionSubmitting: boolean
  decisionError: string | null
  decisionNotice: string | null
  decisionChanged: boolean
  onDecisionDraftChange: (draft: DecisionDraftState) => void
  onDecisionFeedbackClear: () => void
  onDecisionSubmit: (event: FormEvent<HTMLFormElement>) => void
  linkDraft: LinkDraftState
  linkControlsDisabled: boolean
  linkSubmitting: boolean
  linkFeedback: string | null
  linkFeedbackIsError: boolean
  onLinkDraftChange: (draft: LinkDraftState) => void
  onLinkFeedbackClear: () => void
  onLinkSubmit: (event: FormEvent<HTMLFormElement>) => void
  isArchived: boolean
  lifecycleConfirmingArchive: boolean
  lifecycleSubmitting: boolean
  lifecycleError: string | null
  lifecycleNotice: string | null
  onLifecycleConfirmingArchiveChange: (open: boolean) => void
  onArchive: () => void
  onRestore: () => void
  experienceDraft: ExperienceDraftState
  experienceSubmitting: boolean
  experienceError: string | null
  experienceNotice: string | null
  onExperienceDraftChange: (draft: ExperienceDraftState) => void
  onExperienceFeedbackClear: () => void
  onExperienceSubmit: (event: FormEvent<HTMLFormElement>) => void
}

export function VPSOperationsPanel({
  detail,
  timeline,
  decisionDraft,
  decisionSubmitting,
  decisionError,
  decisionNotice,
  decisionChanged,
  onDecisionDraftChange,
  onDecisionFeedbackClear,
  onDecisionSubmit,
  linkDraft,
  linkControlsDisabled,
  linkSubmitting,
  linkFeedback,
  linkFeedbackIsError,
  onLinkDraftChange,
  onLinkFeedbackClear,
  onLinkSubmit,
  isArchived,
  lifecycleConfirmingArchive,
  lifecycleSubmitting,
  lifecycleError,
  lifecycleNotice,
  onLifecycleConfirmingArchiveChange,
  onArchive,
  onRestore,
  experienceDraft,
  experienceSubmitting,
  experienceError,
  experienceNotice,
  onExperienceDraftChange,
  onExperienceFeedbackClear,
  onExperienceSubmit,
}: VPSOperationsPanelProps) {
  return (
    <section className="page-panel asset-operation-panel">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">OPERATIONS</p>
          <h2>资产操作</h2>
        </div>
        <span className="section-heading__meta">
          更新会立即写入资产台账
        </span>
      </div>
      <div className="asset-operation-grid">
        <VPSRenewalDecisionForm
          detail={detail}
          draft={decisionDraft}
          submitting={decisionSubmitting}
          error={decisionError}
          notice={decisionNotice}
          decisionChanged={decisionChanged}
          onDraftChange={onDecisionDraftChange}
          onFeedbackClear={onDecisionFeedbackClear}
          onSubmit={onDecisionSubmit}
        />

        <VPSNodeLinkForm
          detail={detail}
          draft={linkDraft}
          controlsDisabled={linkControlsDisabled}
          submitting={linkSubmitting}
          error={linkFeedbackIsError ? linkFeedback : null}
          notice={linkFeedbackIsError ? null : linkFeedback}
          onDraftChange={onLinkDraftChange}
          onFeedbackClear={onLinkFeedbackClear}
          onSubmit={onLinkSubmit}
        />

        <VPSLifecycleCard
          detail={detail}
          isArchived={isArchived}
          confirmingArchive={lifecycleConfirmingArchive}
          submitting={lifecycleSubmitting}
          error={lifecycleError}
          notice={lifecycleNotice}
          onArchiveConfirmOpenChange={onLifecycleConfirmingArchiveChange}
          onArchive={onArchive}
          onRestore={onRestore}
        />

        <VPSExperienceLogForm
          timeline={timeline}
          draft={experienceDraft}
          submitting={experienceSubmitting}
          error={experienceError}
          notice={experienceNotice}
          onDraftChange={onExperienceDraftChange}
          onFeedbackClear={onExperienceFeedbackClear}
          onSubmit={onExperienceSubmit}
        />
      </div>
    </section>
  )
}
