import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import type { VPSAssetDetail, VPSTimeline } from '../../lib/types'
import { VPSDecisionBoard } from './VPSDecisionBoard'

function minimalDetail(overrides: Partial<VPSAssetDetail> = {}): VPSAssetDetail {
  return {
    vps_id: 'vps_001',
    display_name: 'Tokyo Edge',
    lifecycle_status: 'active',
    usage_status: 'in_use',
    renewal_decision: 'keep',
    monitoring_instance_links: [],
    labels: [],
    created_at: '2026-04-26T09:00:00Z',
    updated_at: '2026-04-26T09:00:00Z',
    ...overrides,
  } as unknown as VPSAssetDetail
}

const emptyTimeline: VPSTimeline = {
  vps_id: 'vps_001',
  renewal_decisions: [],
  price_histories: [],
  ip_histories: [],
  spec_snapshots: [],
  experience_logs: [],
}

describe('VPSDecisionBoard', () => {
  it('does not describe migration as an implemented lifecycle flow when preview is absent', () => {
    render(
      <MemoryRouter>
        <VPSDecisionBoard
          detail={minimalDetail({ renewal_decision: 'cancel' })}
          timeline={emptyTimeline}
          primarySubscription={null}
          subscriptionLoadFailed={false}
          subscriptionError={null}
          services={[]}
          domains={[]}
          factNotice={null}
          factError={null}
          linkFeedback={null}
          linkFeedbackIsError={false}
          serviceNotice={null}
          serviceError={null}
          domainNotice={null}
          domainError={null}
          experienceNotice={null}
          subscriptionNotice={null}
          subscriptionCreateError={null}
          validityExtensionNotice={null}
          validityExtensionError={null}
          monitoringCreateNotice={null}
          monitoringCreateError={null}
          lifecycleNotice={null}
          lifecycleError={null}
          cancellationPreview={null}
          cancellationPreviewError={null}
          onDecisionEdit={vi.fn()}
          onCancellationOpen={vi.fn()}
          onFactEdit={vi.fn()}
          onExperienceLog={vi.fn()}
          onSubscriptionCreate={vi.fn()}
          onMonitoringInstanceCreate={vi.fn()}
          onOpenFacts={vi.fn()}
          onOpenMonitoringInstanceEvidence={vi.fn()}
          onOpenServices={vi.fn()}
          onOpenDomains={vi.fn()}
          onOpenTimeline={vi.fn()}
        />
      </MemoryRouter>,
    )

    expect(screen.queryByText(/迁移流程/)).not.toBeInTheDocument()
    expect(screen.getByText(/迁移意向/)).toBeInTheDocument()
  })
})
