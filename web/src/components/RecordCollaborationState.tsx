import { PageState } from './PageState'

export type RecordCollaborationSurfaceState =
  | 'ready'
  | 'loading'
  | 'empty'
  | 'error'
  | 'revoked'
  | 'deleted'

type RecordCollaborationStateProps = {
  state: Exclude<RecordCollaborationSurfaceState, 'ready'>
  loadingTitle: string
  emptyTitle: string
  errorTitle: string
}

export function RecordCollaborationState({
  state,
  loadingTitle,
  emptyTitle,
  errorTitle,
}: RecordCollaborationStateProps) {
  if (state === 'loading') {
    return <PageState kind="loading" compact title={loadingTitle} />
  }
  if (state === 'error') {
    return <PageState kind="error" compact title={errorTitle} description="请稍后重试；当前内容未展示。" />
  }
  if (state === 'empty') {
    return <PageState kind="empty" compact surface="empty" title={emptyTitle} />
  }
  return (
    <section className={`record-collaboration-state record-collaboration-state--${state} card card--warning`} role="status">
      <p>{state === 'revoked' ? '协作权限已撤销' : '记录已删除'}</p>
      <span>{state === 'revoked' ? '当前协作内容已收起。' : '协作内容不再可用。'}</span>
    </section>
  )
}
