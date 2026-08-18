import { decodeDocumentRenderModelV1 } from '../../../lib/documentMarkdown'

type RecordOutlineProps = {
  model?: unknown
  source: string
}

export function RecordOutline({ model, source }: RecordOutlineProps) {
  const headings = outlineFrom(model, source)
  return (
    <nav className="card" aria-label="正文大纲">
      <h2 className="section-heading__title">大纲</h2>
      {headings.length === 0 ? <p className="text-muted">尚无标题</p> : (
        <ol>
          {headings.map((heading) => (
            <li key={`${heading.level}-${heading.text}`} data-level={heading.level}>{heading.text}</li>
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
  return source.split('\n').flatMap((line) => {
    const match = /^(#{1,6})[ \t]+(.+)$/u.exec(line)
    return match?.[1] && match[2] ? [{ level: match[1].length, text: match[2] }] : []
  })
}
