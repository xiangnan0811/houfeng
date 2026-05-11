import { Link } from 'react-router-dom'

type TargetDetailUnavailableProps = {
  error: string | null
}

export function TargetDetailUnavailable({ error }: TargetDetailUnavailableProps) {
  return (
    <section className="page-panel">
      <p className="page-panel__eyebrow">目标详情</p>
      <h2 className="page-panel__title">目标详情不可用</h2>
      <p className="page-panel__description">{error ?? '未找到目标'}</p>
      <Link className="text-link" to="/targets">
        返回目标列表
      </Link>
    </section>
  )
}
