import { Link } from 'react-router-dom'

import { MonoDigits, StatCard } from '../../components/atoms'

type MonitoringHeroProps = {
  totalMonitoringInstanceCount: number
  abnormalMonitoringInstanceCount: number
  pendingOnboardingMonitoringInstanceCount: number
  maintenanceOrPausedMonitoringInstanceCount: number
  onAbnormalClick: () => void
  onOnboardingClick: () => void
  onRuntimeAttentionClick: () => void
}

export function MonitoringHero({
  totalMonitoringInstanceCount,
  abnormalMonitoringInstanceCount,
  pendingOnboardingMonitoringInstanceCount,
  maintenanceOrPausedMonitoringInstanceCount,
  onAbnormalClick,
  onOnboardingClick,
  onRuntimeAttentionClick,
}: MonitoringHeroProps) {
  return (
    <>
      <div className="page-header animate-in">
        <div>
          <div className="page-eyebrow">观测 · MONITORING</div>
          <h1 className="page-title">监控</h1>
          <p className="page-sub">观察 agent 接入后的监控实例、心跳、主机性能与运行控制。</p>
        </div>
        <div className="header-actions">
          <Link className="btn md primary" to="/vps?view=unlinked">
            从未关联 VPS 接入 agent
          </Link>
        </div>
      </div>
      <div className="stat-grid">
        <StatCard value={<MonoDigits>{totalMonitoringInstanceCount}</MonoDigits>} label="全部监控实例" onClick={onAbnormalClick} />
        <StatCard value={<MonoDigits>{abnormalMonitoringInstanceCount}</MonoDigits>} label="异常" tone={abnormalMonitoringInstanceCount > 0 ? 'err' : 'normal'} onClick={onAbnormalClick} />
        <StatCard value={<MonoDigits>{pendingOnboardingMonitoringInstanceCount}</MonoDigits>} label="待接入" tone={pendingOnboardingMonitoringInstanceCount > 0 ? 'warn' : 'normal'} onClick={onOnboardingClick} />
        <StatCard value={<MonoDigits>{maintenanceOrPausedMonitoringInstanceCount}</MonoDigits>} label="维护/暂停" tone={maintenanceOrPausedMonitoringInstanceCount > 0 ? 'warn' : 'normal'} onClick={onRuntimeAttentionClick} />
      </div>
    </>
  )
}
