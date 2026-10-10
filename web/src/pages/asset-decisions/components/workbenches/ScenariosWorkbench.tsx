import type { ReactNode } from 'react'

import { Badge, MonoDigits } from '../../../../components/atoms'
import { PageState as PageStateView } from '../../../../components/PageState'
import { MANUAL_GROUP_SCENARIO_LABELS, MANUAL_GROUP_STATUS_LABELS, SCENARIO_TEMPLATE_STATUS_LABELS } from '../../constants'
import { manualGroupStatusTone, scenarioTemplateStatusTone } from '../../formatters'
import type { ManualGroupsState, ScenarioTemplatesState } from '../../types'
import { ScanName, ScanRow, WorkbenchPanel } from './WorkbenchPanel'

type ScenariosWorkbenchProps = {
  templatesState: ScenarioTemplatesState
  manualGroupsState: ManualGroupsState
  onOpenTemplate: (templateID: string) => void
  onOpenManualGroup: (manualGroupID: string) => void
}

/** 没有徽章时渲染隐藏占位：桌面保持列对齐，窄屏折行时被收掉，不留空隙。 */
function RowBadges({ children }: { children: ReactNode[] }) {
  const badges = children.filter(Boolean)
  return badges.length > 0
    ? <span className="asset-scan-row__badges">{badges}</span>
    : <span aria-hidden="true" />
}

function MemberCount({ count }: { count: number }) {
  return count > 0
    ? <span className="asset-scan-row__muted"><MonoDigits>{count}</MonoDigits> 台</span>
    : <span aria-hidden="true" />
}

function TemplateList({ templatesState, onOpenTemplate }: Pick<ScenariosWorkbenchProps, 'templatesState' | 'onOpenTemplate'>) {
  if (templatesState.loading) return <PageStateView kind="loading" title="正在加载场景模板…" surface="empty" compact />
  if (templatesState.error) return <PageStateView kind="error" title="场景模板不可用" surface="empty" compact />
  if (templatesState.templates.length === 0) return <PageStateView kind="empty" title="暂无场景模板" surface="empty" compact />
  // 内置与自定义模板全部列出，不截断：自定义模板的维护入口必须可达。
  return (
    <ul className="asset-scan-list asset-scan-list--templates" aria-label="场景模板">
      {templatesState.templates.map((template) => {
        const open = () => onOpenTemplate(template.template_id)
        return (
          <ScanRow key={template.template_id} clickable>
            <ScanName name={template.title} meta={MANUAL_GROUP_SCENARIO_LABELS[template.scenario] ?? template.scenario} />
            <RowBadges>
              {[
                template.builtin ? <Badge key="builtin" variant="info" tone="neutral">内置</Badge> : null,
                template.status !== 'active' ? (
                  <Badge key="status" variant="state" tone={scenarioTemplateStatusTone(template.status)}>
                    {SCENARIO_TEMPLATE_STATUS_LABELS[template.status] ?? template.status}
                  </Badge>
                ) : null,
              ]}
            </RowBadges>
            <MemberCount count={template.member_count} />
            <span className="asset-scan-row__actions">
              <button className="btn sm secondary" type="button" data-row-primary onClick={open}>使用模板</button>
            </span>
          </ScanRow>
        )
      })}
    </ul>
  )
}

function ManualGroupList({ manualGroupsState, onOpenManualGroup }: Pick<ScenariosWorkbenchProps, 'manualGroupsState' | 'onOpenManualGroup'>) {
  if (manualGroupsState.loading) return <PageStateView kind="loading" title="正在加载自定义组合…" surface="empty" compact />
  if (manualGroupsState.error) return <PageStateView kind="error" title="自定义组合不可用" surface="empty" compact />
  if (manualGroupsState.groups.length === 0) return <PageStateView kind="empty" title="尚未创建自定义组合" surface="empty" compact />
  return (
    <ul className="asset-scan-list asset-scan-list--groups" aria-label="自定义资产组合">
      {manualGroupsState.groups.map((group) => {
        const open = () => onOpenManualGroup(group.manual_group_id)
        return (
          <ScanRow key={group.manual_group_id} clickable>
            <ScanName name={group.title} meta={MANUAL_GROUP_SCENARIO_LABELS[group.scenario] ?? group.scenario} />
            <RowBadges>
              {[
                group.status !== 'active' ? (
                  <Badge key="status" variant="state" tone={manualGroupStatusTone(group.status)}>
                    {MANUAL_GROUP_STATUS_LABELS[group.status] ?? group.status}
                  </Badge>
                ) : null,
              ]}
            </RowBadges>
            <MemberCount count={group.member_count} />
            <span className="asset-scan-row__actions">
              <button className="btn sm secondary" type="button" data-row-primary onClick={open}>查看</button>
            </span>
          </ScanRow>
        )
      })}
    </ul>
  )
}

export function ScenariosWorkbench(props: ScenariosWorkbenchProps) {
  return (
    <WorkbenchPanel title="自定义分组" className="asset-workbench--scenarios">
      <div className="asset-workbench__columns">
        <section className="asset-workbench__group" aria-labelledby="asset-workbench-templates">
          <h3 className="asset-workbench__subtitle" id="asset-workbench-templates">场景模板</h3>
          <TemplateList templatesState={props.templatesState} onOpenTemplate={props.onOpenTemplate} />
        </section>
        <section className="asset-workbench__group" aria-labelledby="asset-workbench-groups">
          <h3 className="asset-workbench__subtitle" id="asset-workbench-groups">自定义组合</h3>
          <ManualGroupList manualGroupsState={props.manualGroupsState} onOpenManualGroup={props.onOpenManualGroup} />
        </section>
      </div>
    </WorkbenchPanel>
  )
}
