import { useId } from 'react'

import { SegmentedControl, Toggle } from '../../components/atoms'
import type { ConfigSource, IncidentSweepMode } from '../../lib/types'
import type { SettingsIncidentDefaultsForm } from './types'

type IncidentDefaultsSectionProps = {
  value: SettingsIncidentDefaultsForm
  sweepMode: IncidentSweepMode
  sweepSource: ConfigSource
  deploymentSeconds: number
  onChange: (next: SettingsIncidentDefaultsForm) => void
  onSweepModeChange: (mode: IncidentSweepMode) => void
}

function sourceLabel(source: ConfigSource): string {
  if (source === 'deployment') return '部署配置'
  if (source === 'database') return '页面设置'
  return '程序默认'
}

export function IncidentDefaultsSection({
  value,
  sweepMode,
  sweepSource,
  deploymentSeconds,
  onChange,
  onSweepModeChange,
}: IncidentDefaultsSectionProps) {
  const sweepHintId = useId()
  const sweepSeconds = sweepMode === 'inherit' ? String(deploymentSeconds) : value.sweepIntervalSeconds

  function update<K extends keyof SettingsIncidentDefaultsForm>(
    field: K,
    nextValue: SettingsIncidentDefaultsForm[K],
  ) {
    onChange({ ...value, [field]: nextValue })
  }

  return (
    <>
      <div className="ss-title">异常判定阈值</div>
      <div className="ss-desc">
        <span>首次达到 N 次遗漏周期时判定失联；默认 N = 12，按 5 秒心跳约 60 秒。</span>
        {' '}
        <span>达到 2N 时升级为告警，达到 4N 时升级为严重；恢复需连续 3 次非回填实时心跳。</span>
      </div>
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
            <input
              className="input input--compact"
              aria-label="扫描间隔秒数"
              aria-describedby={sweepHintId}
              aria-readonly={sweepMode === 'inherit'}
              readOnly={sweepMode === 'inherit'}
              inputMode="numeric"
              value={sweepSeconds}
              onChange={(event) => {
                if (sweepMode === 'inherit') return
                update('sweepIntervalSeconds', event.target.value)
              }}
            /> s
          </span>
        </div>
      </div>
      <div className="settings-row">
        <span className="sr-label">扫描间隔配置</span>
        <span className="sr-value settings-mode-control">
          <SegmentedControl
            label="扫描间隔配置方式"
            value={sweepMode}
            onChange={onSweepModeChange}
            items={[
              { value: 'inherit', label: '继承部署' },
              { value: 'override', label: '页面覆盖' },
            ]}
          />
          <span>当前来源：{sourceLabel(sweepSource)}</span>
          {sweepMode === 'override' ? (
            <button type="button" className="btn sm secondary" onClick={() => onSweepModeChange('inherit')}>
              恢复部署配置
            </button>
          ) : null}
        </span>
      </div>
      <p id={sweepHintId} className="telegram-settings__hint">
        {sweepMode === 'inherit'
          ? '当前显示部署间隔。要修改秒数，请先选择页面覆盖。'
          : '保存后按这个秒数扫描，不再跟随部署间隔。'}
      </p>
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
