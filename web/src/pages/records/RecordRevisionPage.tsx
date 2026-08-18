import { useParams } from 'react-router-dom'

import { RecordWorkspace } from './RecordWorkspace'

export function RecordRevisionPage() {
  const { recordId, revisionId } = useParams()
  return <RecordWorkspace mode="revision" {...(recordId ? { recordId } : {})} {...(revisionId ? { revisionId } : {})} />
}
