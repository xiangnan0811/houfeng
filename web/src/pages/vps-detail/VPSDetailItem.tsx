import { formatOptional } from '../../lib/format'

type VPSDetailItemProps = {
  label: string
  value: string | number | null | undefined
}

export function VPSDetailItem({ label, value }: VPSDetailItemProps) {
  return (
    <div className="asset-detail-grid__item">
      <dt>{label}</dt>
      <dd>{formatOptional(value)}</dd>
    </div>
  )
}
