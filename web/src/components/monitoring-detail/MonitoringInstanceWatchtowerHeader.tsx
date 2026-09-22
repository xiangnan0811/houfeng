import type { ReactNode } from 'react'
import { Link, useLocation, useSearchParams } from 'react-router-dom'

import { Hostname } from '../atoms'
import { Button } from '../atoms/Button'
import type { MonitoringInstanceRecord, VPSSummary } from '../../lib/types'
import {
  formatMonitoringInstanceLocation,
  formatMonitoringInstanceProvider,
  validateReturnVPSId,
} from '../../pages/monitoring-detail/monitoringDetailHelpers'

export type MonitoringInstanceRuntimeAction =
  | 'enter-maintenance'
  | 'exit-maintenance'
  | 'pause'
  | 'resume'

type Props = {
  monitoringInstance: MonitoringInstanceRecord
  readOnly?: boolean
  linkedVPS: VPSSummary[]
  linkedVPSLoading: boolean
  linkedVPSLoaded: boolean
  linkedVPSError: string | null
  onRetryLinkedVPS: () => void
  returnVPSId?: string | null
  /** Refresh / management / onboarding entry points, rendered on the right. */
  actions?: ReactNode
  /** Runtime facts (health, heartbeat, sample) sit under the actions. */
  aside?: ReactNode
}

const IDENTITY_LABEL_LIMIT = 3

function identityLabelText(labels: string[]): { text: string; title: string } {
  const full = labels.join(' · ')
  if (labels.length <= IDENTITY_LABEL_LIMIT) return { text: full, title: full }
  return {
    text: `${labels.slice(0, IDENTITY_LABEL_LIMIT).join(' · ')} 等 ${labels.length} 个`,
    title: full,
  }
}

function linkedVPSIdentity(
  linkedVPS: VPSSummary[],
  loading: boolean,
  loaded: boolean,
  error: string | null,
  navigationState: unknown,
  returnVPSId: string | null,
  onRetry: () => void,
) {
  // The source VPS is already reachable through the single link, so the extra
  // "返回来源 VPS" shortcut would be a redundant second link to the same page.
  const sourceReachableThroughLink =
    !loading &&
    !error &&
    loaded &&
    linkedVPS.length === 1 &&
    Boolean(linkedVPS[0]?.vps_id) &&
    linkedVPS[0]?.vps_id === returnVPSId

  const returnAction =
    returnVPSId && !sourceReachableThroughLink ? (
      <>
        <span aria-hidden="true"> · </span>
        <Link
          className="text-link"
          to={`/vps/${encodeURIComponent(returnVPSId)}`}
          state={navigationState}
        >
          返回来源 VPS
        </Link>
      </>
    ) : null

  if (loading && !loaded) {
    return <span data-testid="linked-vps-loading">加载中{returnAction}</span>
  }

  if (error) {
    return (
      <span data-testid="linked-vps-error">
        未同步
        <Button variant="ghost" size="sm" onClick={onRetry}>重试</Button>
        {returnAction}
      </span>
    )
  }

  if (!loaded) {
    return <span data-testid="linked-vps-loading">加载中{returnAction}</span>
  }

  if (linkedVPS.length === 0) {
    return (
      <span data-testid="linked-vps-unlinked">
        <Link className="text-link" to="/vps?view=unlinked">未关联</Link>
        {returnAction}
      </span>
    )
  }

  if (linkedVPS.length === 1) {
    const vps = linkedVPS[0]
    if (!vps?.vps_id || !vps.display_name) {
      return (
        <span data-testid="linked-vps-error">
          未同步
          <Button variant="ghost" size="sm" onClick={onRetry}>重试</Button>
          {returnAction}
        </span>
      )
    }
    return (
      <span data-testid="linked-vps-single">
        <Link
          className="text-link"
          to={`/vps/${encodeURIComponent(vps.vps_id)}`}
          state={navigationState}
        >
          {vps.display_name}
        </Link>
        {returnAction}
      </span>
    )
  }

  const first = linkedVPS[0]
  const second = linkedVPS[1]
  const linkable = [first, second].filter(
    (vps): vps is VPSSummary => Boolean(vps?.vps_id && vps.display_name),
  )
  return (
    <span data-testid="linked-vps-many">
      {linkable.map((vps, index) => (
        <span key={vps.vps_id}>
          {index > 0 ? ' · ' : null}
          <Link
            className="text-link"
            to={`/vps/${encodeURIComponent(vps.vps_id)}`}
            state={navigationState}
          >
            {vps.display_name}
          </Link>
        </span>
      ))}
      {` 等 ${linkedVPS.length} 台`}
      {returnAction}
    </span>
  )
}

export function MonitoringInstanceWatchtowerHeader({
  monitoringInstance,
  readOnly = false,
  linkedVPS,
  linkedVPSLoading,
  linkedVPSLoaded,
  linkedVPSError,
  onRetryLinkedVPS,
  returnVPSId,
  actions,
  aside,
}: Props) {
  const location = useLocation()
  const [searchParams] = useSearchParams()
  const effectiveReturnVPSId =
    returnVPSId !== undefined ? returnVPSId : validateReturnVPSId(searchParams.get('return_vps'))

  const labels = Array.isArray(monitoringInstance.labels) ? monitoringInstance.labels : []
  const labelContent = identityLabelText(labels)
  const provider = formatMonitoringInstanceProvider(monitoringInstance.provider)
  const locationText = formatMonitoringInstanceLocation(
    monitoringInstance.region,
    monitoringInstance.city,
  )
  const note = (monitoringInstance.note ?? '').trim()

  return (
    <header
      className="monitoring-detail-header"
      role="banner"
      aria-label="监控实例身份与操作"
    >
      <div className="monitoring-detail-header__identity">
        <div className="monitoring-detail-header__title-row">
          <h1 className="monitoring-detail-header__title">{monitoringInstance.display_name}</h1>
          {readOnly ? <span className="monitoring-detail-readonly">只读预览</span> : null}
        </div>
        <dl className="monitoring-detail-identity">
          <div className="monitoring-detail-identity__item">
            <dt>关联</dt>
            <dd>
              {linkedVPSIdentity(
                linkedVPS,
                linkedVPSLoading,
                linkedVPSLoaded,
                linkedVPSError,
                location.state,
                effectiveReturnVPSId,
                onRetryLinkedVPS,
              )}
            </dd>
          </div>
          {provider ? (
            <div className="monitoring-detail-identity__item">
              <dt>服务商</dt>
              <dd>{provider}</dd>
            </div>
          ) : null}
          {locationText ? (
            <div className="monitoring-detail-identity__item">
              <dt>位置</dt>
              <dd>{locationText}</dd>
            </div>
          ) : null}
          {monitoringInstance.group ? (
            <div className="monitoring-detail-identity__item">
              <dt>分组</dt>
              <dd>{monitoringInstance.group}</dd>
            </div>
          ) : null}
          {labels.length > 0 ? (
            <div className="monitoring-detail-identity__item">
              <dt>标签</dt>
              <dd title={labelContent.title}>{labelContent.text}</dd>
            </div>
          ) : null}
          {note ? (
            <div className="monitoring-detail-identity__item monitoring-detail-identity__item--note">
              <dt>备注</dt>
              <dd className="monitoring-detail-identity__note" title={note}>{note}</dd>
            </div>
          ) : null}
          <div className="monitoring-detail-identity__item">
            <dt>ID</dt>
            <dd className="monitoring-detail-identity__id">
              <Hostname>{monitoringInstance.monitoring_instance_id}</Hostname>
            </dd>
          </div>
        </dl>
      </div>
      {actions || aside ? (
        <div className="monitoring-detail-header__end">
          {actions ? <div className="monitoring-detail-header__actions">{actions}</div> : null}
          {aside}
        </div>
      ) : null}
    </header>
  )
}
