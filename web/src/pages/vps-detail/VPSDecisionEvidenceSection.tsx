import { Button, MonoDigits } from '../../components/atoms'
import { formatOptional } from '../../lib/format'
import {
  VPS_EXPERIENCE_CATEGORY_LABELS,
  VPS_EXPERIENCE_SEVERITY_LABELS,
  type VPSTimeline,
} from '../../lib/types'
import { VPSDetailEvidenceCard } from './VPSDetailEvidenceCard'

type VPSDecisionEvidenceSectionProps = {
  timeline: VPSTimeline
  notice: string | null
  onExperienceLog: () => void
}

export function VPSDecisionEvidenceSection({
  timeline,
  notice,
  onExperienceLog,
}: VPSDecisionEvidenceSectionProps) {
  const latestDecision = timeline.renewal_decisions[0]
  const latestExperience = timeline.experience_logs[0]

  return (
    <section className="page-panel vps-detail-section vps-decision-evidence" aria-labelledby="vps-decision-evidence-title">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">DECISION NOTES</p>
          <h2 id="vps-decision-evidence-title">决策依据与经验记录</h2>
          <p className="section-heading__description">
            把保留、观察、迁移或取消的理由沉淀到资产历史，避免详情页变成一次性字段表。
          </p>
        </div>
        <div className="section-heading__actions">
          <Button variant="secondary" size="sm" onClick={onExperienceLog}>补充经验记录</Button>
        </div>
      </div>

      {notice ? <p className="asset-operation-feedback" role="status">{notice}</p> : null}

      <div className="vps-detail-evidence-grid vps-detail-evidence-grid--decision">
        <VPSDetailEvidenceCard
          label="最近决策理由"
          value={latestDecision?.reason || '尚未记录决策理由'}
          meta={latestDecision ? `决策记录 ${latestDecision.decision_id}` : '可通过“调整决策”补充理由'}
          tone={latestDecision ? 'normal' : 'notice'}
        />
        <VPSDetailEvidenceCard
          label="经验记录"
          value={<><MonoDigits>{timeline.experience_logs.length}</MonoDigits> 条</>}
          meta={latestExperience ? latestExperience.summary : '尚未沉淀稳定性、网络、账单或迁移经验'}
          tone={timeline.experience_logs.length > 0 ? 'normal' : 'notice'}
        />
        <VPSDetailEvidenceCard
          label="最近经验"
          value={latestExperience ? VPS_EXPERIENCE_CATEGORY_LABELS[latestExperience.category] : '暂无'}
          meta={latestExperience ? `${VPS_EXPERIENCE_SEVERITY_LABELS[latestExperience.severity]} · ${formatOptional(latestExperience.details)}` : '记录后会进入资产历史 timeline'}
        />
      </div>
    </section>
  )
}
