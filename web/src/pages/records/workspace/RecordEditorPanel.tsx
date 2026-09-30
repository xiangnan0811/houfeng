import { lazy, Suspense } from 'react'

import { Button, Input } from '../../../components/atoms'
import type { DocumentReference } from '../../../lib/documentMarkdown'
import { MarkdownSourceEditor } from '../editor/MarkdownSourceEditor'

const MarkdownPreview = lazy(() => import('../editor/MarkdownPreview').then((module) => ({
  default: module.MarkdownPreview,
})))

export type RecordEditorLayout = 'edit' | 'split' | 'preview'

const LAYOUTS: ReadonlyArray<{ value: RecordEditorLayout; label: string }> = [
  { value: 'edit', label: '编辑' },
  { value: 'split', label: '分栏' },
  { value: 'preview', label: '预览' },
]

const PANE_CLASSES: Record<RecordEditorLayout, string> = {
  edit: 'record-editor__panes record-editor__panes--single',
  split: 'record-editor__panes record-editor__panes--split',
  preview: 'record-editor__panes record-editor__panes--single',
}

type RecordEditorPanelProps = {
  title: string
  body: string
  layout: RecordEditorLayout
  references: readonly DocumentReference[]
  onTitle: (title: string) => void
  onBody: (body: string) => void
  onLayout: (layout: RecordEditorLayout) => void
  onSave: () => void
  onInsertTemplate: () => void
  /** 仅已存在的记录可以把勾选条目提升为行动项。 */
  onPromote?: (() => void) | undefined
}

export function RecordEditorPanel({
  title,
  body,
  layout,
  references,
  onTitle,
  onBody,
  onLayout,
  onSave,
  onInsertTemplate,
  onPromote,
}: RecordEditorPanelProps) {
  return (
    <section className="record-section record-editor" aria-label="正文编辑">
      <Input
        label="标题"
        className="record-editor__title"
        value={title}
        placeholder="一句话说明这条记录"
        onChange={(event) => onTitle(event.target.value)}
      />
      <div className="record-section__head record-editor__head">
        <h2 className="record-section__title">正文</h2>
        <div className="record-section__actions">
          {onPromote ? (
            <Button size="sm" variant="ghost" onClick={onPromote}>提升勾选为行动</Button>
          ) : null}
          <div className="tabs tabs--pill record-editor__layout" role="toolbar" aria-label="编辑布局">
            {LAYOUTS.map((item) => (
              <button
                key={item.value}
                type="button"
                className={item.value === layout ? 'tab is-active' : 'tab'}
                aria-pressed={item.value === layout}
                onClick={() => onLayout(item.value)}
              >
                {item.label}
              </button>
            ))}
          </div>
        </div>
      </div>
      <div className={PANE_CLASSES[layout]}>
        {layout !== 'preview' ? (
          <MarkdownSourceEditor value={body} onChange={onBody} onSave={onSave} onInsertTemplate={onInsertTemplate} />
        ) : null}
        {layout !== 'edit' ? (
          <div className="record-editor__preview-pane">
            {/* 分栏时与左侧格式工具条同高，让两侧内容顶端对齐。 */}
            <p className="record-editor__pane-head" aria-hidden="true">预览</p>
            <div className="record-editor__preview" aria-label="Markdown 预览" role="region">
              {body.trim() ? (
                <Suspense fallback={<p className="record-muted">正在加载预览</p>}>
                  <MarkdownPreview source={body} references={references} />
                </Suspense>
              ) : <p className="record-muted">暂无正文</p>}
            </div>
          </div>
        ) : null}
      </div>
    </section>
  )
}
