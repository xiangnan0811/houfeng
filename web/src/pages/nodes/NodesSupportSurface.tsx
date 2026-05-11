import { Link } from 'react-router-dom'

import { Badge, Button, MonoDigits } from '../../components/atoms'

type NodesSupportSurfaceProps = {
  totalNodeCount: number
  displayedNodeCount: number
  abnormalNodeCount: number
  pendingOnboardingNodeCount: number
  maintenanceOrPausedNodeCount: number
  hasActiveFilters: boolean
  onAbnormalClick: () => void
  onOnboardingClick: () => void
  onRuntimeAttentionClick: () => void
}

export function NodesSupportSurface({
  totalNodeCount,
  displayedNodeCount,
  abnormalNodeCount,
  pendingOnboardingNodeCount,
  maintenanceOrPausedNodeCount,
  hasActiveFilters,
  onAbnormalClick,
  onOnboardingClick,
  onRuntimeAttentionClick,
}: NodesSupportSurfaceProps) {
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
          <p>资产关联回到 VPS 台账和 Node 详情核对，避免把孤立运行事实当成资产结论。</p>
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
    </section>
  )
}
