import { Button } from '../../components/atoms'

type VPSDetailErrorPanelProps = {
  error: string | null
  onBack: () => void
}

export function VPSDetailErrorPanel({ error, onBack }: VPSDetailErrorPanelProps) {
  return (
    <div className="page-stack asset-page vps-detail-page">
      <section className="page-panel page-panel--inline">
        <div>
          <div className="page-panel__eyebrow">VPS DETAIL</div>
          <h1 className="page-panel__title">VPS 详情不可用</h1>
          <p className="page-panel__description">{error ?? 'VPS 不存在'}</p>
        </div>
        <div className="page-panel__actions">
          <Button variant="secondary" onClick={onBack}>返回</Button>
        </div>
      </section>
    </div>
  )
}
