import { describe, expect, it } from 'vitest'

import { buildDashboardModel, type DashboardReadyModel } from './dashboardModel'
import {
  remoteError,
  remoteLoading,
  remoteSuccess,
  type RemoteState,
} from './dashboardRemoteState'
import {
  DASHBOARD_FIXTURE_LOADED_AT,
  dashboardOverviewFixture,
  subscriptionOverviewFixture,
  vpsAssetFixture,
} from './dashboardTestFixtures'
import type { DashboardOverview, SubscriptionOverview, VPSAssetRecord } from '../../lib/types'

function readyModel(input: {
  overview?: RemoteState<DashboardOverview>
  vps?: RemoteState<VPSAssetRecord[]>
  subscription?: RemoteState<SubscriptionOverview>
} = {}): DashboardReadyModel {
  const model = buildDashboardModel({
    overview: input.overview ?? remoteSuccess(dashboardOverviewFixture(), DASHBOARD_FIXTURE_LOADED_AT),
    vps: input.vps ?? remoteSuccess([vpsAssetFixture()], DASHBOARD_FIXTURE_LOADED_AT),
    subscription: input.subscription ?? remoteSuccess(subscriptionOverviewFixture(), DASHBOARD_FIXTURE_LOADED_AT),
  })
  expect(model.status).toBe('ready')
  if (model.status !== 'ready') throw new Error(`expected ready model, received ${model.status}`)
  return model
}

describe('buildDashboardModel', () => {
  it('surfaces provider verification and followups without inventing a cancelled lifecycle', () => {
    const overview = dashboardOverviewFixture()
    overview.asset_summary = { ...overview.asset_summary, auto_renew_check_vps_count: 2, pending_followup_count: 3, archived_vps_count: 1 }
    const model = readyModel({ overview: remoteSuccess(overview, DASHBOARD_FIXTURE_LOADED_AT) })
    expect(model.judgements.find((item) => item.id === 'assets')).toMatchObject({
      detail: '自动续费待核对 2 · 跟进事项 3', tone: 'alert',
    })
  })
  it('treats severe monitoring instances as a subset of abnormal instances', () => {
    const model = readyModel({
      overview: remoteSuccess(
        dashboardOverviewFixture({
          abnormal_monitoring_instance_count: 2,
          severe_monitoring_instance_count: 1,
        }),
        DASHBOARD_FIXTURE_LOADED_AT,
      ),
    })

    expect(model.mode).toBe('critical')
    expect(model.observability.abnormalMonitoringCount).toBe(2)
    expect(model.observability.severeMonitoringCount).toBe(1)
    expect(model.observability.abnormalTotal).toBe(2)
  })

  it('does not infer onboarding when the VPS request failed', () => {
    const model = readyModel({
      overview: remoteSuccess(
        dashboardOverviewFixture({
          total_monitoring_instance_count: 0,
          total_target_count: 0,
        }),
        DASHBOARD_FIXTURE_LOADED_AT,
      ),
      vps: remoteError('VPS unavailable'),
      subscription: remoteError('Billing unavailable'),
    })

    expect(model.mode).toBe('stable')
    expect(model.primaryAction.label).not.toBe('创建第一台 VPS')
    expect(model.assetEvidence.status).toBe('unavailable')
    expect(model.tone).toBe('notice')
    expect(model.title).toBe('部分事实待确认')
    expect(model.degradations).toEqual([
      { resource: 'vps', message: 'VPS unavailable' },
      { resource: 'subscription', message: 'Billing unavailable' },
    ])
  })

  it('uses onboarding only for a confirmed empty VPS list and empty observability inventory', () => {
    const model = readyModel({
      overview: remoteSuccess(
        dashboardOverviewFixture({
          total_monitoring_instance_count: 0,
          total_target_count: 0,
        }),
        DASHBOARD_FIXTURE_LOADED_AT,
      ),
      vps: remoteSuccess([], DASHBOARD_FIXTURE_LOADED_AT),
    })

    expect(model.mode).toBe('onboarding')
    expect(model.primaryAction).toEqual({ label: '创建第一台 VPS', to: '/vps' })
  })

  it.each([
    {
      mode: 'critical',
      overview: dashboardOverviewFixture({
        abnormal_monitoring_instance_count: 2,
        severe_monitoring_instance_count: 1,
      }),
      label: '处理严重异常',
      to: '/events?severity=严重',
    },
    {
      mode: 'abnormal',
      overview: dashboardOverviewFixture({ abnormal_monitoring_instance_count: 1 }),
      label: '处理观测异常',
      to: '/monitoring?abnormal=1',
    },
    {
      mode: 'maintenance',
      overview: dashboardOverviewFixture({ maintenance_target_count: 1 }),
      label: '查看维护事件',
      to: '/events?maintenance_only=1',
    },
    {
      mode: 'stable',
      overview: dashboardOverviewFixture(),
      label: '核对 VPS 库存',
      to: '/vps',
    },
  ] as const)(
    'builds the unique primary action for $mode mode',
    ({ mode, overview, label, to }) => {
      const model = readyModel({
        overview: remoteSuccess(overview, DASHBOARD_FIXTURE_LOADED_AT),
      })

      expect(model.mode).toBe(mode)
      expect(model.primaryAction).toEqual({ label, to })
      expect(model.judgements.length).toBeLessThanOrEqual(3)
    },
  )

  it('keeps unobserved targets out of the abnormal total and links them on their own', () => {
    const onlyUnobserved = readyModel({
      overview: remoteSuccess(
        dashboardOverviewFixture({ unobserved_target_count: 3 }),
        DASHBOARD_FIXTURE_LOADED_AT,
      ),
    })
    expect(onlyUnobserved.mode).toBe('stable')
    expect(onlyUnobserved.observability.abnormalTotal).toBe(0)
    expect(onlyUnobserved.observability.unobservedTargetCount).toBe(3)
    expect(onlyUnobserved.title).toBe('尚有目标无观测')
    expect(onlyUnobserved.judgements.find((item) => item.id === 'observability')).toMatchObject({
      value: '3',
      to: '/targets?view=unobserved',
    })
    expect(onlyUnobserved.primaryAction.to).toBe('/vps')

    const both = readyModel({
      overview: remoteSuccess(
        dashboardOverviewFixture({
          abnormal_target_count: 2,
          severe_target_count: 1,
          unobserved_target_count: 4,
        }),
        DASHBOARD_FIXTURE_LOADED_AT,
      ),
    })
    expect(both.mode).toBe('critical')
    expect(both.observability.abnormalTotal).toBe(2)
    expect(both.observability.unobservedTargetCount).toBe(4)
    expect(both.primaryAction).toEqual({ label: '处理严重异常', to: '/events?severity=严重' })
    expect(both.judgements.find((item) => item.id === 'observability')?.to).toBe('/events?severity=严重')
    expect(both.judgements.find((item) => item.id === 'assets')?.label).not.toBe('尚无观测')
  })

  it('routes target-only abnormal state to the target work queue', () => {
    const model = readyModel({
      overview: remoteSuccess(
        dashboardOverviewFixture({ abnormal_target_count: 1 }),
        DASHBOARD_FIXTURE_LOADED_AT,
      ),
    })

    expect(model.mode).toBe('abnormal')
    expect(model.primaryAction).toEqual({
      label: '处理观测异常',
      to: '/targets?abnormal=1',
    })
  })

  it('marks subscription failure as a lower-precision dashboard fallback', () => {
    const model = readyModel({
      overview: remoteSuccess(
        dashboardOverviewFixture({
          asset_summary: {
            renewal_due_30d_vps_count: 2,
            cost_by_currency: [
              { currency: 'USD', monthly_total: 42.5, yearly_total: 510 },
            ],
          },
        }),
        DASHBOARD_FIXTURE_LOADED_AT,
      ),
      subscription: remoteError('subscription overview unavailable'),
    })

    expect(model.billingEvidence).toMatchObject({
      status: 'unavailable',
      source: 'dashboard-fallback',
      generatedAt: '2026-07-10T06:25:00Z',
    })
    expect(model.billingEvidence.detail).toContain('subscription overview unavailable')
    expect(model.billingEvidence.detail).toContain('Dashboard 聚合摘要')
  })

  it('routes an asset-attention judgement to the same decision workflow as its primary action', () => {
    const model = readyModel({
      overview: remoteSuccess(
        dashboardOverviewFixture({
          asset_summary: { unreviewed_vps_count: 2 },
        }),
        DASHBOARD_FIXTURE_LOADED_AT,
      ),
    })

    expect(model.mode).toBe('stable')
    expect(model.primaryAction).toEqual({
      label: '进入资产组合决策',
      to: '/asset-decisions?view=needs_decision&renew_within_days=30',
    })
    expect(model.judgements.find((item) => item.id === 'assets')).toMatchObject({
      label: '资产决策待核对',
      to: '/asset-decisions?view=needs_decision&renew_within_days=30',
    })
  })

  it('keeps unknown subscription money out of a zero total and a zero budget', () => {
    const allUnknown = readyModel({
      subscription: remoteSuccess(subscriptionOverviewFixture({
        active_subscription_count: 2,
        total_monthly_cost: 0,
        total_yearly_cost: 0,
        budget_risk_count: 0,
        current_unknown_amount_count: 2,
        current_missing_rate_count: 2,
        vps_costs: [],
      }), DASHBOARD_FIXTURE_LOADED_AT),
    })
    expect(allUnknown.billingEvidence.title).toBe('金额待核对')
    expect(allUnknown.billingEvidence.completeness).toBe('金额待核对')
    expect(allUnknown.billingEvidence.detail).toContain('预算风险 0')
    expect(allUnknown.billingEvidence.detail).toContain('待核对 2')
    expect(allUnknown.billingEvidence.detail).toContain('缺汇率 2')
    expect(allUnknown.judgements.find((item) => item.id === 'billing')).toMatchObject({
      value: '金额待核对',
      tone: 'notice',
    })

    const partial = readyModel({
      subscription: remoteSuccess(subscriptionOverviewFixture({
        total_monthly_cost: 40,
        budget_risk_count: 0,
        current_unknown_amount_count: 1,
        current_missing_rate_count: 1,
      }), DASHBOARD_FIXTURE_LOADED_AT),
    })
    expect(partial.billingEvidence.title).toBe('CNY 40.00/月')
    expect(partial.billingEvidence.completeness).toBe('已知金额小计（另有 1 项待核对）')
    expect(partial.billingEvidence.completeness).not.toContain('已完整折算')

    const trueZero = readyModel({
      subscription: remoteSuccess(subscriptionOverviewFixture({
        active_subscription_count: 1,
        total_monthly_cost: 0,
        total_yearly_cost: 0,
        current_unknown_amount_count: 0,
        current_stale_rate_count: 0,
      }), DASHBOARD_FIXTURE_LOADED_AT),
    })
    expect(trueZero.billingEvidence.title).toBe('CNY 0.00/月')
    expect(trueZero.billingEvidence.completeness).toBe('订阅摘要金额已完整折算')

    const stale = readyModel({
      subscription: remoteSuccess(subscriptionOverviewFixture({
        total_monthly_cost: 12,
        current_stale_rate_count: 1,
        current_unknown_amount_count: 0,
      }), DASHBOARD_FIXTURE_LOADED_AT),
    })
    expect(stale.billingEvidence.title).toBe('CNY 12.00/月')
    expect(stale.billingEvidence.completeness).toBe('汇率过期 1 项，金额仍按过期汇率计入')
    expect(stale.billingEvidence.detail).toContain('汇率过期 1')
  })

  it('preserves overview loading and error as explicit model states', () => {
    expect(buildDashboardModel({
      overview: remoteLoading(),
      vps: remoteLoading(),
      subscription: remoteLoading(),
    })).toEqual({ status: 'loading' })

    expect(buildDashboardModel({
      overview: remoteError('dashboard unavailable'),
      vps: remoteLoading(),
      subscription: remoteLoading(),
    })).toEqual({ status: 'error', error: 'dashboard unavailable' })
  })
})
