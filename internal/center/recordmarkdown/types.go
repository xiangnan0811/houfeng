package recordmarkdown

import (
	"errors"

	"houfeng/internal/center/recordcollaboration"
)

const (
	DocumentRenderContractVersionV1 = "houfeng_markdown/v1"
	MaxDocumentMarkdownSourceBytes  = 256 * 1024
	MaxDocumentRenderNodes          = 4096
	MaxDocumentRenderDepth          = 16
)

var ErrInvalidDocumentMarkdown = errors.New("invalid document markdown")

const (
	DocumentRenderNodeParagraph     = recordcollaboration.CommentRenderNodeParagraph
	DocumentRenderNodeText          = recordcollaboration.CommentRenderNodeText
	DocumentRenderNodeLineBreak     = recordcollaboration.CommentRenderNodeLineBreak
	DocumentRenderNodeEmphasis      = recordcollaboration.CommentRenderNodeEmphasis
	DocumentRenderNodeStrong        = recordcollaboration.CommentRenderNodeStrong
	DocumentRenderNodeStrikethrough = recordcollaboration.CommentRenderNodeStrikethrough
	DocumentRenderNodeInlineCode    = recordcollaboration.CommentRenderNodeInlineCode
	DocumentRenderNodeFencedCode    = recordcollaboration.CommentRenderNodeFencedCode
	DocumentRenderNodeOrderedList   = recordcollaboration.CommentRenderNodeOrderedList
	DocumentRenderNodeUnorderedList = recordcollaboration.CommentRenderNodeUnorderedList
	DocumentRenderNodeLink          = recordcollaboration.CommentRenderNodeLink
	DocumentRenderNodeHeading       = "heading"
	DocumentRenderNodeBlockquote    = "blockquote"
	DocumentRenderNodeThematicBreak = "thematic_break"
	DocumentRenderNodeTable         = "table"
	DocumentRenderNodeTaskList      = "task_list"
	DocumentRenderNodeFootnoteRef   = "footnote_ref"
	DocumentRenderNodeFootnoteDef   = "footnote_def"
	DocumentRenderNodeReference     = "reference"
)

type DocumentReference struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type DocumentTaskItem struct {
	Checked  bool                 `json:"checked"`
	Children []DocumentRenderNode `json:"children"`
}

type DocumentRenderModel struct {
	Version string               `json:"version"`
	Nodes   []DocumentRenderNode `json:"nodes"`
}

type DocumentRenderNode struct {
	Type      string                   `json:"type"`
	Text      string                   `json:"text,omitempty"`
	Href      string                   `json:"href,omitempty"`
	Start     uint64                   `json:"start,omitempty"`
	Level     uint64                   `json:"level,omitempty"`
	Kind      string                   `json:"kind,omitempty"`
	ID        string                   `json:"id,omitempty"`
	Children  []DocumentRenderNode     `json:"children,omitempty"`
	Items     [][]DocumentRenderNode   `json:"items,omitempty"`
	Header    [][]DocumentRenderNode   `json:"header,omitempty"`
	Rows      [][][]DocumentRenderNode `json:"rows,omitempty"`
	TaskItems []DocumentTaskItem       `json:"-"`
}
