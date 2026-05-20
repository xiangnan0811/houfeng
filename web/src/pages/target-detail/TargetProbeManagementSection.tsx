import type { RefObject } from 'react'

import { Button } from '../../components/atoms/Button'

type TargetProbeManagementSectionProps = {
  addProbeButtonRef: RefObject<HTMLButtonElement | null>
  probeFormOpen: boolean
  probeMutationError: string | null
  addDisabled: boolean
  onOpenCreate: () => void
}

export function TargetProbeManagementSection({
  addProbeButtonRef,
  probeFormOpen,
  probeMutationError,
  addDisabled,
  onOpenCreate,
}: TargetProbeManagementSectionProps) {
  return (
    <div className="watchtower-property-item">
      <div className="watchtower-property-item__main">
        <span className="watchtower-property-item__title">探测项目配置</span>
        <span className="watchtower-property-item__desc">
          配置目标的主机存活与应用探测规则；创建和编辑在抽屉工作面中完成。
        </span>
        {probeMutationError ? (
          <span className="watchtower-property-item__error" role="alert">
            {probeMutationError}
          </span>
        ) : null}
      </div>

      <div className="watchtower-property-item__actions">
        <Button
          ref={addProbeButtonRef}
          variant="secondary"
          disabled={addDisabled || probeFormOpen}
          onClick={onOpenCreate}
        >
          添加 ProbeItem
        </Button>
      </div>
    </div>
  )
}
