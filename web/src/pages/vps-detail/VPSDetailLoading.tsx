import { PageState } from '../../components/PageState'

export function VPSDetailLoading() {
  return (
    <div className="page asset-page vps-detail-page vps-detail-workspace">
      <PageState kind="loading" title="正在加载 VPS 详情…" />
    </div>
  )
}
