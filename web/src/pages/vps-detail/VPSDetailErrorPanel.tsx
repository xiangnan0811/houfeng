import { Button } from '../../components/atoms'
import { PageState } from '../../components/PageState'

type VPSDetailErrorPanelProps = {
  error: string | null
  onBack: () => void
}

export function VPSDetailErrorPanel({ error, onBack }: VPSDetailErrorPanelProps) {
  return (
    <div className="page-stack asset-page vps-detail-page">
      <PageState
        kind="error"
        eyebrow="VPS DETAIL"
        title="VPS 详情不可用"
        description={error ?? 'VPS 不存在'}
        technicalSummary={error}
        action={<Button variant="secondary" onClick={onBack}>返回</Button>}
      />
    </div>
  )
}
