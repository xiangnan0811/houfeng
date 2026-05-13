import { PageState } from '../../components/PageState'

export function VPSDetailLoading() {
  return (
    <div className="page-stack asset-page vps-detail-page">
      <PageState kind="loading" title="正在加载 VPS 详情…" />
    </div>
  )
}
