import { Button } from '../../components/atoms/Button'
import { Modal, TabPanel, Tabs, Timestamp } from '../../components/atoms'
import { PageState } from '../../components/PageState'
import { formatElapsedSince } from '../../lib/format'
import { incidentClassLabel, severityTone } from '../../lib/observabilityLabels'
import { STATE_CHANGE_EVENT_TYPE_LABELS, type ActiveIncidentRecord, type MonitoringInstanceRecord, type StateChangeEventRecord } from '../../lib/types'
import { HISTORY_TAB_ITEMS } from './monitoringDetailConstants'
import type { HistoryTab } from './types'

type MonitoringInstanceHistoryDrawerProps = {
  monitoringInstance: MonitoringInstanceRecord
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

export function MonitoringInstanceHistoryDrawer({
  monitoringInstance,
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
}: MonitoringInstanceHistoryDrawerProps) {
  return (
    <Modal
      open={open}
      onClose={onClose}
      title={`${monitoringInstance.display_name} · 历史`}
      ariaLabel="监控实例历史抽屉"
      size="md"
    >
      <div className="monitoring-detail-history">
        <Tabs<HistoryTab>
          label="监控实例历史类型"
          idBase="monitoring-history"
          variant="pill"
          value={tab}
          onChange={onTabChange}
          items={HISTORY_TAB_ITEMS}
        />
        <TabPanel idBase="monitoring-history" value={tab}>
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
                description="该监控实例近期没有发生过被记录的状态变更事件。"
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
              description="该监控实例近期没有触发过被记录的异常。"
              surface="empty"
              compact
            />
          )}
        </TabPanel>
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
  tone: string
  title: string
  detail?: string
  meta?: string
  time?: string
  timeLabel?: string
}) {
  const shownDetail = detail && detail !== title ? detail : ''
  const shownMeta = meta && meta !== shownDetail && meta !== title ? meta : ''
  return (
    <li className={`monitoring-detail-history__row monitoring-detail-history__row--${tone}`}>
      <div className="monitoring-detail-history__copy">
        <span className="monitoring-detail-history__mark">{mark}</span>
        <span className="monitoring-detail-history__title">{title}</span>
        {shownDetail ? <span className="monitoring-detail-history__detail">{shownDetail}</span> : null}
      </div>
      <p className="monitoring-detail-history__meta">
        {shownMeta ? `${shownMeta} · ` : null}
        {timeLabel ? `${timeLabel} ` : null}
        {time ? <Timestamp value={time} mode="absolute" /> : null}
      </p>
    </li>
  )
}
