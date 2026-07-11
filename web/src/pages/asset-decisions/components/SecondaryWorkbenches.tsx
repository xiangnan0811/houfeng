import { Link } from 'react-router-dom'

import { AssetDecisionRenewalTable } from '../../../components/AssetDecisionRenewalTable'
import {
  Badge,
  DataTable,
  type DataTableColumn,
  MonoDigits,
  TabPanel,
  Tabs,
} from '../../../components/atoms'
import { PageState as PageStateView } from '../../../components/PageState'
import {
  formatDate,
  formatMoney,
  formatOptional,
} from '../../../lib/format'
import type {
  AssetDecisionManualGroupSummary,
  AssetDecisionRecordSummary,
  SubscriptionRecord,
  VPSAssetRecord,
} from '../../../lib/types'
import {
  RenewalBadge,
} from '../../assetPageBadges'
import {
  daysUntilDate,
  vpsLocationLabel,
} from '../../assetPageUtils'
import { AssetDecisionSecondaryNav } from '../AssetDecisionSecondaryNav'
import { vpsDetailPath, vpsWorkbenchPath } from '../paths'
import type {
  DecisionQueueView,
  DecisionQueueItem,
  QueueState,
  ManualGroupsState,
  RecordsState,
  ScenarioTemplatesState,
  SecondaryWorkbench,
  AssetDecisionSecondaryNavItem,
} from '../types'
import {
  MANUAL_GROUP_STATUS_LABELS,
  MANUAL_GROUP_SCENARIO_LABELS,
  RECORD_STATUS_LABELS,
  SCENARIO_TEMPLATE_STATUS_LABELS,
  VIEW_LABELS,
} from '../constants'
import {
  manualGroupStatusTone,
  recordStatusTone,
  scenarioTemplateStatusTone,
  baseMoney,
  recordFollowupDoneCount,
} from '../formatters'

type SecondaryWorkbenchesProps = {
  secondaryWorkbench: SecondaryWorkbench | null
  secondaryNavItems: AssetDecisionSecondaryNavItem[]
  queueView: DecisionQueueView
  renewalWindow: number
  queueState: QueueState
  visibleDecisionQueue: DecisionQueueItem[]
  totalDecisionQueue: number
  renewalDueQueueCount: number
  missingSubscriptionCount: number
  unlinkedCount: number
  cancellationAttentionCount: number
  manualGroupsState: ManualGroupsState
  templatesState: ScenarioTemplatesState
  recordsState: RecordsState
  vpsByID: Map<string, VPSAssetRecord>
  onSetSelectedSecondaryWorkbench: (workbench: SecondaryWorkbench | null) => void
  onSetQueueView: (view: DecisionQueueView) => void
  onSelectVPS: (vps: VPSAssetRecord) => void
  onNavigateToVPS: (vps: VPSAssetRecord) => void
  onNavigateToVPSSubscription: (vpsID: string) => void
  onOpenManualGroup: (manualGroupID: string) => void
  onOpenTemplate: (templateID: string) => void
  onOpenRecord: (recordID: string) => void
  hasCancellationAttention: (row: DecisionQueueItem) => boolean
  subscriptionCostAttention: (subscription: SubscriptionRecord | null) => boolean
}

export function SecondaryWorkbenches({
  secondaryWorkbench,
  secondaryNavItems,
  queueView,
  renewalWindow,
  queueState,
  visibleDecisionQueue,
  totalDecisionQueue,
  renewalDueQueueCount,
  missingSubscriptionCount,
  unlinkedCount,
  cancellationAttentionCount,
  manualGroupsState,
  templatesState,
  recordsState,
  vpsByID,
  onSetSelectedSecondaryWorkbench,
  onSetQueueView,
  onSelectVPS,
  onNavigateToVPS,
  onNavigateToVPSSubscription,
  onOpenManualGroup,
  onOpenTemplate,
  onOpenRecord,
  hasCancellationAttention,
  subscriptionCostAttention,
}: SecondaryWorkbenchesProps) {
  const manualGroupColumns: DataTableColumn<AssetDecisionManualGroupSummary>[] = [
    {
      key: 'group',
      label: '组合',
      width: '320px',
      render: (group) => (
        <div className="asset-table__identity">
          <strong>{group.title}</strong>
          <span>{MANUAL_GROUP_SCENARIO_LABELS[group.scenario]}</span>
        </div>
      ),
    },
    {
      key: 'status',
      label: '状态',
      width: '140px',
      render: (group) => (
        <Badge variant="state" tone={manualGroupStatusTone(group.status)}>
          {MANUAL_GROUP_STATUS_LABELS[group.status]}
        </Badge>
      ),
    },
    {
      key: 'members',
      label: '成员数',
      width: '120px',
      render: (group) => (
        <span><MonoDigits>{group.member_count}</MonoDigits> 台</span>
      ),
    },
    {
      key: 'actions',
      label: '操作',
      align: 'right',
      width: '128px',
      render: (group) => (
        <button className="btn sm primary" type="button" onClick={() => onOpenManualGroup(group.manual_group_id)}>
          查看
        </button>
      ),
    },
  ]

  const recordColumns: DataTableColumn<AssetDecisionRecordSummary>[] = [
    {
      key: 'record',
      label: '标题',
      width: '320px',
      render: (record) => (
        <div className="asset-table__identity">
          <strong>{record.title}</strong>
          <span>{VIEW_LABELS[record.source_view]}</span>
        </div>
      ),
    },
    {
      key: 'status',
      label: '状态',
      width: '140px',
      render: (record) => (
        <Badge variant="state" tone={recordStatusTone(record.status)}>
          {RECORD_STATUS_LABELS[record.status]}
        </Badge>
      ),
    },
    {
      key: 'progress',
      label: '进度',
      width: '160px',
      render: (record) => (
        <span>
          <MonoDigits>{recordFollowupDoneCount(record)}</MonoDigits>/<MonoDigits>{record.member_count}</MonoDigits>
        </span>
      ),
    },
    {
      key: 'actions',
      label: '操作',
      align: 'right',
      width: '112px',
      render: (record) => (
        <button className="btn sm primary" type="button" onClick={() => onOpenRecord(record.record_id)}>
          查看
        </button>
      ),
    },
  ]

  const queueTabs = [
    { value: 'all', label: '全部', count: totalDecisionQueue },
    { value: 'unreviewed', label: '待评估', count: queueState.unreviewed.length },
    { value: 'renewal', label: `${renewalWindow}天续费`, count: renewalDueQueueCount },
    { value: 'migrate', label: '迁移', count: queueState.migrate.length },
    { value: 'cancel', label: '取消', count: queueState.cancel.length },
    { value: 'cancellation_attention', label: '取消联动', count: cancellationAttentionCount },
    { value: 'unlinked', label: '未关联', count: unlinkedCount },
    { value: 'missing_subscription', label: '缺订阅', count: missingSubscriptionCount },
  ] satisfies Array<{ value: DecisionQueueView; label: string; count: number }>

  return (
    <>
      <div className="asset-decision-topology animate-in d2">
        <AssetDecisionSecondaryNav
          items={secondaryNavItems}
          active={secondaryWorkbench}
          onOpen={onSetSelectedSecondaryWorkbench}
        />
      </div>

      {secondaryWorkbench === 'scenarios' && (
        <section className="page-panel asset-decision-scenario-records animate-in d3">
          <div className="asset-decision-board__header">
            <div>
              <p className="section-heading__eyebrow">场景工作区</p>
              <h2 className="section-heading__title">场景工作区</h2>
            </div>
            <div className="asset-decision-board__tools">
              <span className="section-count">
                模板 {templatesState.loading ? '...' : templatesState.error ? '不可用' : templatesState.templates.length}
              </span>
              <span className="section-count">
                组合 {manualGroupsState.loading ? '...' : manualGroupsState.error ? '不可用' : manualGroupsState.groups.length}
              </span>
            </div>
          </div>

          <div className="asset-decision-scenario-records__grid">
            <section className="asset-decision-scenario-card asset-decision-templates" aria-label="场景模板">
              <div className="asset-decision-scenario-card__head">
                <div>
                  <p className="section-heading__eyebrow">场景模板</p>
                  <h3 className="section-heading__title">场景模板</h3>
                </div>
              </div>
              {templatesState.loading ? (
                <PageStateView kind="loading" title="正在加载场景模板…" surface="empty" compact />
              ) : templatesState.error ? (
                <PageStateView
                  kind="error"
                  title="场景模板不可用"
                  surface="empty"
                  compact
                />
              ) : templatesState.templates.length === 0 ? (
                <PageStateView
                  kind="empty"
                  title="暂无场景模板"
                  surface="empty"
                  compact
                />
              ) : (
                <div className="asset-decision-template-launchers">
                  {templatesState.templates.slice(0, 6).map((template) => (
                    <article key={template.template_id} className="asset-decision-template-launcher">
                      <div>
                        <span className="asset-decision-chip-row">
                          <Badge variant="state" tone={scenarioTemplateStatusTone(template.status)}>
                            {SCENARIO_TEMPLATE_STATUS_LABELS[template.status]}
                          </Badge>
                          {template.builtin && (
                            <Badge variant="info" tone="notice">
                              内置
                            </Badge>
                          )}
                        </span>
                        <strong>{template.title}</strong>
                        <span>{MANUAL_GROUP_SCENARIO_LABELS[template.scenario]} · {template.goal || template.note || '场景启动器'}</span>
                        <small>蓝图成员 <MonoDigits>{template.member_count}</MonoDigits></small>
                      </div>
                      <button className="btn sm secondary" type="button" onClick={() => onOpenTemplate(template.template_id)}>
                        使用模板
                      </button>
                    </article>
                  ))}
                </div>
              )}
            </section>

            <section className="asset-decision-scenario-card asset-decision-manual-groups" aria-label="自定义组合">
              <div className="asset-decision-scenario-card__head">
                <div>
                  <p className="section-heading__eyebrow">自定义组合</p>
                  <h3 className="section-heading__title">自定义组合</h3>
                </div>
              </div>
              {manualGroupsState.loading ? (
                <PageStateView kind="loading" title="正在加载自定义组合…" surface="empty" compact />
              ) : manualGroupsState.error ? (
                <PageStateView
                  kind="error"
                  title="自定义组合不可用"
                  surface="empty"
                  compact
                />
              ) : manualGroupsState.groups.length === 0 ? (
                <PageStateView
                  kind="empty"
                  title="尚未创建自定义组合"
                  surface="empty"
                  compact
                />
              ) : (
                <div className="asset-table-scroll" role="region" aria-label="自定义资产组合" tabIndex={0}>
                  <DataTable
                    className="asset-table asset-decision-manual-groups-table"
                    columns={manualGroupColumns}
                    rows={manualGroupsState.groups}
                    rowKey={(group) => group.manual_group_id}
                    onRowClick={(group) => onOpenManualGroup(group.manual_group_id)}
                  />
                </div>
              )}
            </section>
          </div>
        </section>
      )}

      {secondaryWorkbench === 'records' && (
        <section className="page-panel asset-decision-scenario-card asset-decision-records animate-in d3" aria-label="已保存组合决策">
          <div className="asset-decision-scenario-card__head">
            <div>
              <p className="section-heading__eyebrow">保存记录</p>
              <h3 className="section-heading__title">已保存组合决策</h3>
            </div>
          </div>
          {recordsState.loading ? (
            <PageStateView kind="loading" title="正在加载决策记录…" surface="empty" compact />
          ) : recordsState.error ? (
            <PageStateView
              kind="error"
              title="决策记录不可用"
              surface="empty"
              compact
            />
          ) : recordsState.records.length === 0 ? (
            <PageStateView
              kind="empty"
              title="尚未保存组合决策"
              surface="empty"
              compact
            />
          ) : (
            <div className="asset-table-scroll" role="region" aria-label="已保存组合决策" tabIndex={0}>
              <DataTable
                className="asset-table asset-decision-records-table"
                columns={recordColumns}
                rows={recordsState.records}
                rowKey={(record) => record.record_id}
                onRowClick={(record) => onOpenRecord(record.record_id)}
              />
            </div>
          )}
        </section>
      )}

      {secondaryWorkbench === 'renewals' && (
        <section className="page-panel asset-renewal-evidence animate-in d4">
          <div className="section-heading section-heading--inline">
            <div>
              <p className="section-heading__eyebrow">续费事实</p>
              <h2 className="section-heading__title">续费事实</h2>
            </div>
            <span className={`section-count${queueState.renewals.length > 0 ? ' section-count--warn' : ''}`}>
              {queueState.renewalsLoading ? '...' : queueState.renewalsError ? '不可用' : `${queueState.renewals.length} 条`}
            </span>
          </div>
          <AssetDecisionRenewalTable
            loading={queueState.renewalsLoading}
            error={queueState.renewalsError}
            renewals={queueState.renewals}
            vpsByID={vpsByID}
            renderVPSReference={(subscription, vps) => (
              <Link className="name" to={vpsDetailPath(subscription.vps_id)}>
                {vps?.display_name ?? subscription.vps_id}
              </Link>
            )}
            renderActions={(subscription) => (
              <>
                <Link className="btn-text sm secondary" to={`/asset-decisions?view=renewal&renew_within_days=${renewalWindow}`}>组合判断</Link>
                <Link className="btn-text sm secondary" to={vpsDetailPath(subscription.vps_id)}>VPS 详情</Link>
              </>
            )}
          />
        </section>
      )}

      {secondaryWorkbench === 'single_queue' && (
        <section id="single-vps-queue" className="page-panel asset-decision-single-queue animate-in d5">
          <div className="asset-decision-board__header">
            <div>
              <p className="section-heading__eyebrow">单台辅助</p>
              <h2 className="section-heading__title">单台辅助队列</h2>
            </div>
            <span className="section-count">
              {queueState.queueLoading ? '...' : `${visibleDecisionQueue.length} / ${totalDecisionQueue}`}
            </span>
          </div>
          <div className="asset-decision-tabs">
            <Tabs
              label="单台辅助队列视图"
              idBase="asset-decision-queue"
              items={queueTabs}
              value={queueView}
              onChange={onSetQueueView}
              variant="pill"
            />
          </div>
          <TabPanel
            idBase="asset-decision-queue"
            value={queueView}
            className="asset-decision-tab-panel"
          >
            {queueState.queueLoading ? (
              <PageStateView
                kind="loading"
                title="正在加载单台队列…"
                surface="empty"
                compact
              />
            ) : queueState.queueError ? (
              <PageStateView
                kind="error"
                title="单台队列不可用"
                surface="empty"
                compact
              />
            ) : visibleDecisionQueue.length === 0 ? (
              <PageStateView
                kind="empty"
                title="当前视图暂无待处理 VPS"
                action={
                  <div className="asset-empty-actions">
                    {queueView !== 'all' && (
                      <button className="btn sm secondary" onClick={() => onSetQueueView('all')}>查看全部</button>
                    )}
                    <Link className="btn sm ghost" to="/vps">VPS 库存</Link>
                    <Link className="btn sm ghost" to="/vps?view=missing_subscription">缺订阅 VPS</Link>
                  </div>
                }
                surface="empty"
                compact
              />
            ) : (
              <div className="asset-table-scroll" role="region" aria-label="单台辅助队列" tabIndex={0}>
              <DataTable
                className="asset-table asset-decision-queue-table"
                columns={[
                  {
                    key: 'vps',
                    label: 'VPS',
                    width: '236px',
                    render: (item) => (
                      <div className="asset-table__identity">
                        <strong>{item.vps.display_name}</strong>
                        <span>{formatOptional(item.vps.provider_name)} · {vpsLocationLabel(item.vps)}</span>
                      </div>
                    ),
                  },
                  {
                    key: 'decision',
                    label: '决策',
                    width: '112px',
                    render: (item) => <RenewalBadge value={item.vps.renewal_decision} />,
                  },
                  {
                    key: 'subscription',
                    label: '订阅',
                    width: '176px',
                    render: (item) => {
                      const sub = item.subscription
                      const daysLeft = sub ? daysUntilDate(sub.renew_at) : null
                      return sub ? (
                        <div className="asset-table__stack">
                          <strong>{formatMoney(sub.monthly_price, sub.currency)}/月</strong>
                          <span className={daysLeft != null && daysLeft <= renewalWindow ? 'days-urgent' : 'days-normal'}>
                            {daysLeft != null ? `${daysLeft}天` : formatDate(sub.renew_at)}
                          </span>
                        </div>
                      ) : (
                        <button
                          type="button"
                          className="text-link"
                          onClick={() => onNavigateToVPSSubscription(item.vps.vps_id)}
                        >
                          缺订阅
                        </button>
                      )
                    },
                  },
                  {
                    key: 'cost',
                    label: '成本信号',
                    width: '220px',
                    render: (item) => {
                      const sub = item.subscription
                      return sub ? (
                        <div className="asset-context-cell asset-cost-signal">
                          <span className={sub.exchange_rate_stale ? 'badge badge-warn' : 'badge badge-ok'}>
                            <span className="badge-dot" />{sub.exchange_rate_stale ? '汇率过期' : '成本已换算'}
                          </span>
                          <small>
                            {baseMoney(sub.monthly_price_base, sub.base_currency ?? 'CNY')}/月 · {baseMoney(sub.yearly_price_base, sub.base_currency ?? 'CNY')}/年
                          </small>
                          {subscriptionCostAttention(sub) ? (
                            <span className="asset-context-pill asset-context-pill--attention">
                              汇率过期
                            </span>
                          ) : (
                            <span className="asset-context-pill">成本正常</span>
                          )}
                        </div>
                      ) : (
                        <div className="asset-context-cell asset-cost-signal">
                          <span className="asset-context-pill asset-context-pill--attention">缺订阅成本</span>
                          <small>无法参与续费判断</small>
                        </div>
                      )
                    },
                  },
                  {
                    key: 'monitoring',
                    label: '监控',
                    width: '112px',
                    render: (item) => (
                      item.vps.active_monitoring_instance_link_count > 0 ? (
                        <span><MonoDigits>{item.vps.active_monitoring_instance_link_count}</MonoDigits> 关联</span>
                      ) : (
                        <span className="text-muted">未关联</span>
                      )
                    ),
                  },
                  {
                    key: 'actions',
                    label: '操作',
                    align: 'right',
                    width: '172px',
                    render: (item) => (
                      <div className="asset-decision-member-actions">
                        <button className="btn sm primary" onClick={() => onSelectVPS(item.vps)}>
                          处理
                        </button>
                        {item.vps.renewal_decision === 'cancel' || hasCancellationAttention(item) ? (
                          <Link className="btn sm secondary" to={vpsWorkbenchPath(item.vps.vps_id, 'cancellation')}>
                            取消/退役
                          </Link>
                        ) : null}
                      </div>
                    ),
                  },
                ]}
                rows={visibleDecisionQueue}
                rowKey={(item) => item.vps.vps_id}
                onRowClick={(item) => onNavigateToVPS(item.vps)}
              />
              </div>
            )}
          </TabPanel>
        </section>
      )}
    </>
  )
}
