import {
  decodeCommandAuditEvidenceReadModel,
  decodeIPQualityEvidenceReadModel,
  decodeMonitoringEventEvidenceReadModel,
  decodeMonitoringEvidenceReadModel,
  decodeSubscriptionCostEvidenceReadModel,
  type CommandAuditEvidenceReadModel,
  type IPQualityEvidenceReadModel,
  type MonitoringEventEvidenceReadModel,
  type MonitoringEvidenceReadModel,
  type SubscriptionCostEvidenceReadModel,
} from './evidenceReadModels'
import { createEvidenceRendererRegistry } from './evidenceRendererRegistryCore'
import { CommandAuditEvidenceRenderer } from './renderers/CommandAuditEvidenceRenderer'
import { IPQualityEvidenceRenderer } from './renderers/IPQualityEvidenceRenderer'
import { MonitoringEventEvidenceRenderer } from './renderers/MonitoringEventEvidenceRenderer'
import { MonitoringHostEvidenceRenderer } from './renderers/MonitoringHostEvidenceRenderer'
import { MonitoringProbeEvidenceRenderer } from './renderers/MonitoringProbeEvidenceRenderer'
import { SubscriptionCostEvidenceRenderer } from './renderers/SubscriptionCostEvidenceRenderer'

export const EvidenceRendererRegistry = createEvidenceRendererRegistry([
  {
    kind: 'ip_quality.report',
    schema_version: 1,
    renderer_version: 'ip_quality_report_v1',
    read_model_version: 'ip_quality_report_read_model/v1',
    decode: decodeIPQualityEvidenceReadModel,
    render: (model) => <IPQualityEvidenceRenderer model={model as IPQualityEvidenceReadModel} />,
  },
  {
    kind: 'monitoring.host',
    schema_version: 1,
    renderer_version: 'monitoring_host_v1',
    read_model_version: 'monitoring_host_read_model/v1',
    decode: (value) => decodeMonitoringEvidenceReadModel(value, 'monitoring_host_read_model/v1'),
    render: (model) => <MonitoringHostEvidenceRenderer model={model as MonitoringEvidenceReadModel} />,
  },
  {
    kind: 'monitoring.probe',
    schema_version: 2,
    renderer_version: 'monitoring_probe_v2',
    read_model_version: 'monitoring_probe_read_model/v1',
    decode: (value) => decodeMonitoringEvidenceReadModel(value, 'monitoring_probe_read_model/v1'),
    render: (model) => <MonitoringProbeEvidenceRenderer model={model as MonitoringEvidenceReadModel} />,
  },
  {
    kind: 'monitoring.event',
    schema_version: 2,
    renderer_version: 'monitoring_event_v2',
    read_model_version: 'monitoring_event_read_model/v2',
    decode: decodeMonitoringEventEvidenceReadModel,
    render: (model) => <MonitoringEventEvidenceRenderer model={model as MonitoringEventEvidenceReadModel} />,
  },
  {
    kind: 'subscription.cost',
    schema_version: 1,
    renderer_version: 'subscription_cost_v1',
    read_model_version: 'subscription_cost_read_model/v1',
    decode: decodeSubscriptionCostEvidenceReadModel,
    render: (model) => <SubscriptionCostEvidenceRenderer model={model as SubscriptionCostEvidenceReadModel} />,
  },
  {
    kind: 'command.audit',
    schema_version: 1,
    renderer_version: 'command_audit_v1',
    read_model_version: 'command_audit_read_model/v1',
    decode: decodeCommandAuditEvidenceReadModel,
    render: (model) => <CommandAuditEvidenceRenderer model={model as CommandAuditEvidenceReadModel} />,
  },
])
