import { type ReactNode } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeSanitize, { defaultSchema } from 'rehype-sanitize'

import {
  decodeDocumentRenderModelV1,
  type DocumentInlineNodeV1,
  type DocumentReference,
  type DocumentRenderModelV1,
  type DocumentRenderNodeV1,
} from '../../../lib/documentMarkdown'
import type { RecordRenderModelStatus } from '../../../lib/types'

type MarkdownPreviewProps = {
  source?: string | undefined
  model?: unknown
  modelStatus?: RecordRenderModelStatus | undefined
  references?: readonly DocumentReference[] | undefined
}

const sanitizeSchema = {
  ...defaultSchema,
  tagNames: (defaultSchema.tagNames ?? []).filter((tag) => tag !== 'img' && tag !== 'script' && tag !== 'iframe'),
  protocols: {
    ...defaultSchema.protocols,
    href: ['http', 'https'],
  },
  attributes: {
    ...defaultSchema.attributes,
    span: [...(defaultSchema.attributes?.span ?? []), 'dataHoufengRef'],
  },
}

function remarkHoufengRefs() {
  return (tree: { type?: string; url?: string; children?: unknown[]; data?: { hName?: string; hProperties?: Record<string, string> } }) => {
    const walk = (node: { type?: string; url?: string; children?: unknown[]; data?: { hName?: string; hProperties?: Record<string, string> } }) => {
      if (node.type === 'link' && typeof node.url === 'string' && (node.url.startsWith('houfeng-evidence:') || node.url.startsWith('houfeng-attachment:'))) {
        node.data = { hName: 'span', hProperties: { dataHoufengRef: node.url } }
      }
      for (const child of node.children ?? []) {
        if (child && typeof child === 'object') walk(child as typeof node)
      }
    }
    walk(tree)
  }
}

export function MarkdownPreview({ source = '', model, modelStatus, references = [] }: MarkdownPreviewProps) {
  const decoded = decodeClosedModel(model)
  if (decoded) {
    return (
      <div className="card" data-render-contract={decoded.version}>
        {decoded.nodes.map((node, index) => renderBlock(node, `block-${index}`, references))}
      </div>
    )
  }
  return (
    <div className="card" data-render-contract="houfeng_markdown/v1-live">
      {fallbackNotice(modelStatus, model)}
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkHoufengRefs]}
        rehypePlugins={[[rehypeSanitize, sanitizeSchema]]}
        components={{
          img: () => null,
          a(props) {
            return renderReferenceOrLink(props.href ?? '', props.children, references, 'live')
          },
          span(props) {
            const href = 'data-houfeng-ref' in props && typeof props['data-houfeng-ref'] === 'string'
              ? props['data-houfeng-ref']
              : ''
            if (href) return renderReferenceOrLink(href, props.children, references, 'live')
            return <span {...props}>{props.children}</span>
          },
        }}
      >
        {source}
      </ReactMarkdown>
    </div>
  )
}

/**
 * Reaching the live path is only silent when nothing was expected: a body the server
 * refused to model and a model this decoder refused both mean the reader is looking at
 * source-rendered output, and neither should look identical to the model path.
 */
function fallbackNotice(modelStatus: RecordRenderModelStatus | undefined, model: unknown): ReactNode {
  if (modelStatus !== 'unsupported' && (model === undefined || model === null)) return null
  return (
    <p className="record-preview__notice" role="status">服务端渲染模型不可用，本页按源码渲染。</p>
  )
}

function renderBlock(node: DocumentRenderNodeV1, key: string, references: readonly DocumentReference[]): ReactNode {
  switch (node.type) {
    case 'paragraph':
      return <p key={key}>{renderInlines(node.children, key)}</p>
    case 'heading': {
      const HeadingTag = `h${node.level}` as 'h1' | 'h2' | 'h3' | 'h4' | 'h5' | 'h6'
      return <HeadingTag key={key}>{renderInlines(node.children, key)}</HeadingTag>
    }
    case 'blockquote':
      return <blockquote key={key}>{node.children.map((child, index) => renderBlock(child, `${key}-${index}`, references))}</blockquote>
    case 'thematic_break':
      return <hr key={key} />
    case 'fenced_code':
      return <pre key={key}><code>{node.text}</code></pre>
    case 'ordered_list':
      return (
        <ol key={key} start={node.start}>
          {node.items.map((item, index) => <li key={`${key}-${index}`}>{renderInlines(item, `${key}-${index}`)}</li>)}
        </ol>
      )
    case 'unordered_list':
      return (
        <ul key={key}>
          {node.items.map((item, index) => <li key={`${key}-${index}`}>{renderInlines(item, `${key}-${index}`)}</li>)}
        </ul>
      )
    case 'task_list':
      return (
        <ul key={key} className="record-task-list">
          {node.items.map((item, index) => (
            <li key={`${key}-${index}`}>
              <input type="checkbox" checked={item.checked} disabled readOnly />
              {renderInlines(item.children, `${key}-${index}`)}
            </li>
          ))}
        </ul>
      )
    case 'table':
      return (
        <table key={key}>
          <thead>
            <tr>{node.header.map((cell, index) => <th key={`${key}-h-${index}`}>{renderInlines(cell, `${key}-h-${index}`)}</th>)}</tr>
          </thead>
          <tbody>
            {node.rows.map((row, rowIndex) => (
              <tr key={`${key}-r-${rowIndex}`}>
                {row.map((cell, cellIndex) => <td key={`${key}-r-${rowIndex}-${cellIndex}`}>{renderInlines(cell, `${key}-r-${rowIndex}-${cellIndex}`)}</td>)}
              </tr>
            ))}
          </tbody>
        </table>
      )
    case 'footnote_def':
      return (
        <aside key={key} className="record-footnote">
          <strong>{node.text}</strong>
          {node.children.map((child, index) => renderBlock(child, `${key}-${index}`, references))}
        </aside>
      )
    case 'reference':
      // A reference is one paragraph holding one link, so both render paths wrap it
      // the same way. Emitting a block element here would nest a block inside the
      // paragraph the live path produces and drift from it structurally.
      return (
        <p key={key}>
          {renderReferenceMark(node.kind, node.id, renderInlines(node.children, key), references, key)}
        </p>
      )
  }
}

function renderInlines(nodes: readonly DocumentInlineNodeV1[], keyPrefix: string): ReactNode[] {
  return nodes.map((node, index) => renderInline(node, `${keyPrefix}-${index}`))
}

function renderInline(node: DocumentInlineNodeV1, key: string): ReactNode {
  switch (node.type) {
    case 'text':
      return <span key={key}>{node.text}</span>
    case 'line_break':
      return <br key={key} />
    case 'inline_code':
      return <code key={key}>{node.text}</code>
    case 'emphasis':
      return <em key={key}>{renderInlines(node.children, key)}</em>
    case 'strong':
      return <strong key={key}>{renderInlines(node.children, key)}</strong>
    case 'strikethrough':
      return <s key={key}>{renderInlines(node.children, key)}</s>
    case 'link':
      return <a key={key} href={node.href} rel="noopener noreferrer">{renderInlines(node.children, key)}</a>
    case 'footnote_ref':
      return <sup key={key}>{node.text}</sup>
  }
}

function renderReferenceOrLink(
  href: string,
  children: ReactNode,
  references: readonly DocumentReference[],
  key: string,
): ReactNode {
  const evidence = href.startsWith('houfeng-evidence:')
  const attachment = href.startsWith('houfeng-attachment:')
  if (!evidence && !attachment) {
    if (!href.startsWith('http://') && !href.startsWith('https://')) return <span key={key}>{children}</span>
    return <a key={key} href={href} rel="noopener noreferrer">{children}</a>
  }
  const kind = evidence ? 'evidence' : 'attachment'
  const id = href.slice(href.indexOf(':') + 1)
  return renderReferenceMark(kind, id, children, references, key)
}

function renderReferenceMark(
  kind: string,
  id: string,
  children: ReactNode,
  references: readonly DocumentReference[],
  key: string,
): ReactNode {
  return (
    <span key={key} className={referenceClass(kind, id, references)} data-ref-kind={kind} data-ref-id={id}>
      {children}
      {!isAuthorized(kind, id, references) ? <span>引用已失效</span> : null}
    </span>
  )
}

function decodeClosedModel(model: unknown): DocumentRenderModelV1 | null {
  if (model === undefined) return null
  try {
    return decodeDocumentRenderModelV1(model)
  } catch {
    return null
  }
}

function isAuthorized(kind: string, id: string, references: readonly DocumentReference[]): boolean {
  return references.some((reference) => reference.kind === kind && reference.id === id)
}

function referenceClass(kind: string, id: string, references: readonly DocumentReference[]): string {
  return isAuthorized(kind, id, references) ? 'card' : 'card card--dim'
}
