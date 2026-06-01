import { Link } from 'react-router-dom'

import { PageState } from '../../components/PageState'

type MonitoringDetailUnavailableProps = {
  message: string
}

export function MonitoringDetailUnavailable({ message }: MonitoringDetailUnavailableProps) {
  return (
    <PageState
      kind="error"
      eyebrow="监控实例详情"
      title="监控实例详情不可用"
      description={message}
      technicalSummary={message}
      action={
        <Link className="text-link" to="/monitoring">
          返回监控实例列表
        </Link>
      }
    />
  )
}
