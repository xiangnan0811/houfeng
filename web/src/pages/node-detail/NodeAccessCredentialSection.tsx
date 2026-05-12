import { Link } from 'react-router-dom'

import { StatusBadge } from '../../components/StatusBadge'
import type { NodeRecord } from '../../lib/types'

type NodeAccessCredentialSectionProps = {
  node: NodeRecord
}

export function NodeAccessCredentialSection({ node }: NodeAccessCredentialSectionProps) {
  return (
    <div className="watchtower-property-item">
      <div className="watchtower-property-item__main">
        <span className="watchtower-property-item__title">绑定状态</span>
        <span className="watchtower-property-item__desc">
          当前绑定状态：<StatusBadge label={node.binding_status} />
        </span>
      </div>
      <div className="watchtower-property-item__actions">
        <Link className="btn btn--secondary btn--md" to={`/nodes/${node.node_id}/onboarding`}>
          打开接入工作台
        </Link>
      </div>
    </div>
  )
}
