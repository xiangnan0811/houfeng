import { Timestamp } from '../../components/atoms'
import type { HostSample } from '../../lib/types'

type MonitoringInstanceSnapshotMetaProps = {
  sample: HostSample | null
}

export function MonitoringInstanceSnapshotMeta({ sample }: MonitoringInstanceSnapshotMetaProps) {
  return (
    <p className="watchtower-snapshot-meta">
      {sample?.observed_at ? (
        <>
          数据快照时间：<Timestamp value={sample.observed_at} mode="absolute" />
          ，刷新页面获取最新。
        </>
      ) : (
        '尚未收到主机样本，暂无快照数据。'
      )}
    </p>
  )
}
