import { Badge, DataTable, MonoDigits } from '../../components/atoms'
import { formatDate, formatDateTime, formatOptional } from '../../lib/format'
import {
  ASSET_DOMAIN_STATUS_LABELS,
  ASSET_SERVICE_STATUS_LABELS,
  ASSET_SERVICE_TYPE_LABELS,
  type AssetDomainRecord,
  type AssetServiceRecord,
  type VPSMonitoringInstanceSummary,
} from '../../lib/types'

export function ArchiveMonitoringPanel({ monitoring }: { monitoring: VPSMonitoringInstanceSummary[] }) {
  return (
    <section className="page-panel archive-page__readonly-panel">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">MONITORING HISTORY</p>
          <h2 className="section-heading__title">监控关联</h2>
          <p className="section-heading__description">只读展示归档前保留在 VPS 台账里的监控实例证据。</p>
        </div>
        <span className="section-heading__meta">
          <MonoDigits>{monitoring.length}</MonoDigits> 个关联
        </span>
      </div>
      <DataTable
        className="archive-page__monitoring-table"
        rows={monitoring}
        rowKey={(item) => item.monitoring_instance_id}
        emptyContent={<span className="empty-inline">尚未关联监控实例</span>}
        columns={[
          {
            key: 'identity',
            label: '监控实例',
            width: '220px',
            render: (item) => (
              <div className="asset-table__identity">
                <strong>{item.display_name}</strong>
                <small>{item.monitoring_instance_id}</small>
              </div>
            ),
          },
          {
            key: 'status',
            label: '状态',
            width: '168px',
            render: (item) => (
              <div className="badge-row badge-row--wrap">
                <Badge variant="info" tone="neutral">{item.lifecycle_status || '未知'}</Badge>
                <Badge variant="info" tone="neutral">{item.monitoring_status || '未知'}</Badge>
              </div>
            ),
          },
          {
            key: 'health',
            label: '历史健康',
            width: '180px',
            render: (item) => (
              <div className="asset-table__stack">
                <strong>{item.current_health_status || '—'}</strong>
                <small>{formatOptional(item.current_primary_issue_summary)}</small>
              </div>
            ),
          },
          {
            key: 'linked_at',
            label: '关联时间',
            width: '156px',
            render: (item) => <MonoDigits>{formatDateTime(item.linked_at)}</MonoDigits>,
          },
        ]}
      />
    </section>
  )
}

export function ArchiveServicesPanel({ services }: { services: AssetServiceRecord[] }) {
  return (
    <section className="page-panel page-panel--scroll-x archive-page__readonly-panel">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">SERVICE CONTEXT</p>
          <h2 className="section-heading__title">服务资产</h2>
          <p className="section-heading__description">只读保留归档 VPS 的服务记录和 Target 关联。</p>
        </div>
        <span className="section-heading__meta">
          <MonoDigits>{services.length}</MonoDigits> 个服务
        </span>
      </div>
      <DataTable
        className="archive-page__service-table"
        rows={services}
        rowKey={(service) => service.service_id}
        emptyContent={<span className="empty-inline">尚未记录服务</span>}
        columns={[
          {
            key: 'service',
            label: '服务',
            width: '220px',
            render: (service) => (
              <div className="asset-table__identity">
                <strong>{service.name}</strong>
                <small>{service.service_id}</small>
              </div>
            ),
          },
          {
            key: 'type',
            label: '类型 / 状态',
            width: '164px',
            render: (service) => (
              <div className="badge-row badge-row--wrap">
                <Badge variant="info" tone="neutral">
                  {ASSET_SERVICE_TYPE_LABELS[service.service_type] ?? service.service_type}
                </Badge>
                <Badge variant="state" tone={service.status === 'active' ? 'normal' : 'offline'}>
                  {ASSET_SERVICE_STATUS_LABELS[service.status] ?? service.status}
                </Badge>
              </div>
            ),
          },
          {
            key: 'endpoint',
            label: '入口',
            width: '220px',
            render: (service) => (
              <div className="asset-table__stack">
                <strong>{formatOptional(service.url)}</strong>
                <small>{service.port ? `端口 ${service.port}` : '端口未记录'}</small>
              </div>
            ),
          },
          {
            key: 'target',
            label: 'Target',
            width: '140px',
            render: (service) => <span className="asset-table__muted">{service.target_id || '未关联'}</span>,
          },
        ]}
      />
    </section>
  )
}

export function ArchiveDomainsPanel({ domains }: { domains: AssetDomainRecord[] }) {
  return (
    <section className="page-panel page-panel--scroll-x archive-page__readonly-panel">
      <div className="section-heading">
        <div>
          <p className="section-heading__eyebrow">DOMAIN CONTEXT</p>
          <h2 className="section-heading__title">域名资产</h2>
          <p className="section-heading__description">只读保留归档 VPS 的域名、证书和 Target 关联。</p>
        </div>
        <span className="section-heading__meta">
          <MonoDigits>{domains.length}</MonoDigits> 个域名
        </span>
      </div>
      <DataTable
        className="archive-page__domain-table"
        rows={domains}
        rowKey={(domain) => domain.domain_id}
        emptyContent={<span className="empty-inline">尚未记录域名</span>}
        columns={[
          {
            key: 'domain',
            label: '域名',
            width: '220px',
            render: (domain) => (
              <div className="asset-table__identity">
                <strong>{domain.domain_name}</strong>
                <small>{domain.domain_id}</small>
              </div>
            ),
          },
          {
            key: 'status',
            label: '状态 / HTTPS',
            width: '164px',
            render: (domain) => (
              <div className="badge-row badge-row--wrap">
                <Badge variant="state" tone={domain.status === 'active' ? 'normal' : 'offline'}>
                  {ASSET_DOMAIN_STATUS_LABELS[domain.status] ?? domain.status}
                </Badge>
                <Badge variant="info" tone={domain.https_enabled ? 'normal' : 'neutral'}>
                  {domain.https_enabled ? 'HTTPS' : '未记录 HTTPS'}
                </Badge>
              </div>
            ),
          },
          {
            key: 'purpose',
            label: '用途 / 到期',
            width: '180px',
            render: (domain) => (
              <div className="asset-table__stack">
                <strong>{formatOptional(domain.purpose)}</strong>
                <small>{formatDate(domain.expires_at)}</small>
              </div>
            ),
          },
          {
            key: 'target',
            label: 'Target',
            width: '140px',
            render: (domain) => <span className="asset-table__muted">{domain.target_id || '未关联'}</span>,
          },
        ]}
      />
    </section>
  )
}
