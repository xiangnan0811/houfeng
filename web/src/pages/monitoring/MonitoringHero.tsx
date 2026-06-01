import { MonoDigits } from '../../components/atoms'

type MonitoringHeroProps = {
  totalMonitoringInstanceCount: number
  abnormalMonitoringInstanceCount: number
  pendingOnboardingMonitoringInstanceCount: number
  maintenanceOrPausedMonitoringInstanceCount: number
  onAbnormalClick: () => void
  onOnboardingClick: () => void
  onRuntimeAttentionClick: () => void
  onCreateClick: () => void
}

export function MonitoringHero({
  totalMonitoringInstanceCount,
  abnormalMonitoringInstanceCount,
  pendingOnboardingMonitoringInstanceCount,
  maintenanceOrPausedMonitoringInstanceCount,
  onAbnormalClick,
  onOnboardingClick,
  onRuntimeAttentionClick,
  onCreateClick,
}: MonitoringHeroProps) {
  return (
    <>
      <div className="page-header animate-in">
        <div>
          <h1 className="page-title">监控</h1>
          <p className="page-sub">观察 agent 接入后的监控实例、心跳、主机性能与运行控制。</p>
        </div>
        <div className="header-actions">
          <button type="button" className="btn md primary" onClick={onCreateClick}>
            接入监控实例
          </button>
        </div>
      </div>
      <div className="hero-stats animate-in">
        <button type="button" className="hero-stat" onClick={onAbnormalClick}>
          <div className="hs-label">全部监控实例</div>
          <div className="hs-value"><MonoDigits>{totalMonitoringInstanceCount}</MonoDigits></div>
        </button>
        <button type="button" className="hero-stat" onClick={onAbnormalClick}>
          <div className="hs-label">异常</div>
          <div className={`hs-value${abnormalMonitoringInstanceCount > 0 ? ' danger' : ''}`}>
            <MonoDigits>{abnormalMonitoringInstanceCount}</MonoDigits>
          </div>
        </button>
        <button type="button" className="hero-stat" onClick={onOnboardingClick}>
          <div className="hs-label">待接入</div>
          <div className={`hs-value${pendingOnboardingMonitoringInstanceCount > 0 ? ' warn' : ''}`}>
            <MonoDigits>{pendingOnboardingMonitoringInstanceCount}</MonoDigits>
          </div>
        </button>
        <button type="button" className="hero-stat" onClick={onRuntimeAttentionClick}>
          <div className="hs-label">维护/暂停</div>
          <div className={`hs-value${maintenanceOrPausedMonitoringInstanceCount > 0 ? ' muted' : ''}`}>
            <MonoDigits>{maintenanceOrPausedMonitoringInstanceCount}</MonoDigits>
          </div>
        </button>
      </div>
    </>
  )
}
