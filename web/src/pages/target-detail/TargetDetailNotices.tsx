import type { ReactNode } from 'react'

import { ObservabilityNotice, type ObservabilityTone } from '../../components/observability'
import { Button } from '../../components/atoms/Button'
import { MonoDigits } from '../../components/atoms/Mono'
import { formatElapsedSince } from '../../lib/format'
import type { ActiveIncidentRecord, TargetRecord } from '../../lib/types'
import { isCoverageGapTarget } from '../targets/targetHelpers'

type NoticeTone = ObservabilityTone

function NoticeRow({
  tone,
  mark,
  title,
  detail,
  action,
  role = 'status',
}: {
  tone: NoticeTone
  mark: string
  title: string
  detail?: ReactNode
  action?: ReactNode
  role?: 'status' | 'alert'
}) {
  return (
    <ObservabilityNotice
      tone={tone}
      mark={mark}
      title={title}
      detail={detail}
      action={action}
      role={role}
    />
  )
}

type Props = {
  target: TargetRecord
  incidents: ActiveIncidentRecord[]
  incidentsError?: string | null
  incidentsRetrying?: boolean
  onRetryIncidents?: () => void
  runtimeError: string | null
  onOpenEvents: () => void
}

export function TargetDetailNotices({
  target,
  incidents,
  incidentsError = null,
  incidentsRetrying = false,
  onRetryIncidents,
  runtimeError,
  onOpenEvents,
}: Props) {
  const rows: Array<{ key: string; node: ReactNode }> = []
  const firstIncident = [...incidents].sort(
    (a, b) => new Date(a.started_at).getTime() - new Date(b.started_at).getTime(),
  )[0]
  const health = target.current_health_status
  if (target.current_active_incident_count > 0) {
    const tone: NoticeTone = health === '严重' ? 'critical' : health === '关注' ? 'notice' : 'alert'
    const mark = health === '严重' || health === '关注' || health === '告警' ? health : '告警'
    rows.push({
      key: 'incidents',
      node: (
        <NoticeRow
          tone={tone}
          mark={mark}
          title={target.current_primary_issue_summary || '存在活跃异常'}
          detail={
            <>
              活跃 <MonoDigits>{target.current_active_incident_count}</MonoDigits>
              {firstIncident ? (
                <>
                  {' · 已持续 '}
                  <MonoDigits>{formatElapsedSince(firstIncident.started_at)}</MonoDigits>
                </>
              ) : null}
            </>
          }
          action={<Button variant="ghost" size="sm" onClick={onOpenEvents}>查看事件</Button>}
          role="alert"
        />
      ),
    })
  }
  if (target.run_status === '维护中') {
    rows.push({
      key: 'maintenance',
      node: (
        <NoticeRow
          tone="maintenance"
          mark="维护中"
          title="探测空窗应按维护上下文解读"
          detail="观测可以继续，异常通知已抑制"
        />
      ),
    })
  } else if (target.run_status === '暂停') {
    rows.push({
      key: 'pause',
      node: (
        <NoticeRow
          tone="offline"
          mark="暂停"
          title="入口探测已暂停"
          detail="不会产生新的探测观测或通知"
        />
      ),
    })
  }
  if (isCoverageGapTarget(target) && target.current_active_incident_count === 0) {
    rows.push({
      key: 'coverage',
      node: (
        <NoticeRow
          tone="notice"
          mark="覆盖缺口"
          title="缺少执行监控实例标签"
          detail="探测覆盖边界不明确"
        />
      ),
    })
  }
  if (incidentsError) {
    rows.push({
      key: 'incidents-error',
      node: (
        <NoticeRow
          tone="alert"
          mark="加载失败"
          title="活跃异常暂不可用"
          detail={incidentsError}
          action={
            onRetryIncidents ? (
              <Button
                variant="ghost"
                size="sm"
                disabled={incidentsRetrying}
                onClick={onRetryIncidents}
                aria-label="重试加载活跃异常"
              >
                {incidentsRetrying ? '重试中…' : '重试'}
              </Button>
            ) : null
          }
          role="alert"
        />
      ),
    })
  }
  if (runtimeError) {
    rows.push({
      key: 'runtime',
      node: (
        <NoticeRow
          tone="alert"
          mark="运行控制"
          title={runtimeError}
          role="alert"
        />
      ),
    })
  }
  if (rows.length === 0) return null
  return (
    <div className="observability-notices monitoring-detail-notices">
      {rows.map((row) => (
        <div key={row.key}>{row.node}</div>
      ))}
    </div>
  )
}
