import { Link } from 'react-router-dom'

import { FilterChip } from '../../../components/filters'
import { PageState as PageStateView } from '../../../components/PageState'
import { TabPanel, Tabs } from '../../../components/atoms'
import type {
  AssetDecisionGroupSummary,
} from '../../../lib/types'
import type {
  MainWorkbenchView,
  PortfolioState,
  AssetDecisionPortfolioLead,
  ContextFilterChip,
  ContextFilterKey,
} from '../types'
import {
  RENEWAL_WINDOWS,
  VIEW_LABELS,
  WORKBENCH_TABS,
} from '../constants'
import {
  renderCompactRiskChips,
} from '../renderHelpers'
import {
  groupPressureLabel,
  compactGroupJudgement,
} from '../formatters'
import { Badge } from '../../../components/atoms'

type PortfolioWorkbenchProps = {
  portfolioView: MainWorkbenchView
  renewalWindow: number
  portfolioState: PortfolioState
  portfolioLead: AssetDecisionPortfolioLead
  contextFilterChips: ContextFilterChip[]
  closedLoopPartialErrors: string[]
  closedLoopAnomalies: number
  partialErrorCount: number
  onSetWorkbenchView: (view: MainWorkbenchView) => void
  onChangeRenewalWindow: (value: string) => void
  onOpenGroup: (groupID: string) => void
  onOpenPortfolioLead: () => void
  onClearContextFilter: (key: ContextFilterKey) => void
  onClearAllContextFilters: () => void
}

export function PortfolioWorkbench({
  portfolioView,
  renewalWindow,
  portfolioState,
  portfolioLead,
  contextFilterChips,
  closedLoopPartialErrors,
  closedLoopAnomalies,
  partialErrorCount,
  onSetWorkbenchView,
  onChangeRenewalWindow,
  onOpenGroup,
  onOpenPortfolioLead,
  onClearContextFilter,
  onClearAllContextFilters,
}: PortfolioWorkbenchProps) {
  const overview = portfolioState.overview

  const workbenchTabs = WORKBENCH_TABS.map((item) => {
    const count =
      item.value === 'needs_decision' ? overview?.needs_decision_count
        : item.value === 'renewal' ? overview?.renewal_group_count
          : item.value === 'region' ? overview?.region_group_count
            : item.value === 'provider' ? overview?.provider_group_count
              : item.value === 'cost' ? overview?.cost_group_count
                : item.value === 'evidence' ? overview?.evidence_group_count
                  : undefined
    return {
      ...item,
      ...(count === undefined ? {} : { count }),
    }
  })

  function renderDecisionGroupCards(groups: AssetDecisionGroupSummary[]) {
    return (
      <div className="asset-decision-group-cards" aria-label="决策组扫描列表">
        {groups.map((group, index) => {
          const assessment = group.evidence_assessment
          const hasOperationalRisk = group.cancellation_attention_count > 0
            || group.active_incident_count > 0
            || group.abnormal_monitoring_count > 0
            || group.evidence_chips.some((chip) => chip.tone === 'critical' || chip.tone === 'alert')
          const tone = hasOperationalRisk || assessment.gap_signal_count > 0 ? 'alert' : 'normal'
          return (
            <article key={group.group_id} className={`asset-decision-group-card asset-decision-group-card--${tone}`}>
              <div className="asset-decision-group-card__rank">
                <strong>P{index + 1}</strong>
                <span>{VIEW_LABELS[group.view]}</span>
              </div>
              <div className="asset-decision-group-card__body">
                <div className="asset-decision-group-card__head">
                  <div>
                    <strong>{group.title}</strong>
                    <span>{group.scope_label}</span>
                  </div>
                  <span className="asset-decision-chip-row">
                    <Badge variant="info" tone={tone}>
                      {groupPressureLabel(group)}
                    </Badge>
                  </span>
                </div>

                <div className="asset-decision-group-card__evidence">
                  <strong>{compactGroupJudgement(group)}</strong>
                  <span className="asset-decision-chip-row">
                    {renderCompactRiskChips(group.evidence_chips, assessment)}
                  </span>
                </div>
              </div>
              <div className="asset-decision-group-card__actions">
                <button className="btn md primary" type="button" onClick={() => onOpenGroup(group.group_id)}>
                  查看组
                </button>
              </div>
            </article>
          )
        })}
      </div>
    )
  }

  return (
    <>
      <section className={`asset-decision-focus asset-decision-command-summary asset-decision-command-summary--${portfolioLead.tone} animate-in d1`} aria-label="资产组合决策当前判断">
        <div className="asset-decision-command-summary__lead">
          <span className="section-heading__eyebrow">{portfolioLead.eyebrow}</span>
          <h2 className="asset-decision-command-summary__title">{portfolioLead.title}</h2>
          <p className="asset-decision-command-summary__summary">{portfolioLead.summary}</p>
          {portfolioLead.kind === 'work' && portfolioLead.actionLabel && (
            <div className="asset-decision-command-summary__actions">
              <button className="btn lg primary" type="button" onClick={onOpenPortfolioLead}>
                {portfolioLead.actionLabel}
              </button>
              <Link className="btn md secondary" to={`/asset-decisions?view=evidence&renew_within_days=${renewalWindow}&scenario=evidence_cleanup`}>
                资料缺口
              </Link>
            </div>
          )}
        </div>
        <div className="asset-decision-command-summary__facts" aria-label="资产组合决策当前事实">
          <div className="asset-decision-focus__item asset-decision-focus__item--notice">
            <span>组合组数</span>
            <strong>{portfolioState.overviewLoading ? '...' : overview?.group_count ?? portfolioState.groups.length}</strong>
          </div>
          <div className="asset-decision-focus__item asset-decision-focus__item--alert">
            <span>续费组</span>
            <strong>{portfolioState.overviewLoading ? '...' : overview?.renewal_group_count ?? 0}</strong>
          </div>
          <div className="asset-decision-focus__item asset-decision-focus__item--critical">
            <span>闭环异常</span>
            <strong>{closedLoopAnomalies}</strong>
            {(closedLoopAnomalies > 0 || partialErrorCount > 0) && <small>{portfolioLead.riskLabel}</small>}
          </div>
          <div className="asset-decision-focus__item asset-decision-focus__item--normal">
            <span>证据状态</span>
            <strong>{overview ? '已聚合' : '等待'}</strong>
          </div>
        </div>
      </section>

      {closedLoopPartialErrors.length > 0 && (
        <div className="inline-alert warn" role="status">
          {closedLoopPartialErrors.join('、')}暂不可用，当前只展示已成功加载的事实。
        </div>
      )}

      <div className="page-panel--scan page-panel--scan--single animate-in d2">
        <section className="page-panel asset-decision-command">
          <div className="asset-decision-board__header">
            <div>
              <p className="section-heading__eyebrow">组合扫描</p>
              <h2 className="section-heading__title">决策组扫描</h2>
            </div>
            <div className="asset-decision-board__tools">
              <div className="asset-decision-window">
                <span>续费窗口</span>
                <select
                  className="input filter-select--inline"
                  aria-label="续费窗口"
                  value={String(renewalWindow)}
                  onChange={(event) => onChangeRenewalWindow(event.target.value)}
                >
                  {RENEWAL_WINDOWS.map((value) => (
                    <option key={value} value={value}>未来 {value} 天</option>
                  ))}
                </select>
              </div>
              <p className="section-count">{portfolioState.overviewError ? '组合概览不可用' : `当前显示 ${portfolioState.groups.length} 个组`}</p>
            </div>
          </div>
          <div className="asset-decision-tabs">
            <Tabs
              label="资产决策组合视图"
              idBase="asset-portfolio-workbench"
              items={workbenchTabs}
              value={portfolioView}
              onChange={onSetWorkbenchView}
              variant="pill"
            />
            {contextFilterChips.length > 0 && (
              <div className="asset-decision-filter-chips" aria-label="资产决策上下文筛选">
                {contextFilterChips.map((chip) => (
                  <FilterChip
                    key={chip.key}
                    label={`${chip.label}: ${chip.value}`}
                    onRemove={() => onClearContextFilter(chip.key)}
                  />
                ))}
                <button className="filter-clear" type="button" onClick={onClearAllContextFilters}>清除上下文</button>
              </div>
            )}
          </div>

          <TabPanel
            idBase="asset-portfolio-workbench"
            value={portfolioView}
            className="asset-decision-tab-panel"
          >
            {portfolioState.groupsLoading ? (
              <PageStateView
                kind="loading"
                title="正在加载决策组…"
                surface="empty"
                compact
              />
            ) : portfolioState.groupsError ? (
              <PageStateView
                kind="error"
                title="决策组不可用"
                surface="empty"
                compact
              />
            ) : portfolioState.groups.length === 0 ? (
              <PageStateView
                kind="empty"
                title="当前视图暂无决策组"
                action={portfolioLead.kind === 'stable' ? undefined : <button className="btn sm secondary" onClick={() => onSetWorkbenchView('needs_decision')}>查看需要决策</button>}
                surface="empty"
                compact
              />
            ) : (
              renderDecisionGroupCards(portfolioState.groups)
            )}
          </TabPanel>
        </section>
      </div>
    </>
  )
}
