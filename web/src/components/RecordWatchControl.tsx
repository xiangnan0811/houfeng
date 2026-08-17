import type { RecordFollowerPreference, RecordWatch } from '../lib/types'
import type { RecordCollaborationSurfaceState } from './RecordCollaborationState'
import { RecordCollaborationState } from './RecordCollaborationState'
import { Button } from './atoms'

type RecordWatchControlProps = {
  state: RecordCollaborationSurfaceState
  watch: RecordWatch | null
  busy: boolean
  onChange: (preference: RecordFollowerPreference) => void
}

const sourceLabels: Array<[keyof RecordWatch['sources'], string]> = [
  ['author', '创建人'], ['owner', '负责人'], ['participant', '参与人'],
  ['comment', '评论参与'], ['mention', '被提及'], ['action', '行动参与'],
]

export function RecordWatchControl({ state, watch, busy, onChange }: RecordWatchControlProps) {
  if (state !== 'ready' || !watch) {
    return <RecordCollaborationState state={state === 'ready' ? 'error' : state}
      loadingTitle="正在读取关注状态" emptyTitle="暂无关注状态" errorTitle="关注状态暂不可用" />
  }
  const activeSources = sourceLabels.filter(([key]) => watch.sources[key]).map(([, label]) => label)
  return (
    <section className="record-collaboration-panel record-watch-control card" aria-labelledby="record-watch-title">
      <header className="record-collaboration-panel__header section-heading">
        <div><p className="record-collaboration-panel__eyebrow section-heading__eyebrow">WATCH POLICY</p><h2 className="section-heading__title" id="record-watch-title">关注策略</h2></div>
        <span className="record-collaboration-panel__signal badge badge--info">v{watch.version}</span>
      </header>
      <p className="record-watch-control__sources">
        自动来源：<span>{activeSources.length ? activeSources.join('、') : '无'}</span>
      </p>
      <div className="record-watch-control__commands page-form-actions" role="group" aria-label="关注偏好">
        <Button size="sm" variant={watch.preference === 'watching' ? 'primary' : 'secondary'} disabled={busy}
          aria-pressed={watch.preference === 'watching'} onClick={() => onChange('watching')}>关注全部更新</Button>
        <Button size="sm" variant={watch.preference === 'default' ? 'primary' : 'secondary'} disabled={busy}
          aria-pressed={watch.preference === 'default'} onClick={() => onChange('default')}>跟随自动来源</Button>
        <Button size="sm" variant={watch.preference === 'muted' ? 'primary' : 'secondary'} disabled={busy}
          aria-pressed={watch.preference === 'muted'} onClick={() => onChange('muted')}>静默可选更新</Button>
      </div>
      <p className="record-watch-control__mandatory inline-alert warn">直接指派、安全提醒与提及仍会送达，静默设置不会覆盖这些必要通知。</p>
    </section>
  )
}
