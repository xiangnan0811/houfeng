import { Timestamp } from '../../components/atoms'

export function TargetSnapshotMeta() {
  return (
    <p className="watchtower-snapshot-meta">
      数据快照时间：<Timestamp value={new Date().toISOString()} mode="absolute" />
      ，刷新页面获取最新。
    </p>
  )
}
