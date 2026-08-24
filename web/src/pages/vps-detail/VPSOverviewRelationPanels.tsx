import { useEffect, useRef, useState } from 'react'

import { Button, Modal } from '../../components/atoms'
import {
  listVPSDomains,
  listVPSMonitoringInstances,
  listVPSServices,
} from '../../lib/api'
import type {
  AssetDomainRecord,
  AssetServiceRecord,
  VPSMonitoringInstanceSummary,
} from '../../lib/types'
import { VPSDomainsSection } from './VPSDomainsSection'
import type { VPSManagementController, VPSManagementPanel } from './hooks/useVPSManagementController'
import { VPSMonitoringInstanceLinksSection } from './VPSMonitoringInstanceLinksSection'
import { VPSServicesSection } from './VPSServicesSection'
import { describeManagementError } from './vpsManagementHelpers'

type RelationPanel = Extract<
  VPSManagementPanel,
  'monitoring-instance-evidence' | 'services-detail' | 'domains-detail'
>

type RelationData =
  | { panel: 'monitoring-instance-evidence'; vpsId: string; records: VPSMonitoringInstanceSummary[] }
  | { panel: 'services-detail'; vpsId: string; records: AssetServiceRecord[] }
  | { panel: 'domains-detail'; vpsId: string; records: AssetDomainRecord[] }

type LoadState =
  | { status: 'idle' }
  | { status: 'loading'; panel: RelationPanel; vpsId: string }
  | { status: 'error'; panel: RelationPanel; vpsId: string; message: string }
  | { status: 'ready'; data: RelationData }

type Props = {
  vpsId: string
  management: VPSManagementController
}

const PANEL_COPY: Record<RelationPanel, { title: string; subject: string }> = {
  'monitoring-instance-evidence': { title: '关联监控实例', subject: '监控实例' },
  'services-detail': { title: '关联服务', subject: '服务' },
  'domains-detail': { title: '关联域名', subject: '域名' },
}

const noop = () => undefined

export function VPSOverviewRelationPanels({ vpsId, management }: Props) {
  const panel = relationPanel(management.panel)
  const [loadState, setLoadState] = useState<LoadState>({ status: 'idle' })
  const [loadRevision, setLoadRevision] = useState(0)
  const requestIdRef = useRef(0)

  useEffect(() => {
    if (!panel) return
    const requestId = ++requestIdRef.current
    // eslint-disable-next-line react-hooks/set-state-in-effect -- opening a relation panel invalidates any prior panel data before its scoped request starts
    setLoadState({ status: 'loading', panel, vpsId })

    void loadPanel(panel, vpsId)
      .then((data) => {
        if (requestId !== requestIdRef.current) return
        setLoadState({ status: 'ready', data })
      })
      .catch((error: unknown) => {
        if (requestId !== requestIdRef.current) return
        setLoadState({
          status: 'error',
          panel,
          vpsId,
          message: describeManagementError(error, `加载${PANEL_COPY[panel].subject}失败`),
        })
      })

    return () => {
      requestIdRef.current += 1
    }
  }, [loadRevision, panel, vpsId])

  if (!panel) return null
  const copy = PANEL_COPY[panel]
  const stateIsCurrent = (
    (loadState.status === 'loading' || loadState.status === 'error')
      && loadState.panel === panel
      && loadState.vpsId === vpsId
  ) || (
    loadState.status === 'ready'
      && loadState.data.panel === panel
      && loadState.data.vpsId === vpsId
  )

  return (
    <Modal
      open
      onClose={management.closePanel}
      title={copy.title}
      ariaLabel={copy.title}
      size="xl"
    >
      <div className="vps-detail-modal">
        {!stateIsCurrent || loadState.status === 'loading' ? (
          <p role="status">正在加载{copy.subject}…</p>
        ) : null}
        {stateIsCurrent && loadState.status === 'error' ? (
          <>
            <p className="asset-operation-feedback asset-operation-feedback--error" role="alert">
              {loadState.message}
            </p>
            <Button onClick={() => setLoadRevision((current) => current + 1)}>
              重试加载{copy.subject}
            </Button>
          </>
        ) : null}
        {stateIsCurrent && loadState.status === 'ready' ? renderPanel(loadState.data) : null}
      </div>
    </Modal>
  )
}

function relationPanel(panel: VPSManagementPanel): RelationPanel | null {
  switch (panel) {
    case 'monitoring-instance-evidence':
    case 'services-detail':
    case 'domains-detail':
      return panel
    default:
      return null
  }
}

async function loadPanel(panel: RelationPanel, vpsId: string): Promise<RelationData> {
  switch (panel) {
    case 'monitoring-instance-evidence':
      return { panel, vpsId, records: await listVPSMonitoringInstances(vpsId) }
    case 'services-detail':
      return { panel, vpsId, records: await listVPSServices(vpsId) }
    case 'domains-detail':
      return { panel, vpsId, records: await listVPSDomains(vpsId) }
  }
}

function renderPanel(data: RelationData) {
  switch (data.panel) {
    case 'monitoring-instance-evidence':
      return (
        <VPSMonitoringInstanceLinksSection
          monitoring={data.records}
          readOnly
          unlinkingMonitoringInstanceId={null}
          pendingUnlinkMonitoringInstance={null}
          linkFeedback={null}
          linkFeedbackIsError={false}
          onCreateMonitoringInstance={noop}
          onOpenLink={noop}
          onUpgradeMonitoringInstance={noop}
          onRequestUnlinkMonitoringInstance={noop}
          onCancelUnlinkMonitoringInstance={noop}
          onConfirmUnlinkMonitoringInstance={noop}
        />
      )
    case 'services-detail':
      return (
        <VPSServicesSection
          services={data.records}
          error={null}
          notice={null}
          readOnly
          onCreate={noop}
        />
      )
    case 'domains-detail':
      return (
        <VPSDomainsSection
          domains={data.records}
          error={null}
          notice={null}
          readOnly
          onCreate={noop}
        />
      )
  }
}
