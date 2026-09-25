import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import type { MonitoringInstanceManagementReview, MonitoringInstanceRecord } from '../../lib/types'
import { MonitoringDetailManagementMenu } from './MonitoringDetailManagementMenu'

function record(overrides: Partial<MonitoringInstanceRecord> = {}): MonitoringInstanceRecord {
  return {
    monitoring_instance_id: 'mi_manage',
    display_name: 'Tokyo Managed Edge',
    group: '',
    region: 'ap-northeast-1',
    city: 'Tokyo',
    provider: 'Vultr',
    lifecycle_status: '在用',
    monitoring_status: '启用',
    binding_status: '已绑定',
    labels: [],
    note: '',
    current_health_status: '正常',
    last_heartbeat_at: '2026-04-24T09:00:00Z',
    last_sync_at: '2026-04-24T09:05:00Z',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-27T09:30:00Z',
    ...overrides,
  }
}

function review(
  current: MonitoringInstanceRecord,
  overrides: Partial<MonitoringInstanceManagementReview> & {
    actions?: {
      can_retire?: boolean
      can_restore_lifecycle?: boolean
      can_archive?: boolean
      can_restore_archive?: boolean
      can_permanent_cleanup?: boolean
    }
    blockers?: string[]
    warnings?: string[]
  } = {},
): MonitoringInstanceManagementReview {
  const flags = overrides.actions ?? {}
  const blockers = overrides.blockers ?? []
  const warnings = overrides.warnings ?? []
  const action = (allowed = false) => ({ allowed, blockers, warnings })
  const rest = { ...overrides }
  delete rest.actions
  delete rest.blockers
  delete rest.warnings
  return {
    record: current,
    active_vps_links: [],
    counts: {
      heartbeat_count: 0,
      host_sample_count: 0,
      probe_observation_count: 0,
      host_sample_daily_aggregate_count: 0,
      ip_quality_report_count: 0,
      active_incident_count: 0,
      state_change_event_count: 0,
      notification_record_count: 0,
      asset_lifecycle_action_step_count: 0,
      command_action_audit_count: 2,
      active_vps_link_count: 0,
    },
    action_reviews: {
      retire: action(flags.can_retire ?? true),
      restore: action(flags.can_restore_lifecycle ?? false),
      archive: action(flags.can_archive ?? false),
      restore_from_archive: action(flags.can_restore_archive ?? false),
      permanent_cleanup: action(flags.can_permanent_cleanup ?? false),
    },
    dependency_impacts: [],
    preview_digest: 'review-digest',
    empty_mistake_candidate: false,
    ...rest,
  }
}

type Overrides = Partial<Parameters<typeof MonitoringDetailManagementMenu>[0]>

function renderMenu(overrides: Overrides = {}) {
  const current = overrides.monitoringInstance ?? record()
  const props = {
    monitoringInstance: current,
    runtimeActions: [{ action: 'enter-maintenance' as const, label: '进入维护' }],
    runtimeSubmitting: false,
    onRuntimeAction: vi.fn(),
    registerActionRef: vi.fn(),
    onOpenOnboarding: vi.fn(),
    onboardingActionLabel: '接入 agent…',
    onOpenCommands: vi.fn(),
    onOpenMetadata: vi.fn(),
    review: review(current),
    loading: false,
    error: null,
    submittingAction: null,
    actionError: null,
    onLoadReview: vi.fn(),
    onRetire: vi.fn(),
    onRestoreLifecycle: vi.fn(),
    onArchive: vi.fn(),
    onRestoreArchive: vi.fn(),
    onPermanentCleanup: vi.fn(),
    ...overrides,
  }

  const utils = render(
    <MemoryRouter>
      <MonitoringDetailManagementMenu {...props} />
    </MemoryRouter>,
  )
  return { ...utils, props }
}

function openMenu() {
  const trigger = screen.getByRole('button', { name: '管理' })
  fireEvent.click(trigger)
  return trigger
}

describe('MonitoringDetailManagementMenu', () => {
  it('loads the management review when the menu opens', () => {
    const { props } = renderMenu()
    expect(props.onLoadReview).not.toHaveBeenCalled()
    openMenu()
    expect(props.onLoadReview).toHaveBeenCalledTimes(1)
  })

  it('groups runtime, profile and lifecycle items under Chinese labels only', () => {
    renderMenu()
    openMenu()
    expect(screen.getByText('运行控制')).toBeInTheDocument()
    expect(screen.getByText('资料')).toBeInTheDocument()
    expect(screen.getByText('生命周期')).toBeInTheDocument()
    expect(screen.getByText('当前：在用')).toBeInTheDocument()
    expect(screen.queryByText(/Group/)).not.toBeInTheDocument()
  })

  it('uses .btn.lg menu items and a monitoring-owned menu container', () => {
    const { container } = renderMenu()
    openMenu()
    const menu = container.querySelector('.monitoring-detail-management__menu')
    expect(menu).not.toBeNull()
    const items = Array.from(container.querySelectorAll('[role="menuitem"]'))
    expect(items.length).toBeGreaterThan(0)
    for (const item of items) {
      expect(item.className).toContain('lg')
      expect(item.className).toContain('monitoring-detail-management__item')
    }
    expect(container.querySelector('.vps-overview-management')).toBeNull()
  })

  it('dispatches runtime actions and keeps the menu open for the confirmation', () => {
    const onRuntimeAction = vi.fn()
    renderMenu({ onRuntimeAction })
    openMenu()
    fireEvent.click(screen.getByRole('menuitem', { name: '进入维护' }))
    expect(onRuntimeAction).toHaveBeenCalledWith('enter-maintenance')
    expect(screen.getByRole('menu', { name: '管理' })).toBeInTheDocument()
  })

  it('closes the menu when opening the metadata dialog, the onboarding drawer or commands', () => {
    for (const [label, key] of [
      ['编辑分组、标签与备注', 'onOpenMetadata'],
      ['接入 agent…', 'onOpenOnboarding'],
      ['执行诊断命令…', 'onOpenCommands'],
    ] as const) {
      const handler = vi.fn()
      const { unmount } = renderMenu({ [key]: handler } as Overrides)
      openMenu()
      fireEvent.click(screen.getByRole('menuitem', { name: label }))
      expect(handler).toHaveBeenCalledTimes(1)
      expect(screen.queryByRole('menu', { name: '管理' })).not.toBeInTheDocument()
      unmount()
    }
  })

  it('links the command audit to the monitoring instance', () => {
    renderMenu()
    openMenu()
    expect(screen.getByRole('menuitem', { name: '查看命令审计' })).toHaveAttribute(
      'href',
      '/command-audit?monitoring_instance=mi_manage',
    )
  })

  it('hides the runtime group and disables profile editing for archived instances', () => {
    renderMenu({
      monitoringInstance: record({ lifecycle_status: '已退役', archived_at: '2026-04-27T09:35:00Z' }),
    })
    openMenu()
    expect(screen.queryByText('运行控制')).not.toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '进入维护' })).not.toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '接入 agent…' })).not.toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: '编辑分组、标签与备注' })).toBeDisabled()
    expect(screen.getByText('已归档实例资料只读')).toBeInTheDocument()
  })

  it('hides the runtime group for a retired instance that is not archived yet', () => {
    renderMenu({ monitoringInstance: record({ lifecycle_status: '已退役' }) })
    openMenu()
    expect(screen.queryByText('运行控制')).not.toBeInTheDocument()
  })

  it('shows only the lifecycle actions the review allows', () => {
    const current = record()
    renderMenu({
      monitoringInstance: current,
      review: review(current, {
        actions: {
          can_retire: false,
          can_restore_lifecycle: false,
          can_archive: true,
          can_restore_archive: true,
          can_permanent_cleanup: false,
        },
      }),
    })
    openMenu()
    expect(screen.queryByRole('menuitem', { name: '退役' })).not.toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: '归档' })).toBeEnabled()
    expect(screen.getByRole('menuitem', { name: '恢复归档' })).toBeEnabled()
    expect(screen.queryByRole('menuitem', { name: '永久清理' })).not.toBeInTheDocument()
  })

  it('shows a loading line and no action buttons while the review loads', () => {
    renderMenu({ review: null, loading: true })
    openMenu()
    expect(screen.getByText('正在加载…')).toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '退役' })).not.toBeInTheDocument()
  })

  it('shows the review error with a forced retry', () => {
    const onLoadReview = vi.fn()
    renderMenu({ review: null, loading: false, error: 'review failed', onLoadReview })
    openMenu()
    expect(screen.getByText('review failed')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    expect(onLoadReview).toHaveBeenCalledWith(true)
  })

  it('freezes the display name while the confirmation is open and rejects a changed version', () => {
    const onPermanentCleanup = vi.fn()
    const onLoadReview = vi.fn()
    const initial = record({ lifecycle_status: '已退役' })
    const { rerender } = render(
      <MemoryRouter>
        <MonitoringDetailManagementMenu
          {...renderMenuProps(initial, review(initial, {
            actions: {
              can_retire: false,
              can_restore_lifecycle: false,
              can_archive: false,
              can_restore_archive: true,
              can_permanent_cleanup: true,
            },
          }), { onPermanentCleanup, onLoadReview })}
        />
      </MemoryRouter>,
    )

    openMenu()
    fireEvent.click(screen.getByRole('menuitem', { name: '永久清理' }))
    const dialog = screen.getByRole('alertdialog', { name: '永久清理监控实例' })
    expect(within(dialog).getByText(/Tokyo Managed Edge 将进入不可恢复清理流程/)).toBeInTheDocument()

    const renamed = record({
      lifecycle_status: '已退役',
      display_name: 'Renamed Edge',
      updated_at: '2026-04-27T10:00:00Z',
    })
    rerender(
      <MemoryRouter>
        <MonitoringDetailManagementMenu
          {...renderMenuProps(renamed, review(renamed, {
            actions: {
              can_retire: false,
              can_restore_lifecycle: false,
              can_archive: false,
              can_restore_archive: true,
              can_permanent_cleanup: true,
            },
          }), { onPermanentCleanup, onLoadReview })}
        />
      </MemoryRouter>,
    )

    expect(within(dialog).getByText(/Tokyo Managed Edge 将进入不可恢复清理流程/)).toBeInTheDocument()
    expect(within(dialog).queryByText(/Renamed Edge 将进入不可恢复清理流程/)).not.toBeInTheDocument()
    fireEvent.change(within(dialog).getByLabelText('原因'), { target: { value: '误创建空实例' } })
    fireEvent.change(within(dialog).getByLabelText('输入实例名称确认'), { target: { value: 'Tokyo Managed Edge' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '确认永久清理' }))
    expect(onPermanentCleanup).not.toHaveBeenCalled()
    expect(onLoadReview).toHaveBeenCalledWith(true)
    expect(screen.getByText('实例已更新，请关闭后重新确认。')).toBeInTheDocument()
  })

  it('requires the reason and the exact display name before submitting', () => {
    const onArchive = vi.fn()
    const current = record()
    renderMenu({
      monitoringInstance: current,
      review: review(current, {
        actions: {
          can_retire: false,
          can_restore_lifecycle: false,
          can_archive: true,
          can_restore_archive: false,
          can_permanent_cleanup: false,
        },
      }),
      onArchive,
    })
    openMenu()
    fireEvent.click(screen.getByRole('menuitem', { name: '归档' }))
    const dialog = screen.getByRole('alertdialog', { name: '归档监控实例' })
    const confirm = within(dialog).getByRole('button', { name: '确认归档' })
    expect(confirm).toBeDisabled()
    fireEvent.change(within(dialog).getByLabelText('原因'), { target: { value: '重复创建' } })
    expect(confirm).toBeDisabled()
    fireEvent.change(within(dialog).getByLabelText('输入实例名称确认'), { target: { value: 'Tokyo Managed Edge' } })
    expect(confirm).toBeEnabled()
    fireEvent.click(confirm)
    expect(onArchive).toHaveBeenCalledWith('重复创建', 'Tokyo Managed Edge', {
      preview_digest: 'review-digest',
      confirm_shared_impact: false,
    })
  })

  it('closes on Escape and restores focus to the 管理 trigger', async () => {
    renderMenu()
    const trigger = openMenu()
    const first = screen.getByRole('menuitem', { name: '进入维护' })
    expect(first).toHaveFocus()

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('menu', { name: '管理' })).not.toBeInTheDocument()
    await waitFor(() => expect(trigger).toHaveFocus())
  })

  it('closes on Tab without preventing default', () => {
    renderMenu()
    openMenu()
    const notPrevented = fireEvent.keyDown(document, { key: 'Tab' })
    expect(notPrevented).toBe(true)
    expect(screen.queryByRole('menu', { name: '管理' })).not.toBeInTheDocument()
  })

  it('moves focus with ArrowDown, ArrowUp, Home and End', () => {
    renderMenu()
    openMenu()
    const items = screen.getAllByRole('menuitem')
    const first = items[0]!
    const last = items[items.length - 1]!
    expect(first).toHaveFocus()

    fireEvent.keyDown(document, { key: 'ArrowDown' })
    expect(items[1]).toHaveFocus()
    fireEvent.keyDown(document, { key: 'ArrowUp' })
    expect(first).toHaveFocus()
    fireEvent.keyDown(document, { key: 'End' })
    expect(last).toHaveFocus()
    fireEvent.keyDown(document, { key: 'ArrowDown' })
    expect(first).toHaveFocus()
    fireEvent.keyDown(document, { key: 'Home' })
    expect(first).toHaveFocus()
  })

  it('closes on an outside pointerdown without stealing focus', () => {
    renderMenu()
    openMenu()
    fireEvent.mouseDown(document.body)
    expect(screen.queryByRole('menu', { name: '管理' })).not.toBeInTheDocument()
  })

  it('resets shared-impact consent when dialog is closed and reopened with the same or another action', () => {
    const current = record()
    const sharedReview = review(current, {
      dependency_impacts: [
        {
          object_type: 'monitoring_instance',
          object_id: 'mi_manage',
          vps_id: 'vps_001',
          vps_lifecycle_status: 'active',
          relation_type: 'agent',
          relation_id: 'rel_001',
          relation_status: 'active',
          classification: 'current',
        },
        {
          object_type: 'monitoring_instance',
          object_id: 'mi_manage',
          vps_id: 'vps_002',
          vps_lifecycle_status: 'active',
          relation_type: 'agent',
          relation_id: 'rel_002',
          relation_status: 'active',
          classification: 'current',
        },
      ],
      preview_digest: 'shared-digest-v1',
      actions: { can_retire: true },
    })

    renderMenu({ monitoringInstance: current, review: sharedReview })
    openMenu()
    fireEvent.click(screen.getByRole('menuitem', { name: '退役' }))

    // Fill reason
    fireEvent.change(screen.getByLabelText('原因'), { target: { value: '退役实例' } })
    const confirmBtn = screen.getByRole('button', { name: '确认退役' })
    const checkbox = screen.getByLabelText('确认此监控实例对多台 VPS 的当前或残留影响')

    // Confirm should be disabled until checked
    expect(confirmBtn).toBeDisabled()
    expect(checkbox).not.toBeChecked()

    fireEvent.click(checkbox)
    expect(checkbox).toBeChecked()
    expect(confirmBtn).toBeEnabled()

    // Cancel dialog
    fireEvent.click(screen.getByRole('button', { name: '取消' }))

    // Reopen menu and dialog
    openMenu()
    fireEvent.click(screen.getByRole('menuitem', { name: '退役' }))

    const reopenedCheckbox = screen.getByLabelText('确认此监控实例对多台 VPS 的当前或残留影响')
    const reopenedConfirmBtn = screen.getByRole('button', { name: '确认退役' })
    expect(reopenedCheckbox).not.toBeChecked()
    expect(reopenedConfirmBtn).toBeDisabled()
  })

  it('invalidates shared-impact consent when review digest changes', () => {
    const current = record()
    const sharedReview = review(current, {
      dependency_impacts: [
        {
          object_type: 'monitoring_instance',
          object_id: 'mi_manage',
          vps_id: 'vps_001',
          vps_lifecycle_status: 'active',
          relation_type: 'agent',
          relation_id: 'rel_001',
          relation_status: 'active',
          classification: 'current',
        },
        {
          object_type: 'monitoring_instance',
          object_id: 'mi_manage',
          vps_id: 'vps_002',
          vps_lifecycle_status: 'active',
          relation_type: 'agent',
          relation_id: 'rel_002',
          relation_status: 'active',
          classification: 'current',
        },
      ],
      preview_digest: 'shared-digest-v1',
      actions: { can_retire: true },
    })

    const { rerender } = render(
      <MemoryRouter>
        <MonitoringDetailManagementMenu
          {...renderMenuProps(current, sharedReview, {})}
        />
      </MemoryRouter>,
    )
    openMenu()
    fireEvent.click(screen.getByRole('menuitem', { name: '退役' }))

    fireEvent.change(screen.getByLabelText('原因'), { target: { value: '退役实例' } })
    const checkbox = screen.getByLabelText('确认此监控实例对多台 VPS 的当前或残留影响')
    fireEvent.click(checkbox)
    expect(checkbox).toBeChecked()
    expect(screen.getByRole('button', { name: '确认退役' })).toBeEnabled()

    // Review refreshes with changed digest
    const updatedReview = { ...sharedReview, preview_digest: 'shared-digest-v2' }
    rerender(
      <MemoryRouter>
        <MonitoringDetailManagementMenu
          {...renderMenuProps(current, updatedReview, {})}
        />
      </MemoryRouter>,
    )

    const updatedCheckbox = screen.getByLabelText('确认此监控实例对多台 VPS 的当前或残留影响')
    expect(updatedCheckbox).not.toBeChecked()
    expect(screen.getByRole('button', { name: '确认退役' })).toBeDisabled()
  })

  it('blocks management confirmation while the review digest is missing or the review failed', () => {
    const current = record()
    const loaded = review(current, {
      actions: { can_retire: false, can_archive: true },
    })
    const { rerender } = render(
      <MemoryRouter>
        <MonitoringDetailManagementMenu
          {...renderMenuProps(current, { ...loaded, preview_digest: '' }, {})}
        />
      </MemoryRouter>,
    )
    openMenu()
    fireEvent.click(screen.getByRole('menuitem', { name: '归档' }))
    fireEvent.change(screen.getByLabelText('原因'), { target: { value: '重复创建' } })
    fireEvent.change(screen.getByLabelText('输入实例名称确认'), { target: { value: 'Tokyo Managed Edge' } })
    expect(screen.getByRole('button', { name: '确认归档' })).toBeDisabled()

    rerender(
      <MemoryRouter>
        <MonitoringDetailManagementMenu
          {...renderMenuProps(current, loaded, { error: '管理审查加载失败' })}
        />
      </MemoryRouter>,
    )
    expect(screen.getByRole('button', { name: '确认归档' })).toBeDisabled()

    rerender(
      <MemoryRouter>
        <MonitoringDetailManagementMenu
          {...renderMenuProps(current, loaded, { review: null })}
        />
      </MemoryRouter>,
    )
    expect(screen.getByRole('button', { name: '确认归档' })).toBeDisabled()
  })

  it('keeps the reason and display-name draft and clears shared confirmation when the action is stale', async () => {
    const current = record()
    const onArchive = vi.fn().mockResolvedValue('stale')
    renderMenu({
      monitoringInstance: current,
      review: review(current, {
        actions: { can_retire: false, can_archive: true },
        dependency_impacts: [
          {
            object_type: 'monitoring_instance',
            object_id: 'mi_manage',
            vps_id: 'vps_001',
            vps_lifecycle_status: 'active',
            relation_type: 'agent',
            relation_id: 'rel_001',
            relation_status: 'active',
            classification: 'current',
          },
          {
            object_type: 'monitoring_instance',
            object_id: 'mi_manage',
            vps_id: 'vps_002',
            vps_lifecycle_status: 'active',
            relation_type: 'agent',
            relation_id: 'rel_002',
            relation_status: 'active',
            classification: 'current',
          },
        ],
        preview_digest: 'shared-digest-v1',
      }),
      onArchive,
    })
    openMenu()
    fireEvent.click(screen.getByRole('menuitem', { name: '归档' }))
    fireEvent.change(screen.getByLabelText('原因'), { target: { value: '重复创建' } })
    fireEvent.change(screen.getByLabelText('输入实例名称确认'), { target: { value: 'Tokyo Managed Edge' } })
    fireEvent.click(screen.getByRole('checkbox', { name: '确认此监控实例对多台 VPS 的当前或残留影响' }))
    fireEvent.click(screen.getByRole('button', { name: '确认归档' }))

    expect(onArchive).toHaveBeenCalledWith('重复创建', 'Tokyo Managed Edge', {
      preview_digest: 'shared-digest-v1',
      confirm_shared_impact: true,
    })
    const dialog = await screen.findByRole('alertdialog', { name: '归档监控实例' })
    expect(within(dialog).getByText('影响范围已变化，共享确认已清除，不会自动重新提交。')).toBeInTheDocument()
    expect(within(dialog).getByLabelText('原因')).toHaveValue('重复创建')
    expect(within(dialog).getByLabelText('输入实例名称确认')).toHaveValue('Tokyo Managed Edge')
    expect(within(dialog).getByRole('checkbox', { name: '确认此监控实例对多台 VPS 的当前或残留影响' })).not.toBeChecked()
  })

})

function renderMenuProps(
  monitoringInstance: MonitoringInstanceRecord,
  currentReview: MonitoringInstanceManagementReview,
  overrides: Overrides,
): Parameters<typeof MonitoringDetailManagementMenu>[0] {
  return {
    monitoringInstance,
    runtimeActions: [],
    runtimeSubmitting: false,
    onRuntimeAction: vi.fn(),
    registerActionRef: vi.fn(),
    onOpenOnboarding: vi.fn(),
    onboardingActionLabel: '接入 agent…',
    onOpenCommands: vi.fn(),
    onOpenMetadata: vi.fn(),
    review: currentReview,
    loading: false,
    error: null,
    submittingAction: null,
    actionError: null,
    onLoadReview: vi.fn(),
    onRetire: vi.fn(),
    onRestoreLifecycle: vi.fn(),
    onArchive: vi.fn(),
    onRestoreArchive: vi.fn(),
    onPermanentCleanup: vi.fn(),
    ...overrides,
  }
}
