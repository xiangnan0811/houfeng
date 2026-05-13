import { Link } from 'react-router-dom'

import { PageState } from '../../components/PageState'

type TargetDetailUnavailableProps = {
  error: string | null
}

export function TargetDetailUnavailable({ error }: TargetDetailUnavailableProps) {
  const message = error ?? '未找到目标'
  return (
    <PageState
      kind="error"
      eyebrow="目标详情"
      title="目标详情不可用"
      description={message}
      technicalSummary={error}
      action={
        <Link className="text-link" to="/targets">
          返回目标列表
        </Link>
      }
    />
  )
}
