import { dependencyClassificationLabel } from '../lib/assetLifecycle'
import type { DependencyImpact } from '../lib/types'

type SharedImpactPanelProps = {
  impacts: readonly DependencyImpact[]
  currentVPSID?: string
  confirmations?: ReadonlyArray<{
    key: string
    label: string
    checked: boolean
    disabled?: boolean
  }>
  onToggle?: (key: string, checked: boolean) => void
}

export function SharedImpactPanel({
  impacts,
  currentVPSID,
  confirmations = [],
  onToggle,
}: SharedImpactPanelProps) {
  if (impacts.length === 0 && confirmations.length === 0) return null
  return (
    <section className="asset-cancel-workbench__section" aria-label="共享影响">
      <div className="asset-cancel-workbench__section-head">
        <div>
          <p className="asset-cancel-workbench__eyebrow">共享影响</p>
          <h3>受影响的 VPS 与来源</h3>
        </div>
      </div>
      <p className="asset-cancel-workbench__choice-note">
        当前承载、已取消残留和状态待确认的依赖都需要确认。已归档父节点、已暂停或已退役依赖、已解除关联只作历史展示，不要求本次确认。
      </p>
      {impacts.length > 0 ? (
        <ul className="asset-lifecycle-confirm__blockers">
          {impacts.map((impact) => (
            <li key={`${impact.object_type}:${impact.object_id}:${impact.vps_id}:${impact.relation_id}`}>
              {impact.vps_id === currentVPSID ? '本机' : impact.vps_id}
              {' · '}
              {impact.vps_lifecycle_status}
              {' · '}
              {impact.relation_type}
              {' '}
              {impact.relation_status}
              {' · '}
              {dependencyClassificationLabel(impact.classification)}
              {' · '}
              {impact.object_type}
              {' '}
              {impact.object_id}
            </li>
          ))}
        </ul>
      ) : (
        <p className="asset-cancel-workbench__empty">没有需要展示的依赖影响。</p>
      )}
      {confirmations.map((item) => (
        <label key={item.key} className="asset-cancel-workbench__inline-check">
          <input
            type="checkbox"
            checked={item.checked}
            disabled={item.disabled}
            onChange={(event) => onToggle?.(item.key, event.target.checked)}
          />
          <span>确认 {item.label} 的跨 VPS 影响</span>
        </label>
      ))}
    </section>
  )
}
