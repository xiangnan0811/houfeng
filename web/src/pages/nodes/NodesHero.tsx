import { MonoDigits } from '../../components/atoms'

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
    <>
      <div className="page-header animate-in">
        <div>
          <h1 className="page-title">节点观测</h1>
          <p className="page-sub">管理和监控所有节点</p>
        </div>
        <div className="header-actions">
          <button type="button" className="btn md primary" onClick={onCreateClick}>
            新建节点
          </button>
        </div>
      </div>
      <div className="hero-stats animate-in">
        <button type="button" className="hero-stat" onClick={onAbnormalClick}>
          <div className="hs-label">全部节点</div>
          <div className="hs-value"><MonoDigits>{totalNodeCount}</MonoDigits></div>
        </button>
        <button type="button" className="hero-stat" onClick={onAbnormalClick}>
          <div className="hs-label">异常</div>
          <div className={`hs-value${abnormalNodeCount > 0 ? ' danger' : ''}`}>
            <MonoDigits>{abnormalNodeCount}</MonoDigits>
          </div>
        </button>
        <button type="button" className="hero-stat" onClick={onOnboardingClick}>
          <div className="hs-label">待接入</div>
          <div className={`hs-value${pendingOnboardingNodeCount > 0 ? ' warn' : ''}`}>
            <MonoDigits>{pendingOnboardingNodeCount}</MonoDigits>
          </div>
        </button>
        <button type="button" className="hero-stat" onClick={onRuntimeAttentionClick}>
          <div className="hs-label">维护/暂停</div>
          <div className={`hs-value${maintenanceOrPausedNodeCount > 0 ? ' muted' : ''}`}>
            <MonoDigits>{maintenanceOrPausedNodeCount}</MonoDigits>
          </div>
        </button>
      </div>
    </>
  )
}
