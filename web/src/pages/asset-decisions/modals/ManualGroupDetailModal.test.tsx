import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type {
  AssetDecisionManualGroupDetail,
  VPSAssetRecord,
} from '../../../lib/types'
import type { FormSubmitEvent } from '../types'
import { ManualGroupDetailModal } from './ManualGroupDetailModal'

const CANDIDATE: VPSAssetRecord = {
  vps_id: 'vps_002',
  display_name: 'Tokyo Candidate',
  provider_id: 'provider_001',
  provider_name: 'Provider One',
  product_name: 'Compute',
  order_ref: 'order-002',
  country: 'JP',
  region: 'Kanto',
  city: 'Tokyo',
  datacenter: 'nrt-1',
  ipv4: '192.0.2.2',
  ipv6: '',
  ssh_host: '192.0.2.2',
  ssh_port: 22,
  ssh_user: 'root',
  os_name: 'Debian',
  virtualization: 'kvm',
  lifecycle_status: 'active',
  usage_status: 'standby',
  renewal_decision: 'unreviewed',
  importance: 'normal',
  labels: [],
  note: '',
  active_monitoring_instance_link_count: 0,
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
}

const DETAIL = {
  manual_group_id: 'admg_001',
  title: '自定义组合',
  status: 'active',
  members: [],
} as unknown as AssetDecisionManualGroupDetail

describe('ManualGroupDetailModal', () => {
  it('emits semantic member-draft patches instead of React state setters', () => {
    const updateMemberAddDraft = vi.fn()
    const setMemberAddAdvanced = vi.fn()
    const submitMemberAdd = vi.fn((event: FormSubmitEvent) => event.preventDefault())

    render(
      <ManualGroupDetailModal
        open
        manualDetailState={{ loading: false, error: null, detail: DETAIL }}
        manualDetailPanel="add"
        manualGroupProgress={{
          readinessLabel: '待整理',
          readinessTone: 'notice',
          readyToRecord: false,
          doneCount: 0,
          totalCount: 1,
          items: [],
        }}
        manualGroupError={null}
        manualGroupSaving={false}
        templateSaving={false}
        manualMemberSaving={{}}
        pendingManualMemberRemoval={null}
        manualMemberAddDraft={{
          vpsID: '',
          intendedRole: 'observe_candidate',
          intendedAction: 'review',
          reason: '',
          note: '',
          sortOrder: '',
        }}
        manualMemberAddAdvanced={false}
        vpsCatalogState={{ loading: false, error: null }}
        manualMemberCandidateRows={[CANDIDATE]}
        recordDraft={null}
        recordDraftEditingMemberID={null}
        recordSaving={false}
        recordSaveError={null}
        manualMemberColumns={[]}
        onClose={vi.fn()}
        onSelectManualDetailPanel={vi.fn()}
        onStartManualRecordSave={vi.fn()}
        onSubmitRecordSave={vi.fn()}
        onCancelRecordSave={vi.fn()}
        onSubmitManualGroupPatch={vi.fn()}
        onSaveManualGroupAsTemplate={vi.fn()}
        onSubmitManualMemberAdd={submitMemberAdd}
        onRequestManualMemberRemoval={vi.fn()}
        onCancelManualMemberRemoval={vi.fn()}
        onDeleteManualMember={vi.fn()}
        onUpdateMemberAddDraft={updateMemberAddDraft}
        onSetManualMemberAddAdvancedVisible={setMemberAddAdvanced}
        onUpdateRecordDraft={vi.fn()}
        onUpdateRecordDraftMember={vi.fn()}
        onEditRecordDraftMember={vi.fn()}
      />,
    )

    fireEvent.change(screen.getByLabelText('VPS'), { target: { value: 'vps_002' } })
    expect(updateMemberAddDraft).toHaveBeenCalledWith({ vpsID: 'vps_002' })

    fireEvent.click(screen.getByRole('button', { name: '高级选项' }))
    expect(setMemberAddAdvanced).toHaveBeenCalledWith(true)

    fireEvent.submit(screen.getByRole('button', { name: '加入组合' }).closest('form')!)
    expect(submitMemberAdd).toHaveBeenCalledOnce()
  })
})
