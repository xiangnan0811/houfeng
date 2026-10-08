import { lazy, Suspense } from 'react'

import { Modal } from '../../../components/atoms'
import { capabilityFlagsForSession } from '../../../lib/auth-client'
import { useAuth } from '../../../lib/auth-context'

const RecordExportPanel = lazy(() => import('../RecordExportPanel').then((module) => ({
  default: module.RecordExportPanel,
})))
const RecordImportPanel = lazy(() => import('../RecordImportPanel').then((module) => ({
  default: module.RecordImportPanel,
})))

export type RecordTool = 'export' | 'import' | null

type RecordToolDialogsProps = {
  tool: RecordTool
  recordId?: string | undefined
  revisionId?: string | undefined
  snapshotIds: readonly string[]
  onClose: () => void
}

/** 导出 / 导入是次要工具：从页头按钮打开弹窗，面板按需懒加载。 */
export function RecordToolDialogs({ tool, recordId, revisionId, snapshotIds, onClose }: RecordToolDialogsProps) {
  const auth = useAuth()
  const { portability } = capabilityFlagsForSession(auth)
  if (!portability) return null
  return (
    <>
      <Modal open={tool === 'export' && Boolean(recordId)} onClose={onClose} title="导出记录" size="md">
        {recordId ? (
          <Suspense fallback={<p className="record-muted">正在加载导出</p>}>
            <RecordExportPanel
              recordId={recordId}
              {...(revisionId ? { revisionId } : {})}
              snapshotIds={[...snapshotIds]}
            />
          </Suspense>
        ) : null}
      </Modal>
      <Modal open={tool === 'import'} onClose={onClose} title="导入记录" size="md">
        <Suspense fallback={<p className="record-muted">正在加载导入</p>}>
          <RecordImportPanel />
        </Suspense>
      </Modal>
    </>
  )
}
