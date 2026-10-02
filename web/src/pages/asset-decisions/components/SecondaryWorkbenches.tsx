import type { VPSAssetRecord } from '../../../lib/types'
import { AssetDecisionSecondaryNav } from '../AssetDecisionSecondaryNav'
import type {
  AssetDecisionSecondaryNavItem,
  ManualGroupsState,
  RecordsState,
  ScenarioTemplatesState,
  SecondaryWorkbench,
} from '../types'
import { RecordsWorkbench } from './workbenches/RecordsWorkbench'
import { RenewalsWorkbench } from './workbenches/RenewalsWorkbench'
import { ScenariosWorkbench } from './workbenches/ScenariosWorkbench'
import { SingleQueueWorkbench, type SingleQueueWorkbenchProps } from './workbenches/SingleQueueWorkbench'

type SecondaryWorkbenchesProps = SingleQueueWorkbenchProps & {
  secondaryWorkbench: SecondaryWorkbench | null
  secondaryNavItems: AssetDecisionSecondaryNavItem[]
  manualGroupsState: ManualGroupsState
  templatesState: ScenarioTemplatesState
  recordsState: RecordsState
  vpsByID: Map<string, VPSAssetRecord>
  onSetSelectedSecondaryWorkbench: (workbench: SecondaryWorkbench | null) => void
  onOpenManualGroup: (manualGroupID: string) => void
  onOpenTemplate: (templateID: string) => void
  onOpenRecord: (recordID: string) => void
}

export function SecondaryWorkbenches({
  secondaryWorkbench,
  secondaryNavItems,
  manualGroupsState,
  templatesState,
  recordsState,
  vpsByID,
  onSetSelectedSecondaryWorkbench,
  onOpenManualGroup,
  onOpenTemplate,
  onOpenRecord,
  ...queue
}: SecondaryWorkbenchesProps) {
  return (
    <>
      <div className="asset-decision-topology">
        <AssetDecisionSecondaryNav
          items={secondaryNavItems}
          active={secondaryWorkbench}
          onOpen={onSetSelectedSecondaryWorkbench}
        />
      </div>

      {secondaryWorkbench === 'records' ? (
        <RecordsWorkbench recordsState={recordsState} onOpenRecord={onOpenRecord} />
      ) : null}

      {secondaryWorkbench === 'scenarios' ? (
        <ScenariosWorkbench
          templatesState={templatesState}
          manualGroupsState={manualGroupsState}
          onOpenTemplate={onOpenTemplate}
          onOpenManualGroup={onOpenManualGroup}
        />
      ) : null}

      {secondaryWorkbench === 'renewals' ? (
        <RenewalsWorkbench queueState={queue.queueState} vpsByID={vpsByID} renewalWindow={queue.renewalWindow} />
      ) : null}

      {secondaryWorkbench === 'single_queue' ? <SingleQueueWorkbench {...queue} /> : null}
    </>
  )
}
