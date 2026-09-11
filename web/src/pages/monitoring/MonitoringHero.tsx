import { Link } from 'react-router-dom'

import { MonoDigits } from '../../components/atoms'

type MonitoringHeroProps = {
  abnormalMonitoringInstanceCount: number
  pendingOnboardingMonitoringInstanceCount: number
  maintenanceOrPausedMonitoringInstanceCount: number
  onAbnormalClick: () => void
  onOnboardingClick: () => void
  onRuntimeAttentionClick: () => void
}

export function MonitoringHero({
  abnormalMonitoringInstanceCount,
  pendingOnboardingMonitoringInstanceCount,
  maintenanceOrPausedMonitoringInstanceCount,
  onAbnormalClick,
  onOnboardingClick,
  onRuntimeAttentionClick,
}: MonitoringHeroProps) {
  return (
    <header className="page__head">
      <h1 className="page__title">监控</h1>
      <div className="page__actions">
        <button
          type="button"
          className="btn sm ghost"
          onClick={onAbnormalClick}
          disabled={abnormalMonitoringInstanceCount === 0}
        >
          异常 <MonoDigits>{abnormalMonitoringInstanceCount}</MonoDigits>
        </button>
        <button
          type="button"
          className="btn sm ghost"
          onClick={onOnboardingClick}
          disabled={pendingOnboardingMonitoringInstanceCount === 0}
        >
          待接入 <MonoDigits>{pendingOnboardingMonitoringInstanceCount}</MonoDigits>
        </button>
        <button
          type="button"
          className="btn sm ghost"
          onClick={onRuntimeAttentionClick}
          disabled={maintenanceOrPausedMonitoringInstanceCount === 0}
        >
          维护/暂停 <MonoDigits>{maintenanceOrPausedMonitoringInstanceCount}</MonoDigits>
        </button>
        <Link className="btn sm primary" to="/vps?view=unlinked">
          从未关联 VPS 接入
        </Link>
      </div>
    </header>
  )
}
