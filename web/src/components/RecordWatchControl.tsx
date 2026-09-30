import type { RecordFollowerPreference, RecordWatch } from '../lib/types'
import type { RecordCollaborationSurfaceState } from './RecordCollaborationState'
import { RecordCollaborationState } from './RecordCollaborationState'

type RecordWatchControlProps = {
  state: RecordCollaborationSurfaceState
  watch: RecordWatch | null
  busy: boolean
  onChange: (preference: RecordFollowerPreference) => void
}

const preferences: Array<[RecordFollowerPreference, string, string]> = [
  ['watching', '关注全部更新', '全部更新'],
  ['default', '跟随自动来源', '自动'],
  ['muted', '静默可选更新', '静默'],
]

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
    <section className="record-collaboration-panel record-watch-control" aria-labelledby="record-watch-title">
      <header className="record-collaboration-panel__header">
        <h2 className="record-collaboration-panel__title" id="record-watch-title">关注</h2>
      </header>
      <div className="tabs tabs--pill record-watch-control__commands" role="group" aria-label="关注偏好">
        {preferences.map(([preference, label, short]) => (
          <button key={preference} type="button" className={watch.preference === preference ? 'tab is-active' : 'tab'}
            disabled={busy} aria-pressed={watch.preference === preference} aria-label={label}
            onClick={() => onChange(preference)}>{short}</button>
        ))}
      </div>
      <p className="record-watch-control__sources">
        自动来源：<span>{activeSources.length ? activeSources.join('、') : '无'}</span>
      </p>
      {watch.preference === 'muted' ? (
        <p className="record-watch-control__mandatory">直接指派、安全提醒与提及仍会送达，静默设置不会覆盖这些必要通知。</p>
      ) : null}
    </section>
  )
}
