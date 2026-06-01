import {
  DataTable,
  Hostname,
  MonoDigits,
  StatusGlyph,
  type DataTableColumn,
  type HealthState,
} from '../../components/atoms'
import type { ContainerInfo, HostSample } from '../../lib/types'

type MonitoringInstanceContainersSectionProps = {
  sample: HostSample | null
}

export function MonitoringInstanceContainersSection({ sample }: MonitoringInstanceContainersSectionProps) {
  const containerColumns: DataTableColumn<ContainerInfo>[] = [
    {
      key: 'status',
      label: '状态',
      width: 60,
      render: (container) => {
        const state: HealthState =
          container.status === 'running'
            ? 'normal'
            : container.status === 'exited'
              ? 'offline'
              : 'notice'
        return <StatusGlyph state={state} size="sm" ariaLabel={container.status} />
      },
    },
    {
      key: 'name',
      label: '容器名',
      render: (container) => <Hostname>{container.name}</Hostname>,
    },
    {
      key: 'image',
      label: 'Image',
      render: (container) => (
        <span className="watchtower-container-image">{container.image}</span>
      ),
    },
    {
      key: 'cpu',
      label: 'CPU%',
      align: 'right',
      width: 72,
      cellClassName: 'mono',
      render: (container) =>
        container.cpu_pct != null ? <MonoDigits>{container.cpu_pct.toFixed(1)}%</MonoDigits> : '—',
    },
    {
      key: 'mem',
      label: 'Mem%',
      align: 'right',
      width: 72,
      cellClassName: 'mono',
      render: (container) =>
        container.mem_pct != null ? <MonoDigits>{container.mem_pct.toFixed(1)}%</MonoDigits> : '—',
    },
  ]

  return (
    <>
      {sample?.containers && sample.containers.length > 0 ? (
        <DataTable
          density="compact"
          columns={containerColumns}
          rows={sample.containers}
          rowKey={(container) => container.id}
          emptyContent="暂无容器数据"
        />
      ) : (
        <p className="empty-inline">暂无容器数据</p>
      )}
    </>
  )
}
