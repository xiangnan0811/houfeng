import type { MonitoringEvidenceReadModel } from '../evidenceReadModels'
import { MonitoringEvidenceRenderer } from './MonitoringEvidenceRenderer'

type Props = {
  model: MonitoringEvidenceReadModel
}

export function MonitoringProbeEvidenceRenderer({ model }: Props) {
  return <MonitoringEvidenceRenderer model={model} title="探针监控趋势" />
}
