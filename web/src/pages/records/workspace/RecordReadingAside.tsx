import { Button, Input } from '../../../components/atoms'
import { RecordWatchControl } from '../../../components/RecordWatchControl'
import type { RecordMaterialItem } from '../editor/RecordMaterialDrawer'
import { RecordMaterialList } from '../editor/RecordMaterialList'
import { RecordOutline } from '../editor/RecordOutline'
import type { RecordCollaboration } from './useRecordCollaboration'
import { countClass } from './recordPresentation'

type RecordReadingAsideProps = {
  source: string
  model: unknown
  materials: readonly RecordMaterialItem[]
  collaboration: RecordCollaboration | null
  restore: {
    reason: string
    busy: boolean
    onReason: (reason: string) => void
    onRestore: () => void
  } | null
}

export function RecordReadingAside({ source, model, materials, collaboration, restore }: RecordReadingAsideProps) {
  return (
    <>
      {restore ? (
        <section className="record-section record-restore" aria-labelledby="record-restore-title">
          <h2 className="record-section__title" id="record-restore-title">恢复此修订</h2>
          <Input label="恢复原因" value={restore.reason} onChange={(event) => restore.onReason(event.target.value)} />
          <Button size="md" disabled={restore.busy} onClick={restore.onRestore}>恢复为新修订</Button>
        </section>
      ) : null}

      <RecordOutline source={source} model={model} minHeadings={2} />

      <section className="record-section" aria-labelledby="record-materials-title">
        <h2 className="record-section__title" id="record-materials-title">
          材料 <span className={countClass(materials.length)}>{materials.length}</span>
        </h2>
        {materials.length > 0
          ? <RecordMaterialList items={materials} />
          : <p className="record-muted">没有附件或证据</p>}
      </section>

      {collaboration ? (
        <RecordWatchControl
          state={collaboration.state}
          watch={collaboration.watch}
          busy={collaboration.busy}
          onChange={collaboration.setWatchPreference}
        />
      ) : null}
    </>
  )
}
