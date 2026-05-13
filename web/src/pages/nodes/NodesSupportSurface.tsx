import { Link } from 'react-router-dom'

import { Badge, Button, Hostname, MonoDigits, StatusGlyph } from '../../components/atoms'
import { nodeEvidenceGlyphState } from './nodeHelpers'
import type { NodeEvidenceItem, NodeEvidenceLead } from './types'

type NodesSupportSurfaceProps = {
  totalNodeCount: number
  displayedNodeCount: number
  abnormalNodeCount: number
  pendingOnboardingNodeCount: number
  maintenanceOrPausedNodeCount: number
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
  maintenanceOrPausedNodeCount,
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
          <p className="observability-support__description">
            Node 页面保留运行事实、接入状态和 freshness，用来判断 VPS 是否有可信观测证据。
          </p>
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

      <div className={`nodes-evidence-lead nodes-evidence-lead--${evidenceLead.tone}`}>
        <div className="nodes-evidence-lead__main">
          <p className="nodes-evidence-lead__eyebrow">{evidenceLead.eyebrow}</p>
          <h3>{evidenceLead.title}</h3>
          <p>{evidenceLead.description}</p>
          {filterContext.length > 0 ? (
            <div className="nodes-evidence-lead__filters" aria-label="当前证据筛选">
              {filterContext.map((item) => (
                <span key={item}>{item}</span>
              ))}
            </div>
          ) : (
            <div className="nodes-evidence-lead__filters" aria-label="当前证据筛选">
              <span>完整 Node 库存</span>
            </div>
          )}
        </div>
        <div className="nodes-evidence-lead__action">
          {evidenceLead.actionKind === 'asset' ? (
            <Link className="btn btn--secondary btn--md" to="/vps">
              {evidenceLead.actionLabel}
            </Link>
          ) : (
            <Button variant="secondary" size="md" onClick={handleLeadAction}>
              {evidenceLead.actionLabel}
            </Button>
          )}
          <Link className="observability-support-link" to="/asset-decisions">
            资产决策队列
          </Link>
        </div>
      </div>

      <div className="observability-support__grid" aria-label="节点观测证据摘要">
        <article className="observability-support-lane observability-support-lane--alert">
          <div className="observability-support-lane__head">
            <span>异常证据</span>
            <Badge variant="count" tone={abnormalNodeCount > 0 ? 'alert' : 'normal'}>
              <MonoDigits>{abnormalNodeCount}</MonoDigits>
            </Badge>
          </div>
          <p>健康状态不是正常的节点，优先进入异常排查和资产稳定性判断。</p>
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
          <p>待接入、未绑定或指纹变更待确认的节点会削弱 VPS 证据可信度。</p>
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

        <article className="observability-support-lane observability-support-lane--maintenance">
          <div className="observability-support-lane__head">
            <span>维护 / 暂停</span>
            <Badge
              variant="count"
              tone={maintenanceOrPausedNodeCount > 0 ? 'maintenance' : 'normal'}
            >
              <MonoDigits>{maintenanceOrPausedNodeCount}</MonoDigits>
            </Badge>
          </div>
          <p>维护或暂停会解释趋势空窗，避免把人为窗口误判成资产故障。</p>
          <div className="observability-support-lane__actions">
            <Button
              variant="secondary"
              size="sm"
              onClick={onRuntimeAttentionClick}
              disabled={maintenanceOrPausedNodeCount === 0}
            >
              运行关注
            </Button>
            <Link className="observability-support-link" to="/events?object_type=node&maintenance_only=1">
              维护事件
            </Link>
          </div>
        </article>

        <article className="observability-support-lane observability-support-lane--asset">
          <div className="observability-support-lane__head">
            <span>VPS 关联</span>
            <Badge variant="info" tone="neutral">资产上下文</Badge>
          </div>
          <p>列表不推导 linked VPS health；资产关联回到 VPS 台账和 Node 详情核对。</p>
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
          <article className="nodes-evidence-focus">
            <div className="nodes-evidence-focus__glyph">
              <StatusGlyph
                state={nodeEvidenceGlyphState(topEvidence.node)}
                ariaLabel={`${topEvidence.title} 证据状态`}
              />
            </div>
            <div className="nodes-evidence-focus__body">
              <p className="nodes-evidence-focus__eyebrow">优先核对节点</p>
              <h3>优先核对：{topEvidence.title}</h3>
              <p>{topEvidence.reason}</p>
              <span>
                <Hostname truncate maxChars={18}>{topEvidence.node.node_id}</Hostname>
                {' · '}
                {topEvidence.meta}
              </span>
            </div>
            <Link className="btn btn--ghost btn--sm" to={topEvidence.route}>
              {topEvidence.actionLabel}
            </Link>
          </article>
        ) : (
          <article className="nodes-evidence-focus nodes-evidence-focus--stable">
            <div className="nodes-evidence-focus__glyph">
              <StatusGlyph state="normal" ariaLabel="Node 证据稳定" />
            </div>
            <div className="nodes-evidence-focus__body">
              <p className="nodes-evidence-focus__eyebrow">运行证据</p>
              <h3>没有需要优先核对的 Node</h3>
              <p>当前列表没有异常、接入缺口、维护或暂停对象。</p>
              <span>继续从 VPS 库存、订阅和资产决策队列核对资产侧事实。</span>
            </div>
            <Link className="btn btn--ghost btn--sm" to="/vps">
              查看 VPS
            </Link>
          </article>
        )}
      </div>
    </section>
  )
}
