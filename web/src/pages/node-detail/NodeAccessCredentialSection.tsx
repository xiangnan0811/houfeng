import { Link } from 'react-router-dom'

import { CollapsibleSection } from '../../components/CollapsibleSection'
import { StatusBadge } from '../../components/StatusBadge'
import type { NodeRecord } from '../../lib/types'

type NodeAccessCredentialSectionProps = {
  node: NodeRecord
}

export function NodeAccessCredentialSection({ node }: NodeAccessCredentialSectionProps) {
  return (
    <CollapsibleSection title="接入凭证状态" className="watchtower-secondary">
      <p>
        当前绑定状态：<StatusBadge label={node.binding_status} />
      </p>
      <p>
        <Link className="text-link" to={`/nodes/${node.node_id}/onboarding`}>
          查看接入工作台 →
        </Link>
      </p>
    </CollapsibleSection>
  )
}
