import { Toggle } from '../../components/atoms'
import type { SettingsIncidentDefaultsForm } from './types'

type IncidentDefaultsSectionProps = {
  value: SettingsIncidentDefaultsForm
  onChange: (next: SettingsIncidentDefaultsForm) => void
}

export function IncidentDefaultsSection({ value, onChange }: IncidentDefaultsSectionProps) {
  function update<K extends keyof SettingsIncidentDefaultsForm>(
    field: K,
    nextValue: SettingsIncidentDefaultsForm[K],
  ) {
    onChange({ ...value, [field]: nextValue })
  }

  return (
    <>
      <div className="ss-title">异常判定阈值</div>
      <div className="ss-desc">触发异常的资源使用率阈值与通知策略</div>
      <div className="settings-row-group settings-row-group--3">
        <div className="settings-row">
          <span className="sr-label">心跳间隔</span>
          <span className="sr-value">
            <input className="input input--compact" aria-label="心跳间隔秒数" inputMode="numeric" value={value.heartbeatIntervalSeconds} onChange={(e) => update('heartbeatIntervalSeconds', e.target.value)} /> s
          </span>
        </div>
        <div className="settings-row">
          <span className="sr-label">失联判定阈值</span>
          <span className="sr-value">
            <input className="input input--compact" aria-label="失联判定阈值" inputMode="numeric" value={value.staleThresholdIntervals} onChange={(e) => update('staleThresholdIntervals', e.target.value)} /> 次
          </span>
        </div>
        <div className="settings-row">
          <span className="sr-label">扫描间隔</span>
          <span className="sr-value">
            <input className="input input--compact" aria-label="扫描间隔秒数" inputMode="numeric" value={value.sweepIntervalSeconds} onChange={(e) => update('sweepIntervalSeconds', e.target.value)} /> s
          </span>
        </div>
      </div>
      <div className="settings-row">
        <span className="sr-label">通知触发</span>
        <span className="sr-value settings-toggles">
          <label className="settings-toggle-item"><Toggle label="开始" checked={value.notifyOnStarted} onChange={(c) => update('notifyOnStarted', c)} /><span>开始</span></label>
          <label className="settings-toggle-item"><Toggle label="升级" checked={value.notifyOnEscalated} onChange={(c) => update('notifyOnEscalated', c)} /><span>升级</span></label>
          <label className="settings-toggle-item"><Toggle label="恢复" checked={value.notifyOnRecovered} onChange={(c) => update('notifyOnRecovered', c)} /><span>恢复</span></label>
        </span>
      </div>
      <div className="settings-row-group settings-row-group--2">
        <div className="settings-row">
          <span className="sr-label">CPU 关注 / 告警 / 严重</span>
          <span className="sr-value">
            <input className="input input--compact" aria-label="CPU 关注阈值" inputMode="numeric" value={value.cpuWarningPct} onChange={(e) => update('cpuWarningPct', e.target.value)} />
            {' / '}
            <input className="input input--compact" aria-label="CPU 告警阈值" inputMode="numeric" value={value.cpuAlertPct} onChange={(e) => update('cpuAlertPct', e.target.value)} />
            {' / '}
            <input className="input input--compact" aria-label="CPU 严重阈值" inputMode="numeric" value={value.cpuCriticalPct} onChange={(e) => update('cpuCriticalPct', e.target.value)} />
            %
          </span>
        </div>
        <div className="settings-row">
          <span className="sr-label">内存 关注 / 告警 / 严重</span>
          <span className="sr-value">
            <input className="input input--compact" aria-label="内存 关注阈值" inputMode="numeric" value={value.memWarningPct} onChange={(e) => update('memWarningPct', e.target.value)} />
            {' / '}
            <input className="input input--compact" aria-label="内存 告警阈值" inputMode="numeric" value={value.memAlertPct} onChange={(e) => update('memAlertPct', e.target.value)} />
            {' / '}
            <input className="input input--compact" aria-label="内存 严重阈值" inputMode="numeric" value={value.memCriticalPct} onChange={(e) => update('memCriticalPct', e.target.value)} />
            %
          </span>
        </div>
        <div className="settings-row">
          <span className="sr-label">磁盘 关注 / 告警 / 严重</span>
          <span className="sr-value">
            <input className="input input--compact" aria-label="磁盘 关注阈值" inputMode="numeric" value={value.diskWarningPct} onChange={(e) => update('diskWarningPct', e.target.value)} />
            {' / '}
            <input className="input input--compact" aria-label="磁盘 告警阈值" inputMode="numeric" value={value.diskAlertPct} onChange={(e) => update('diskAlertPct', e.target.value)} />
            {' / '}
            <input className="input input--compact" aria-label="磁盘 严重阈值" inputMode="numeric" value={value.diskCriticalPct} onChange={(e) => update('diskCriticalPct', e.target.value)} />
            %
          </span>
        </div>
        <div className="settings-row">
          <span className="sr-label">Inode 关注 / 告警 / 严重</span>
          <span className="sr-value">
            <input className="input input--compact" aria-label="Inode 关注阈值" inputMode="numeric" value={value.inodeWarningPct} onChange={(e) => update('inodeWarningPct', e.target.value)} />
            {' / '}
            <input className="input input--compact" aria-label="Inode 告警阈值" inputMode="numeric" value={value.inodeAlertPct} onChange={(e) => update('inodeAlertPct', e.target.value)} />
            {' / '}
            <input className="input input--compact" aria-label="Inode 严重阈值" inputMode="numeric" value={value.inodeCriticalPct} onChange={(e) => update('inodeCriticalPct', e.target.value)} />
            %
          </span>
        </div>
      </div>
      <div className="settings-row-group settings-row-group--2">
        <div className="settings-row">
          <span className="sr-label">IOWait 关注 / 严重</span>
          <span className="sr-value">
            <input className="input input--compact" aria-label="IOWait 关注阈值" inputMode="numeric" value={value.iowaitWarningPct} onChange={(e) => update('iowaitWarningPct', e.target.value)} />
            {' / '}
            <input className="input input--compact" aria-label="IOWait 严重阈值" inputMode="numeric" value={value.iowaitCriticalPct} onChange={(e) => update('iowaitCriticalPct', e.target.value)} />
            %
          </span>
        </div>
        <div className="settings-row">
          <span className="sr-label">Load5 关注 / 严重</span>
          <span className="sr-value">
            <input className="input input--compact" aria-label="Load5 关注阈值" inputMode="numeric" value={value.load5Warning} onChange={(e) => update('load5Warning', e.target.value)} />
            {' / '}
            <input className="input input--compact" aria-label="Load5 严重阈值" inputMode="numeric" value={value.load5Critical} onChange={(e) => update('load5Critical', e.target.value)} />
          </span>
        </div>
      </div>
    </>
  )
}
