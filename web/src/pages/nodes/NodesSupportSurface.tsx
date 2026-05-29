import { Link } from 'react-router-dom'

import { Badge, Button, Hostname, MonoDigits, StatusGlyph } from '../../components/atoms'
import { ObservabilityEvidenceFocus } from '../../components/ObservabilityEvidenceFocus'
import { ObservabilityEvidenceLead } from '../../components/ObservabilityEvidenceLead'
import { nodeEvidenceGlyphState } from './nodeHelpers'
import type { NodeEvidenceItem, NodeEvidenceLead } from './types'

type NodesSupportSurfaceProps = {
  totalNodeCount: number
  displayedNodeCount: number
  abnormalNodeCount: number
  pendingOnboardingNodeCount: number
  evidenceLead: NodeEvidenceLead
  topEvidence: NodeEvidenceItem | null
  filterContext: string[]
  hasActiveFilters: boolean
  onAbnormalClick: () => void
  onOnboardingClick: () => void
  onRuntimeAttentionClick: () => void
  onClearFilters: () => void
  onCreateClick: () => void
}

export function NodesSupportSurface({
  totalNodeCount,
  displayedNodeCount,
  abnormalNodeCount,
  pendingOnboardingNodeCount,
  evidenceLead,
  topEvidence,
  filterContext,
  hasActiveFilters,
  onAbnormalClick,
  onOnboardingClick,
  onRuntimeAttentionClick,
  onClearFilters,
  onCreateClick,
}: NodesSupportSurfaceProps) {
  function handleLeadAction() {
    if (evidenceLead.actionKind === 'abnormal') {
      onAbnormalClick()
    } else if (evidenceLead.actionKind === 'onboarding') {
      onOnboardingClick()
    } else if (evidenceLead.actionKind === 'runtime') {
      onRuntimeAttentionClick()
    } else if (evidenceLead.actionKind === 'clear') {
      onClearFilters()
    } else if (evidenceLead.actionKind === 'create') {
      onCreateClick()
    }
  }

  return (
    <section className="page-panel observability-support observability-support--nodes">
      <div className="observability-support__header">
        <div>
          <p className="observability-support__eyebrow">OBSERVABILITY SUPPORT</p>
          <h2 className="observability-support__title">资产判断支撑</h2>
        </div>
        <div className="observability-support__scope" aria-label="当前节点筛选范围">
          <span>{hasActiveFilters ? '当前筛选' : '完整库存'}</span>
          <strong>
            <MonoDigits>{displayedNodeCount}</MonoDigits>
            <small>/</small>
            <MonoDigits>{totalNodeCount}</MonoDigits>
          </strong>
        </div>
      </div>

      <ObservabilityEvidenceLead
        tone={evidenceLead.tone}
        eyebrow={evidenceLead.eyebrow}
        title={evidenceLead.title}
        description={evidenceLead.description}
        filterItems={filterContext}
        emptyFilterLabel="完整 Node 库存"
        filterAriaLabel="当前证据筛选"
        action={
          evidenceLead.actionKind === 'asset' ? (
            <Link className="btn md secondary" to="/vps">
              {evidenceLead.actionLabel}
            </Link>
          ) : (
            <Button variant="secondary" size="md" onClick={handleLeadAction}>
              {evidenceLead.actionLabel}
            </Button>
          )
        }
        secondaryAction={
          <Link className="observability-support-link" to="/asset-decisions">
            资产决策队列
          </Link>
        }
      />

      <div className="observability-support__grid" aria-label="节点观测证据摘要">
        <article className="observability-support-lane observability-support-lane--alert">
          <div className="observability-support-lane__head">
            <span>异常证据</span>
            <Badge variant="count" tone={abnormalNodeCount > 0 ? 'alert' : 'normal'}>
              <MonoDigits>{abnormalNodeCount}</MonoDigits>
            </Badge>
          </div>
          <p>健康异常</p>
          <div className="observability-support-lane__actions">
            <Button
              variant="secondary"
              size="sm"
              onClick={onAbnormalClick}
              disabled={abnormalNodeCount === 0}
            >
              仅看异常
            </Button>
            <Link className="observability-support-link" to="/events?object_type=node&severity=严重">
              严重事件
            </Link>
          </div>
        </article>

        <article className="observability-support-lane observability-support-lane--notice">
          <div className="observability-support-lane__head">
            <span>接入 / 绑定</span>
            <Badge variant="count" tone={pendingOnboardingNodeCount > 0 ? 'notice' : 'normal'}>
              <MonoDigits>{pendingOnboardingNodeCount}</MonoDigits>
            </Badge>
          </div>
          <p>未接入 / 待确认</p>
          <div className="observability-support-lane__actions">
            <Button
              variant="secondary"
              size="sm"
              onClick={onOnboardingClick}
              disabled={pendingOnboardingNodeCount === 0}
            >
              待接入/绑定
            </Button>
            <Link className="observability-support-link" to="/nodes?onboarding=pending">
              深链视图
            </Link>
          </div>
        </article>

        <article className="observability-support-lane observability-support-lane--asset">
          <div className="observability-support-lane__head">
            <span>VPS 关联</span>
            <Badge variant="info" tone="neutral">资产上下文</Badge>
          </div>
          <p>资产侧核对</p>
          <div className="observability-support-lane__actions">
            <Link className="observability-support-link" to="/vps?view=unlinked">
              未关联 VPS
            </Link>
            <Link className="observability-support-link" to="/asset-decisions">
              决策队列
            </Link>
          </div>
        </article>
      </div>

      <div className="nodes-evidence-context" aria-label="节点证据下一步">
        {topEvidence ? (
          <ObservabilityEvidenceFocus
            glyph={
              <StatusGlyph
                state={nodeEvidenceGlyphState(topEvidence.node)}
                ariaLabel={`${topEvidence.title} 证据状态`}
              />
            }
            eyebrow="优先核对节点"
            title={`优先核对：${topEvidence.title}`}
            description={topEvidence.reason}
            meta={
              <>
                <Hostname truncate maxChars={18}>{topEvidence.node.node_id}</Hostname>
                {' · '}
                {topEvidence.meta}
              </>
            }
            action={
              <Link className="btn sm ghost" to={topEvidence.route}>
                {topEvidence.actionLabel}
              </Link>
            }
          />
        ) : (
          <ObservabilityEvidenceFocus
            stable
            glyph={<StatusGlyph state="normal" ariaLabel="Node 证据稳定" />}
            eyebrow="运行证据"
            title="Node 当前稳定"
            description="无异常 / 接入缺口。"
            meta="VPS 库存 / 资产决策"
            action={
              <Link className="btn sm ghost" to="/vps">
                查看 VPS
              </Link>
            }
          />
        )}
      </div>
    </section>
  )
}
