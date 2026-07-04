import { Modal } from '../../../components/atoms'
import { AssetDecisionWorkPanel, type AssetDecisionDraft } from '../../../components/AssetDecisionWorkPanel'
import type { VPSAssetRecord } from '../../../lib/types'

type RenewalDecisionModalProps = {
  open: boolean
  selectedVPS: VPSAssetRecord | null
  decisionDraft: AssetDecisionDraft
  submitting: boolean
  error: string | null
  onDraftChange: (draft: AssetDecisionDraft) => void
  onSubmit: (event: React.FormEvent<HTMLFormElement>) => void
  onClose: () => void
}

export function RenewalDecisionModal({
  open,
  selectedVPS,
  decisionDraft,
  submitting,
  error,
  onDraftChange,
  onSubmit,
  onClose,
}: RenewalDecisionModalProps) {
  return (
    <Modal
      open={open}
      onClose={onClose}
      title={selectedVPS ? `处理 ${selectedVPS.display_name}` : '处理续费决策'}
      ariaLabel="续费决策处理"
    >
      <AssetDecisionWorkPanel
        surface="drawer"
        selectedVPS={selectedVPS}
        decisionDraft={decisionDraft}
        submitting={submitting}
        error={error}
        notice={null}
        onDraftChange={onDraftChange}
        onSubmit={onSubmit}
        onCancel={onClose}
      />
    </Modal>
  )
}
