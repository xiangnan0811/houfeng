import type { ComponentProps } from 'react'
import { Link } from 'react-router-dom'

import { GroupDetailModal } from '../modals/GroupDetailModal'
import { ManualGroupDetailModal } from '../modals/ManualGroupDetailModal'
import { RecordDetailModal } from '../modals/RecordDetailModal'
import { RenewalDecisionModal } from '../modals/RenewalDecisionModal'
import { TemplateDetailModal } from '../modals/TemplateDetailModal'
import { PortfolioWorkbench } from './PortfolioWorkbench'
import { SecondaryWorkbenches } from './SecondaryWorkbenches'

type AssetDecisionPageViewProps = {
  decisionNotice: string | null
  portfolio: ComponentProps<typeof PortfolioWorkbench>
  secondary: ComponentProps<typeof SecondaryWorkbenches>
  groupModal: ComponentProps<typeof GroupDetailModal>
  manualGroupModal: ComponentProps<typeof ManualGroupDetailModal>
  templateModal: ComponentProps<typeof TemplateDetailModal>
  renewalModal: ComponentProps<typeof RenewalDecisionModal>
  recordModal: ComponentProps<typeof RecordDetailModal>
}

export function AssetDecisionPageView({
  decisionNotice,
  portfolio,
  secondary,
  groupModal,
  manualGroupModal,
  templateModal,
  renewalModal,
  recordModal,
}: AssetDecisionPageViewProps) {
  return (
    <div className="page asset-decision-workbench">
      <header className="page__head">
        <h1 className="page__title">资产组合决策</h1>
        <div className="page__actions">
          <Link className="btn sm secondary" to="/vps">VPS 库存</Link>
          <Link className="btn sm secondary" to="/subscriptions">订阅列表</Link>
        </div>
      </header>

      {decisionNotice && <div className="inline-alert ok" role="status">{decisionNotice}</div>}
      <PortfolioWorkbench {...portfolio} />
      <SecondaryWorkbenches {...secondary} />
      <GroupDetailModal {...groupModal} />
      <ManualGroupDetailModal {...manualGroupModal} />
      <TemplateDetailModal {...templateModal} />
      <RenewalDecisionModal {...renewalModal} />
      <RecordDetailModal {...recordModal} />
    </div>
  )
}
