import { useParams } from 'react-router-dom'

import { RecordWorkspace } from './RecordWorkspace'

export function RecordEditPage() {
  const { recordId } = useParams()
  return <RecordWorkspace mode="edit" {...(recordId ? { recordId } : {})} />
}
