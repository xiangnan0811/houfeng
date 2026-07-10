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
