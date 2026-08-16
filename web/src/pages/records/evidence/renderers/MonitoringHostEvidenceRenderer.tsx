import type { MonitoringEvidenceReadModel } from '../evidenceReadModels'
import { MonitoringEvidenceRenderer } from './MonitoringEvidenceRenderer'

type Props = {
  model: MonitoringEvidenceReadModel
}

export function MonitoringHostEvidenceRenderer({ model }: Props) {
  return <MonitoringEvidenceRenderer model={model} title="主机监控趋势" />
}
