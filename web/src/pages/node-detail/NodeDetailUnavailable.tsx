import { Link } from 'react-router-dom'

type NodeDetailUnavailableProps = {
  message: string
}

export function NodeDetailUnavailable({ message }: NodeDetailUnavailableProps) {
  return (
    <section className="page-panel">
      <p className="page-panel__eyebrow">节点详情</p>
      <h2 className="page-panel__title">节点详情不可用</h2>
      <p className="page-panel__description">{message}</p>
      <Link className="text-link" to="/nodes">
        返回节点列表
      </Link>
    </section>
  )
}
