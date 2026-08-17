import { type ReactNode } from 'react'

import {
  decodeCommentRenderModelV1,
  type CommentInlineNodeV1,
  type CommentRenderNodeV1,
} from '../lib/commentMarkdown'

type RecordCommentMarkdownProps = {
  model: unknown
}

export function RecordCommentMarkdown({ model }: RecordCommentMarkdownProps) {
  const decoded = decodeCommentRenderModelV1(model)
  return (
    <div className="record-comment-markdown" data-render-contract={decoded.version}>
      {decoded.nodes.map((node, index) => renderBlockNode(node, `block-${index}`))}
    </div>
  )
}

function renderBlockNode(node: CommentRenderNodeV1, key: string): ReactNode {
  switch (node.type) {
    case 'paragraph':
      return <p key={key}>{renderInlineNodes(node.children, key)}</p>
    case 'fenced_code':
      return <pre key={key}><code>{node.text}</code></pre>
    case 'ordered_list':
      return (
        <ol key={key} start={node.start}>
          {node.items.map((item, index) => <li key={`${key}-item-${index}`}>{renderInlineNodes(item, `${key}-item-${index}`)}</li>)}
        </ol>
      )
    case 'unordered_list':
      return (
        <ul key={key}>
          {node.items.map((item, index) => <li key={`${key}-item-${index}`}>{renderInlineNodes(item, `${key}-item-${index}`)}</li>)}
        </ul>
      )
  }
}

function renderInlineNodes(nodes: readonly CommentInlineNodeV1[], keyPrefix: string): ReactNode[] {
  return nodes.map((node, index) => renderInlineNode(node, `${keyPrefix}-inline-${index}`))
}

function renderInlineNode(node: CommentInlineNodeV1, key: string): ReactNode {
  switch (node.type) {
    case 'text':
      return <span key={key}>{node.text}</span>
    case 'line_break':
      return <br key={key} />
    case 'inline_code':
      return <code key={key}>{node.text}</code>
    case 'emphasis':
      return <em key={key}>{renderInlineNodes(node.children, key)}</em>
    case 'strong':
      return <strong key={key}>{renderInlineNodes(node.children, key)}</strong>
    case 'strikethrough':
      return <s key={key}>{renderInlineNodes(node.children, key)}</s>
    case 'link':
      return <a key={key} href={node.href} rel="noopener noreferrer">{renderInlineNodes(node.children, key)}</a>
  }
}
