import { useParams } from 'react-router-dom'

import { RecordWorkspace } from './RecordWorkspace'

export function RecordDetailPage() {
  const { recordId } = useParams()
  return <RecordWorkspace mode="read" {...(recordId ? { recordId } : {})} />
}
