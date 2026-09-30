import { useRef, type KeyboardEvent } from 'react'

import { Button } from '../../../components/atoms'
import { insertMarkdownAroundSelection } from '../recordWorkspaceModel'

type MarkdownSourceEditorProps = {
  value: string
  onChange: (value: string) => void
  readOnly?: boolean
  onSave?: () => void
  onInsertTemplate?: () => void
}

export function MarkdownSourceEditor({
  value,
  onChange,
  readOnly = false,
  onSave,
  onInsertTemplate,
}: MarkdownSourceEditorProps) {
  const sourceRef = useRef<HTMLTextAreaElement>(null)

  const wrap = (before: string, after = before) => {
    const area = sourceRef.current
    const start = area?.selectionStart ?? value.length
    const end = area?.selectionEnd ?? value.length
    const next = insertMarkdownAroundSelection(value, start, end, before, after)
    onChange(next.value)
    requestAnimationFrame(() => {
      area?.focus()
      area?.setSelectionRange(next.selectionStart, next.selectionEnd)
    })
  }

  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (!(event.metaKey || event.ctrlKey) || readOnly) return
    if (event.key === 'b') {
      event.preventDefault()
      wrap('**')
      return
    }
    if (event.key === 'i') {
      event.preventDefault()
      wrap('*')
      return
    }
    if (event.key === 's') {
      event.preventDefault()
      onSave?.()
    }
  }

  return (
    <div className="record-editor__source">
      {readOnly ? null : (
        <div className="record-editor__format" role="toolbar" aria-label="Markdown 格式">
          <Button size="sm" variant="ghost" onClick={() => wrap('**')}>加粗</Button>
          <Button size="sm" variant="ghost" onClick={() => wrap('*')}>斜体</Button>
          <Button size="sm" variant="ghost" onClick={() => wrap('`')}>代码</Button>
          <Button size="sm" variant="ghost" onClick={() => wrap('\n## ', '')}>标题</Button>
          <Button size="sm" variant="ghost" onClick={() => wrap('\n- [ ] ', '')}>任务</Button>
          {onInsertTemplate ? (
            <Button size="sm" variant="ghost" onClick={onInsertTemplate}>插入模板</Button>
          ) : null}
        </div>
      )}
      <textarea
        ref={sourceRef}
        className="input record-editor__textarea"
        aria-label="Markdown 源文"
        value={value}
        readOnly={readOnly}
        rows={18}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={onKeyDown}
        spellCheck={false}
      />
    </div>
  )
}
