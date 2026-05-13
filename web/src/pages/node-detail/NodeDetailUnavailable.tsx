import { Link } from 'react-router-dom'

import { PageState } from '../../components/PageState'

type NodeDetailUnavailableProps = {
  message: string
}

export function NodeDetailUnavailable({ message }: NodeDetailUnavailableProps) {
  return (
    <PageState
      kind="error"
      eyebrow="节点详情"
      title="节点详情不可用"
      description={message}
      technicalSummary={message}
      action={
        <Link className="text-link" to="/nodes">
          返回节点列表
        </Link>
      }
    />
  )
}
