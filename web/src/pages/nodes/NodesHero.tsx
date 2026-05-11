import { Link } from 'react-router-dom'

import { Button, MonoDigits } from '../../components/atoms'

type NodesHeroProps = {
  totalNodeCount: number
  abnormalNodeCount: number
  pendingOnboardingNodeCount: number
  maintenanceOrPausedNodeCount: number
  onAbnormalClick: () => void
  onOnboardingClick: () => void
  onRuntimeAttentionClick: () => void
  onCreateClick: () => void
}

export function NodesHero({
  totalNodeCount,
  abnormalNodeCount,
  pendingOnboardingNodeCount,
  maintenanceOrPausedNodeCount,
  onAbnormalClick,
  onOnboardingClick,
  onRuntimeAttentionClick,
  onCreateClick,
}: NodesHeroProps) {
  return (
    <header className="section-heading section-heading--inline nodes-hero">
      <div>
        <p className="section-heading__eyebrow">节点</p>
        <h2 className="section-heading__title">节点列表</h2>
        <p className="section-heading__description">
          按健康风险、接入状态和最近运行事实管理服务器节点。
        </p>
      </div>
      <div className="nodes-hero__aside" aria-label="节点库存摘要">
        <div className="nodes-hero__stats">
          <Link
            className="nodes-hero-stat nodes-hero-stat--normal"
            to="/nodes"
            aria-label={`全部节点：${totalNodeCount}`}
          >
            <span>全部</span>
            <strong>
              <MonoDigits>{totalNodeCount}</MonoDigits>
            </strong>
          </Link>
          <button
            type="button"
            className="nodes-hero-stat nodes-hero-stat--alert"
            aria-label={`异常节点：${abnormalNodeCount}`}
            onClick={onAbnormalClick}
          >
            <span>异常</span>
            <strong>
              <MonoDigits>{abnormalNodeCount}</MonoDigits>
            </strong>
          </button>
          <button
            type="button"
            className="nodes-hero-stat nodes-hero-stat--notice"
            aria-label={`待接入节点：${pendingOnboardingNodeCount}`}
            onClick={onOnboardingClick}
          >
            <span>待接入</span>
            <strong>
              <MonoDigits>{pendingOnboardingNodeCount}</MonoDigits>
            </strong>
          </button>
          <button
            type="button"
            className="nodes-hero-stat nodes-hero-stat--maintenance"
            aria-label={`维护或暂停节点：${maintenanceOrPausedNodeCount}`}
            onClick={onRuntimeAttentionClick}
          >
            <span>维护/暂停</span>
            <strong>
              <MonoDigits>{maintenanceOrPausedNodeCount}</MonoDigits>
            </strong>
          </button>
        </div>
        <Button variant="primary" size="md" onClick={onCreateClick}>
          新建节点
        </Button>
      </div>
    </header>
  )
}
