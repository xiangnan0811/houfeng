import { Link, useLocation } from 'react-router-dom'

import { PageState } from '../../components/PageState'
import { resolveMonitoringListHref } from '../monitoring/monitoringListUrl'

type MonitoringDetailUnavailableProps = {
  message: string
  returnVPSId?: string | null
}

export function MonitoringDetailUnavailable({ message, returnVPSId = null }: MonitoringDetailUnavailableProps) {
  const location = useLocation()
  const listHref = resolveMonitoringListHref(location.state)

  return (
    <PageState
      kind="error"
      eyebrow="监控实例详情"
      title="监控实例详情不可用"
      description={message}
      technicalSummary={message}
      action={
        returnVPSId ? (
          <>
            <Link
              className="text-link"
              to={`/vps/${encodeURIComponent(returnVPSId)}`}
              state={location.state}
            >
              返回来源 VPS
            </Link>
            <Link className="text-link" to={listHref} state={location.state}>
              返回监控实例列表
            </Link>
          </>
        ) : (
          <Link className="text-link" to={listHref} state={location.state}>
            返回监控实例列表
          </Link>
        )
      }
    />
  )
}
