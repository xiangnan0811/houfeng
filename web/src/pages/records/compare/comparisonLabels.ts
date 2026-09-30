import type { ComparisonReason } from '../../../lib/types'

/** 可比性原因的完整中文说明；审查列表与另存阻断共用。 */
export const COMPARISON_REASON_LABELS: Record<ComparisonReason, string> = {
  metadata_only: '仅元数据，无数值比较',
  kind_missing: '缺少该证据类型',
  metric_missing: '缺少该指标',
  coverage_partial: '覆盖不完整',
  coverage_truncated: '序列被截断',
  common_overlap_unsupported: '当前类型不支持共同重叠',
  common_overlap_empty: '共同重叠为空',
  schema_incompatible: 'schema 不兼容',
  unit_incompatible: '单位不兼容',
  precision_incompatible: '精度不兼容',
  source_tombstoned: '来源已墓碑化',
  source_unavailable: '来源当前不可用',
  snapshot_unreadable: '快照不可读',
}
