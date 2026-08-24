import { matchRoutes } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { appRoutes } from '../../app/router'
import {
  resolveVPSOverviewAnomalyDestination,
  resolveVPSOverviewRelationDestination,
  type VPSOverviewDestination,
} from './vpsOverviewDestination'

const vpsId = 'vps_001'

describe('resolveVPSOverviewAnomalyDestination', () => {
  const tests: Array<{
    ruleId: string
    action: { id: string; route?: string }
    expected: VPSOverviewDestination
  }> = [
    {
      ruleId: 'monitoring.health.abnormal.v1',
      action: { id: 'open_monitoring', route: '/monitoring?abnormal=1' },
      expected: { kind: 'route', to: '/monitoring?abnormal=1' },
    },
    {
      ruleId: 'monitoring.incidents.open.v1',
      action: { id: 'open_incidents', route: '/events?object_type=monitoring_instance' },
      expected: { kind: 'route', to: '/events?object_type=monitoring_instance' },
    },
    ...[
      'ip_quality.risk.elevated.v1',
      'ip_quality.stale.v1',
      'ip_quality.partial.v1',
    ].map((ruleId) => ({
      ruleId,
      action: { id: 'open_ip_quality', route: '/vps/vps_001/ip-quality' },
      expected: { kind: 'route' as const, to: '/vps/vps_001/ip-quality' },
    })),
    {
      ruleId: 'renewal.subscription.missing.v1',
      action: { id: 'open_subscription' },
      expected: { kind: 'command', command: 'open_subscription' },
    },
    {
      ruleId: 'renewal.due.soon.v1',
      action: { id: 'open_renewal_decision' },
      expected: { kind: 'command', command: 'open_renewal_decision' },
    },
    {
      ruleId: 'lifecycle.blocker.v1',
      action: { id: 'open_management' },
      expected: { kind: 'command', command: 'open_management' },
    },
    {
      ruleId: 'source.unavailable.v1',
      action: { id: 'retry_overview' },
      expected: { kind: 'command', command: 'retry_overview' },
    },
  ]

  for (const test of tests) {
    it(`resolves ${test.ruleId} with ${test.action.id}`, () => {
      expect(resolveVPSOverviewAnomalyDestination(vpsId, test.ruleId, test.action)).toEqual(test.expected)
    })
  }

  it.each([
    ['unknown action', 'monitoring.health.abnormal.v1', { id: 'unknown', route: '/monitoring?abnormal=1' }],
    ['mismatched rule and action', 'monitoring.health.abnormal.v1', { id: 'open_incidents', route: '/events?object_type=monitoring_instance' }],
    ['same-origin wrong route', 'monitoring.health.abnormal.v1', { id: 'open_monitoring', route: '/vps/vps_001' }],
    ['external route', 'monitoring.health.abnormal.v1', { id: 'open_monitoring', route: 'https://evil.example' }],
    ['protocol-relative route', 'monitoring.health.abnormal.v1', { id: 'open_monitoring', route: '//evil.example' }],
    ['backslash route', 'monitoring.health.abnormal.v1', { id: 'open_monitoring', route: '\\evil.example' }],
    ['command carrying a route', 'renewal.subscription.missing.v1', { id: 'open_subscription', route: '/vps/vps_001' }],
  ])('fails closed for %s', (_name, ruleId, action) => {
    expect(resolveVPSOverviewAnomalyDestination(vpsId, ruleId, action)).toBeNull()
  })
})

describe('resolveVPSOverviewRelationDestination', () => {
  it.each([
    ['subscriptions', { kind: 'route', to: '/subscriptions?vps_id=vps_001' }],
    ['monitoring_instances', { kind: 'command', command: 'open_monitoring_instances' }],
    ['services', { kind: 'command', command: 'open_services' }],
    ['domains', { kind: 'command', command: 'open_domains' }],
  ] as const)('resolves the %s relation', (kind, expected) => {
    const route = kind === 'subscriptions' ? '/subscriptions?vps_id=vps_001' : undefined
    expect(resolveVPSOverviewRelationDestination(vpsId, { kind, ...(route ? { route } : {}) })).toEqual(expected)
  })

  it.each([
    ['unknown relation', { kind: 'unknown' }],
    ['same-origin wrong route', { kind: 'subscriptions', route: '/subscriptions' }],
    ['external route', { kind: 'subscriptions', route: 'https://evil.example' }],
    ['protocol-relative route', { kind: 'subscriptions', route: '//evil.example' }],
    ['backslash route', { kind: 'subscriptions', route: '\\evil.example' }],
    ['command relation carrying a route', { kind: 'services', route: '/vps/vps_001' }],
  ])('fails closed for %s', (_name, relation) => {
    expect(resolveVPSOverviewRelationDestination(vpsId, relation)).toBeNull()
  })

  it('computes encoded VPS path and query destinations before comparing API routes', () => {
    const encodedVpsId = 'vps /东京'
    expect(resolveVPSOverviewAnomalyDestination(encodedVpsId, 'ip_quality.stale.v1', {
      id: 'open_ip_quality',
      route: '/vps/vps%20%2F%E4%B8%9C%E4%BA%AC/ip-quality',
    })).toEqual({ kind: 'route', to: '/vps/vps%20%2F%E4%B8%9C%E4%BA%AC/ip-quality' })
    expect(resolveVPSOverviewRelationDestination(encodedVpsId, {
      kind: 'subscriptions',
      route: '/subscriptions?vps_id=vps+%2F%E4%B8%9C%E4%BA%AC',
    })).toEqual({ kind: 'route', to: '/subscriptions?vps_id=vps+%2F%E4%B8%9C%E4%BA%AC' })
  })
})

describe('VPS overview route ownership', () => {
  it.each([
    ['/monitoring?abnormal=1', 'monitoring'],
    ['/events?object_type=monitoring_instance', 'events'],
    ['/vps/vps_001/ip-quality', 'vps/:vpsId/ip-quality'],
    ['/subscriptions?vps_id=vps_001', 'subscriptions'],
  ])('matches %s to the non-wildcard owner %s', (to, ownerPath) => {
    const matches = matchRoutes(appRoutes, to)
    expect(matches).not.toBeNull()
    const owner = matches?.[matches.length - 1]?.route.path
    expect(owner).toBe(ownerPath)
    expect(owner).not.toBe('*')
  })
})
