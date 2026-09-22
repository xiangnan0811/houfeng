import { Link } from 'react-router-dom'

import { ObservabilityNoticeRow, type ObservabilityTone } from '../../components/observability'
import { Button } from '../../components/atoms/Button'
import { Modal, TabPanel, Tabs } from '../../components/atoms'
import { PageState } from '../../components/PageState'
import { formatElapsedSince } from '../../lib/format'
import { incidentClassLabel, severityTone } from '../../lib/observabilityLabels'
import { STATE_CHANGE_EVENT_TYPE_LABELS, type ActiveIncidentRecord, type StateChangeEventRecord, type TargetRecord } from '../../lib/types'
import { HISTORY_TAB_ITEMS } from './targetDetailConstants'
import type { HistoryTab } from './types'

type TargetHistoryDrawerProps = {
  target: TargetRecord
  open: boolean
  tab: HistoryTab
  events: StateChangeEventRecord[]
  eventsError: string | null
  onRetryEvents?: () => void
  historyIncidents: ActiveIncidentRecord[] | null
  historyIncidentsLoading: boolean
  historyIncidentsError: string | null
  onClose: () => void
  onTabChange: (tab: HistoryTab) => void
  onRetryHistoryIncidents: () => void
}

export function TargetHistoryDrawer({
  target,
  open,
  tab,
  events,
  eventsError,
  onRetryEvents,
  historyIncidents,
  historyIncidentsLoading,
  historyIncidentsError,
  onClose,
  onTabChange,
  onRetryHistoryIncidents,
}: TargetHistoryDrawerProps) {
  return (
    <Modal
      open={open}
      onClose={onClose}
      title={`${target.name} · 历史`}
      ariaLabel="目标历史抽屉"
      size="md"
    >
      <div className="monitoring-detail-history">
        <Tabs<HistoryTab>
          label="目标历史类型"
          idBase="target-history"
          variant="pill"
          value={tab}
          onChange={onTabChange}
          items={HISTORY_TAB_ITEMS}
        />
        <TabPanel idBase="target-history" value={tab}>
          {tab === 'events' ? (
            eventsError ? (
              <PageState
                kind="error"
                title="事件时间线暂不可用"
                description={eventsError}
                surface="empty"
                compact
                action={onRetryEvents ? <Button variant="secondary" size="sm" onClick={onRetryEvents}>重试</Button> : null}
              />
            ) : events.length === 0 ? (
              <PageState
                kind="empty"
                title="近期无状态变更事件"
                description="该目标近期没有发生过被记录的状态变更事件。"
                surface="empty"
                compact
              />
            ) : (
              <ul className="monitoring-detail-history__list">
                {events.map((event, index) => (
                  <HistoryRow
                    key={event.event_id ?? `${event.created_at}-${index}`}
                    mark={event.severity || '事件'}
                    tone={severityTone(event.severity)}
                    title={STATE_CHANGE_EVENT_TYPE_LABELS[event.event_type] ?? event.event_type}
                    detail={event.summary || '暂无摘要'}
                    meta={incidentClassLabel(event.incident_class)}
                    time={event.created_at}
                  />
                ))}
              </ul>
            )
          ) : historyIncidentsLoading ? (
            <PageState kind="loading" title="正在加载历史异常" surface="empty" compact />
          ) : historyIncidentsError ? (
            <PageState
              kind="error"
              title="历史异常暂不可用"
              description={historyIncidentsError}
              surface="empty"
              compact
              action={<Button variant="secondary" size="sm" onClick={onRetryHistoryIncidents}>重试</Button>}
            />
          ) : historyIncidents && historyIncidents.length > 0 ? (
            <ul className="monitoring-detail-history__list">
              {historyIncidents.map((incident) => (
                <HistoryRow
                  key={incident.incident_id}
                  mark={incident.severity}
                  tone={severityTone(incident.severity)}
                  title={incident.source_summary || incidentClassLabel(incident.incident_class) || '异常'}
                  detail={incidentClassLabel(incident.incident_class)}
                  meta={`已持续 ${formatElapsedSince(incident.started_at)}`}
                  time={incident.last_evaluated_at}
                  timeLabel="最近评估"
                />
              ))}
            </ul>
          ) : (
            <PageState
              kind="empty"
              title="近期无异常发生"
              description="该目标近期没有触发过被记录的异常。"
              surface="empty"
              compact
            />
          )}
        </TabPanel>
        <p className="monitoring-detail-history__footer">
          <Link
            className="text-link"
            to={`/events?object_type=target&object_id=${encodeURIComponent(target.target_id)}`}
          >
            在事件流中查看
          </Link>
        </p>
      </div>
    </Modal>
  )
}

function HistoryRow({
  mark,
  tone,
  title,
  detail,
  meta,
  time,
  timeLabel,
}: {
  mark: string
  tone: ObservabilityTone
  title: string
  detail?: string
  meta?: string
  time?: string
  timeLabel?: string
}) {
  const shownDetail = detail && detail !== title ? detail : ''
  const shownMeta = meta && meta !== shownDetail && meta !== title ? meta : ''
  return (
    <ObservabilityNoticeRow
      tone={tone}
      mark={mark}
      title={title}
      detail={shownDetail || undefined}
      meta={shownMeta || undefined}
      time={time}
      timeLabel={timeLabel}
    />
  )
}
