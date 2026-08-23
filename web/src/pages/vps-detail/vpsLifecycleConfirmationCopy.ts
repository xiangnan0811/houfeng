import type { VPSAssetDetail } from '../../lib/types'

export type VPSLifecycleAction = 'archive' | 'restore'

export function vpsLifecycleConfirmationCopy(
  detail: Pick<VPSAssetDetail, 'display_name'>,
  action: VPSLifecycleAction,
) {
  const isRestore = action === 'restore'
  return {
    title: isRestore ? '确认恢复 VPS' : '确认归档 VPS',
    current: isRestore
      ? `当前：${detail.display_name} 已归档，不在当前资产工作集中。`
      : `当前：${detail.display_name} 仍在当前资产工作集中。`,
    result: isRestore
      ? '操作后：生命周期变为闲置，并由后端清空归档时间。'
      : '操作后：生命周期变为已归档，并记录归档时间。',
    impact: isRestore
      ? '恢复后它会重新进入 VPS 台账的人工核对范围，但不会自动改变续费决策。'
      : '归档后它不会作为活跃 VPS 进入续费、迁移或成本核对队列。',
    unchanged: isRestore
      ? '不会删除或重建 VPS、订阅、监控实例关联或资产历史。'
      : '不会删除 VPS、订阅、监控实例关联或资产历史。后续可恢复为闲置。',
    confirmLabel: isRestore ? '确认恢复' : '确认归档',
  }
}
