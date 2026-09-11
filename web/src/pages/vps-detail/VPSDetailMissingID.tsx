import { Button } from '../../components/atoms'

type VPSDetailMissingIDProps = {
  onBack: () => void
}

export function VPSDetailMissingID({ onBack }: VPSDetailMissingIDProps) {
  return (
    <div className="page asset-page vps-detail-page vps-detail-workspace">
      <section className="page-panel page-panel--inline">
        <div>
          <div className="page-panel__eyebrow">VPS 详情</div>
          <h1 className="page-panel__title">VPS 详情不可用</h1>
          <p className="page-panel__description">缺少 VPS ID</p>
        </div>
        <div className="page-panel__actions">
          <Button variant="secondary" onClick={onBack}>返回</Button>
        </div>
      </section>
    </div>
  )
}
