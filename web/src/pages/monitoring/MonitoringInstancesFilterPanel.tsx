import { Toggle } from '../../components/atoms/Toggle'
import {
  MONITORING_INSTANCE_HEALTH_STATUS_FILTER_OPTIONS,
  MONITORING_INSTANCE_LIFECYCLE_FILTER_OPTIONS,
  MONITORING_INSTANCE_RUN_STATUS_FILTER_OPTIONS,
} from './monitoringHelpers'
import type { MonitoringInstanceFilterOption, MonitoringInstanceFilterState } from './types'

type MonitoringInstancesFilterPanelProps = {
  hasActiveFilters: boolean
  filterState: MonitoringInstanceFilterState
  groupOptions: MonitoringInstanceFilterOption[]
  regionOptions: MonitoringInstanceFilterOption[]
  cityOptions: MonitoringInstanceFilterOption[]
  providerOptions: MonitoringInstanceFilterOption[]
  labelOptions: MonitoringInstanceFilterOption[]
  onClearAll: () => void
  onSingleFilterChange: (
    key: 'group' | 'region' | 'city' | 'provider' | 'lifecycle' | 'run_status' | 'health',
    value: string | null,
  ) => void
  onMultiFilterChange: (key: 'labels', values: string[]) => void
  onAbnormalFilterChange: (checked: boolean) => void
  onOnboardingFilterChange: (checked: boolean) => void
}

export function MonitoringInstancesFilterPanel({
  hasActiveFilters,
  filterState,
  groupOptions,
  regionOptions,
  cityOptions,
  providerOptions,
  labelOptions,
  onClearAll,
  onSingleFilterChange,
  onMultiFilterChange,
  onAbnormalFilterChange,
  onOnboardingFilterChange,
}: MonitoringInstancesFilterPanelProps) {
  return (
    <div className="filter-bar list-filter-panel">
      <div className="filter-bar__controls">
        <div className="filter-bar__controls-row">
          <label className="filter-select">
            <span className="filter-select__label">Group</span>
            <select
              className="filter-select__control"
              value={filterState.group ?? ''}
              onChange={(e) => onSingleFilterChange('group', e.target.value || null)}
            >
              <option value="">全部</option>
              {groupOptions.map((o) => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </label>
          <label className="filter-select">
            <span className="filter-select__label">地区</span>
            <select
              className="filter-select__control"
              value={filterState.region ?? ''}
              onChange={(e) => onSingleFilterChange('region', e.target.value || null)}
            >
              <option value="">全部</option>
              {regionOptions.map((o) => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </label>
          <label className="filter-select">
            <span className="filter-select__label">城市</span>
            <select
              className="filter-select__control"
              value={filterState.city ?? ''}
              onChange={(e) => onSingleFilterChange('city', e.target.value || null)}
            >
              <option value="">全部</option>
              {cityOptions.map((o) => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </label>
          <label className="filter-select">
            <span className="filter-select__label">供应商</span>
            <select
              className="filter-select__control"
              value={filterState.provider ?? ''}
              onChange={(e) => onSingleFilterChange('provider', e.target.value || null)}
            >
              <option value="">全部</option>
              {providerOptions.map((o) => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </label>
          <label className="filter-select">
            <span className="filter-select__label">接入阶段</span>
            <select
              className="filter-select__control"
              value={filterState.lifecycle ?? ''}
              onChange={(e) => onSingleFilterChange('lifecycle', e.target.value || null)}
            >
              <option value="">全部</option>
              {MONITORING_INSTANCE_LIFECYCLE_FILTER_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </label>
          <label className="filter-select">
            <span className="filter-select__label">运行状态</span>
            <select
              className="filter-select__control"
              value={filterState.runStatus ?? ''}
              onChange={(e) => onSingleFilterChange('run_status', e.target.value || null)}
            >
              <option value="">全部</option>
              {MONITORING_INSTANCE_RUN_STATUS_FILTER_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </label>
          <label className="filter-select">
            <span className="filter-select__label">健康状态</span>
            <select
              className="filter-select__control"
              value={filterState.health ?? ''}
              onChange={(e) => onSingleFilterChange('health', e.target.value || null)}
            >
              <option value="">全部</option>
              {MONITORING_INSTANCE_HEALTH_STATUS_FILTER_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </label>
          <label className="filter-select">
            <span className="filter-select__label">标签</span>
            <select
              className="filter-select__control"
              value=""
              onChange={(e) => {
                const v = e.target.value
                if (v && !filterState.labels.includes(v)) {
                  onMultiFilterChange('labels', [...filterState.labels, v])
                }
                e.target.value = ''
              }}
            >
              <option value="">{filterState.labels.length === 0 ? '全部' : `已选 ${filterState.labels.length}`}</option>
              {labelOptions.map((o) => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </label>
          <div className="filter-toggle">
            <span className="filter-toggle__label">仅看异常</span>
            <Toggle checked={filterState.abnormal} onChange={onAbnormalFilterChange} label="仅看异常" />
          </div>
          <div className="filter-toggle">
            <span className="filter-toggle__label">待接入/绑定待处理</span>
            <Toggle checked={filterState.onboardingPending} onChange={onOnboardingFilterChange} label="待接入/绑定待处理" />
          </div>
        </div>
        {hasActiveFilters && onClearAll ? (
          <button type="button" className="filter-bar__clear" onClick={onClearAll}>清空所有</button>
        ) : null}
      </div>
      {hasActiveFilters ? (
        <div className="filter-bar__chips">
          {filterState.group ? (
            <span className="filter-chip">
              <span className="filter-chip__label">Group: {filterState.group}</span>
              <button type="button" className="filter-chip__remove" aria-label="移除筛选 Group" onClick={() => onSingleFilterChange('group', null)}>×</button>
            </span>
          ) : null}
          {filterState.region ? (
            <span className="filter-chip">
              <span className="filter-chip__label">地区: {filterState.region}</span>
              <button type="button" className="filter-chip__remove" aria-label="移除筛选 地区" onClick={() => onSingleFilterChange('region', null)}>×</button>
            </span>
          ) : null}
          {filterState.city ? (
            <span className="filter-chip">
              <span className="filter-chip__label">城市: {filterState.city}</span>
              <button type="button" className="filter-chip__remove" aria-label="移除筛选 城市" onClick={() => onSingleFilterChange('city', null)}>×</button>
            </span>
          ) : null}
          {filterState.provider ? (
            <span className="filter-chip">
              <span className="filter-chip__label">供应商: {filterState.provider}</span>
              <button type="button" className="filter-chip__remove" aria-label="移除筛选 供应商" onClick={() => onSingleFilterChange('provider', null)}>×</button>
            </span>
          ) : null}
          {filterState.lifecycle ? (
            <span className="filter-chip">
              <span className="filter-chip__label">接入阶段: {filterState.lifecycle}</span>
              <button type="button" className="filter-chip__remove" aria-label="移除筛选 接入阶段" onClick={() => onSingleFilterChange('lifecycle', null)}>×</button>
            </span>
          ) : null}
          {filterState.runStatus ? (
            <span className="filter-chip">
              <span className="filter-chip__label">运行状态: {filterState.runStatus}</span>
              <button type="button" className="filter-chip__remove" aria-label="移除筛选 运行状态" onClick={() => onSingleFilterChange('run_status', null)}>×</button>
            </span>
          ) : null}
          {filterState.health ? (
            <span className="filter-chip">
              <span className="filter-chip__label">健康状态: {filterState.health}</span>
              <button type="button" className="filter-chip__remove" aria-label="移除筛选 健康状态" onClick={() => onSingleFilterChange('health', null)}>×</button>
            </span>
          ) : null}
          {filterState.labels.map((label) => (
            <span key={`label-${label}`} className="filter-chip">
              <span className="filter-chip__label">标签: {label}</span>
              <button type="button" className="filter-chip__remove" aria-label={`移除筛选 标签: ${label}`} onClick={() => onMultiFilterChange('labels', filterState.labels.filter((item) => item !== label))}>×</button>
            </span>
          ))}
          {filterState.abnormal ? (
            <span className="filter-chip">
              <span className="filter-chip__label">仅看异常</span>
              <button type="button" className="filter-chip__remove" aria-label="移除筛选 仅看异常" onClick={() => onAbnormalFilterChange(false)}>×</button>
            </span>
          ) : null}
          {filterState.onboardingPending ? (
            <span className="filter-chip">
              <span className="filter-chip__label">待接入/绑定待处理</span>
              <button type="button" className="filter-chip__remove" aria-label="移除筛选 待接入/绑定待处理" onClick={() => onOnboardingFilterChange(false)}>×</button>
            </span>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}
