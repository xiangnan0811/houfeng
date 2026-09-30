import { decodeDocumentRenderModelV1 } from '../../../lib/documentMarkdown'

type RecordOutlineProps = {
  model?: unknown
  source: string
  /** 标题少于该数量时不渲染；只有一个标题的大纲没有导航价值。 */
  minHeadings?: number
}

export function RecordOutline({ model, source, minHeadings = 0 }: RecordOutlineProps) {
  const headings = outlineFrom(model, source)
  if (headings.length < minHeadings) return null
  const topLevel = Math.min(...headings.map((heading) => heading.level))
  return (
    <nav className="record-section record-outline" aria-label="正文大纲">
      <h2 className="record-section__title">大纲</h2>
      {headings.length === 0 ? <p className="record-muted">尚无标题</p> : (
        <ol className="record-outline__list">
          {headings.map((heading) => (
            <li key={`${heading.level}-${heading.text}`} data-level={heading.level}
              data-depth={Math.min(heading.level - topLevel, 3)}>{heading.text}</li>
          ))}
        </ol>
      )}
    </nav>
  )
}

function outlineFrom(model: unknown, source: string): Array<{ level: number; text: string }> {
  if (model !== undefined) {
    try {
      return decodeDocumentRenderModelV1(model).nodes.flatMap((node) => {
        if (node.type !== 'heading') return []
        const text = node.children.map((child) => 'text' in child ? child.text : '').join('')
        return [{ level: node.level, text }]
      })
    } catch {
      return outlineFromSource(source)
    }
  }
  return outlineFromSource(source)
}

function outlineFromSource(source: string): Array<{ level: number; text: string }> {
  let fence = ''
  // CRLF / CR 行尾要先去掉，否则闭合围栏行尾的 \r 会让围栏永不闭合。
  return source.split(/\r\n|\r|\n/u).flatMap((line) => {
    // 围栏代码块里的 "# 注释" 不是标题。按 CommonMark：闭合围栏必须是同一字符、
    // 长度不短于开启围栏，且其后只能有空白；开启围栏可以带信息串。
    if (fence) {
      const closing = /^ {0,3}(`{3,}|~{3,})[ \t]*$/u.exec(line)?.[1]
      if (closing && closing[0] === fence[0] && closing.length >= fence.length) fence = ''
      return []
    }
    const opening = /^ {0,3}(`{3,}|~{3,})/u.exec(line)?.[1]
    if (opening) {
      fence = opening
      return []
    }
    const match = /^(#{1,6})[ \t]+(.+)$/u.exec(line)
    return match?.[1] && match[2] ? [{ level: match[1].length, text: match[2] }] : []
  })
}
