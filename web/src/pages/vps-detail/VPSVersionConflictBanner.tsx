import { Button } from '../../components/atoms'
import type { VPSVersionConflictState } from './vpsManagementHelpers'

export function VPSVersionConflictBanner({
  conflict,
  onLoadLatest,
  loading = false,
}: {
  conflict: VPSVersionConflictState
  onLoadLatest: () => void
  loading?: boolean
}) {
  return (
    <div className="asset-operation-feedback asset-operation-feedback--notice" role="status">
      <p>
        {conflict.loaded
          ? '已加载最新版本。保存将使用新的更新时间。你改过的字段会保留；未改过的字段已换成最新值。'
          : 'VPS 已被其他操作更新。请先加载最新版本，草稿会保留。'}
      </p>
      {conflict.compare.length > 0 ? (
        <ul>
          {conflict.compare.map((row) => (
            <li key={row.field}>{row.field}：将保留你的草稿 {row.yours || '（空）'}，而不是最新 {row.latest || '（空）'}</li>
          ))}
        </ul>
      ) : null}
      {conflict.loaded ? null : (
        <Button size="sm" onClick={onLoadLatest} disabled={loading}>
          {loading ? '正在加载最新版本' : '加载最新版本'}
        </Button>
      )}
    </div>
  )
}
