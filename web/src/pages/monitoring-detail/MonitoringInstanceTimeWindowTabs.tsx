import { SegmentedControl } from '../../components/atoms'
import { TIME_WINDOW_ITEMS } from './monitoringDetailConstants'
import type { RuntimeStreamStatus, TimeWindow } from './types'

type MonitoringInstanceTimeWindowTabsProps = {
  value: TimeWindow
  onChange: (value: TimeWindow) => void
  streamStatus?: RuntimeStreamStatus
  streamError?: string | null
}

const STREAM_STATUS_LABELS: Record<RuntimeStreamStatus, string> = {
  idle: '已断开',
  connecting: '连接中',
  connected: '已连接',
  reconnecting: '重连中',
  disconnected: '已断开',
}

export function MonitoringInstanceTimeWindowTabs({
  value,
  onChange,
  streamStatus = 'idle',
  streamError,
}: MonitoringInstanceTimeWindowTabsProps) {
  return (
    <div className="monitoring-detail-window-tabs">
      {value === 'realtime' ? (
        <span className={`monitoring-detail-stream-status monitoring-detail-stream-status--${streamStatus}`}>
          {STREAM_STATUS_LABELS[streamStatus]}
          {streamError ? ` · ${streamError}` : ''}
        </span>
      ) : null}
      <SegmentedControl<TimeWindow>
        label="观测时间窗口"
        value={value}
        onChange={onChange}
        items={TIME_WINDOW_ITEMS}
      />
    </div>
  )
}
