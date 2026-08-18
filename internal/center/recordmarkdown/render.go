package recordmarkdown

import (
	"fmt"
	"html"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"

	"houfeng/internal/center/recordcollaboration"
)

func documentGoldmark() goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.Footnote),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	)
}

func ScanDocumentSource(source string) error {
	stripped := stripAllowedHoufengRefComments(source)
	parsed := documentGoldmark().Parser().Parse(text.NewReader([]byte(stripped)))
	var walkErr error
	sourceBytes := []byte(stripped)
	_ = ast.Walk(parsed, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node.Kind() {
		case ast.KindRawHTML, ast.KindHTMLBlock:
			walkErr = ErrInvalidDocumentMarkdown
			return ast.WalkStop, walkErr
		case ast.KindImage:
			walkErr = ErrInvalidDocumentMarkdown
			return ast.WalkStop, walkErr
		case ast.KindLink, ast.KindAutoLink:
			destination := goldmarkLinkDestination(node, sourceBytes)
			if destination == "" {
				return ast.WalkContinue, nil
			}
			if strings.HasPrefix(destination, "houfeng-evidence:") || strings.HasPrefix(destination, "houfeng-attachment:") {
				return ast.WalkContinue, nil
			}
			if recordcollaboration.ValidateCanonicalHTTPLink(destination) != nil {
				walkErr = ErrInvalidDocumentMarkdown
				return ast.WalkStop, walkErr
			}
		}
		return ast.WalkContinue, nil
	})
	return walkErr
}

func stripAllowedHoufengRefComments(source string) string {
	lines := strings.Split(source, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if houfengRefCommentPattern.MatchString(strings.TrimSpace(line)) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func goldmarkLinkDestination(node ast.Node, source []byte) string {
	switch typed := node.(type) {
	case *ast.Link:
		return string(typed.Destination)
	case *ast.AutoLink:
		return string(typed.URL(source))
	default:
		return ""
	}
}

func DocumentHTMLPolicy() *bluemonday.Policy {
	policy := bluemonday.NewPolicy()
	policy.AllowElements(
		"p", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "hr",
		"ul", "ol", "li", "pre", "code", "em", "strong", "s", "br",
		"table", "thead", "tbody", "tr", "th", "td", "aside", "span", "a", "input",
	)
	policy.AllowAttrs("href").OnElements("a")
	policy.AllowAttrs("rel").OnElements("a")
	policy.AllowAttrs("start").OnElements("ol")
	policy.AllowAttrs("type", "checked", "disabled").OnElements("input")
	policy.AllowAttrs("data-ref-kind", "data-ref-id", "class").OnElements("aside")
	policy.AllowURLSchemes("http", "https")
	return policy
}

func RenderSafeHTML(model DocumentRenderModel) (string, error) {
	if err := model.Validate(); err != nil {
		return "", err
	}
	var builder strings.Builder
	for _, node := range model.Nodes {
		if err := writeDocumentHTML(&builder, node); err != nil {
			return "", err
		}
	}
	return DocumentHTMLPolicy().Sanitize(builder.String()), nil
}

// DocumentRenderStatus reports whether a body could be expressed as a render model.
// The dialect owns these tokens because it owns the decision; the HTTP layer and the
// export path must report the same word for the same body.
type DocumentRenderStatus string

const (
	// DocumentRenderReady means the body parsed into a closed render model.
	DocumentRenderReady DocumentRenderStatus = "ready"
	// DocumentRenderUnsupported means the body is stored and readable but uses
	// structures the dialect does not express, so no model was produced.
	DocumentRenderUnsupported DocumentRenderStatus = "unsupported"
)

// SafeDocumentHTML is the single server-side HTML exit for a record body. Writes
// only validate UTF-8 and the dialect version, so a stored body can legitimately be
// unmodelable; refusing to render it would make export lose content that the record
// UI still shows. The fallback is therefore escaped source as preformatted text —
// not a second, looser Markdown path — and the status says which one the caller got.
func SafeDocumentHTML(source string, authorized []DocumentReference) (string, DocumentRenderStatus, error) {
	model, err := ParseDocumentMarkdownV1(source, authorized)
	if err == nil {
		rendered, renderErr := RenderSafeHTML(model)
		if renderErr != nil {
			return "", "", renderErr
		}
		return rendered, DocumentRenderReady, nil
	}
	var builder strings.Builder
	builder.WriteString("<pre><code>")
	builder.WriteString(html.EscapeString(source))
	builder.WriteString("</code></pre>")
	return DocumentHTMLPolicy().Sanitize(builder.String()), DocumentRenderUnsupported, nil
}

func writeDocumentHTML(builder *strings.Builder, node DocumentRenderNode) error {
	switch node.Type {
	case DocumentRenderNodeParagraph:
		builder.WriteString("<p>")
		if err := writeDocumentInlineHTML(builder, node.Children); err != nil {
			return err
		}
		builder.WriteString("</p>")
	case DocumentRenderNodeHeading:
		fmt.Fprintf(builder, "<h%d>", node.Level)
		if err := writeDocumentInlineHTML(builder, node.Children); err != nil {
			return err
		}
		fmt.Fprintf(builder, "</h%d>", node.Level)
	case DocumentRenderNodeBlockquote:
		builder.WriteString("<blockquote>")
		for _, child := range node.Children {
			if err := writeDocumentHTML(builder, child); err != nil {
				return err
			}
		}
		builder.WriteString("</blockquote>")
	case DocumentRenderNodeThematicBreak:
		builder.WriteString("<hr>")
	case DocumentRenderNodeFencedCode:
		builder.WriteString("<pre><code>")
		builder.WriteString(html.EscapeString(node.Text))
		builder.WriteString("</code></pre>")
	case DocumentRenderNodeOrderedList:
		fmt.Fprintf(builder, `<ol start="%d">`, node.Start)
		for _, item := range node.Items {
			builder.WriteString("<li>")
			if err := writeDocumentInlineHTML(builder, item); err != nil {
				return err
			}
			builder.WriteString("</li>")
		}
		builder.WriteString("</ol>")
	case DocumentRenderNodeUnorderedList:
		builder.WriteString("<ul>")
		for _, item := range node.Items {
			builder.WriteString("<li>")
			if err := writeDocumentInlineHTML(builder, item); err != nil {
				return err
			}
			builder.WriteString("</li>")
		}
		builder.WriteString("</ul>")
	case DocumentRenderNodeTaskList:
		builder.WriteString(`<ul class="task-list">`)
		for _, item := range node.TaskItems {
			builder.WriteString("<li>")
			if item.Checked {
				builder.WriteString(`<input type="checkbox" disabled checked>`)
			} else {
				builder.WriteString(`<input type="checkbox" disabled>`)
			}
			if err := writeDocumentInlineHTML(builder, item.Children); err != nil {
				return err
			}
			builder.WriteString("</li>")
		}
		builder.WriteString("</ul>")
	case DocumentRenderNodeTable:
		builder.WriteString("<table><thead><tr>")
		for _, cell := range node.Header {
			builder.WriteString("<th>")
			if err := writeDocumentInlineHTML(builder, cell); err != nil {
				return err
			}
			builder.WriteString("</th>")
		}
		builder.WriteString("</tr></thead><tbody>")
		for _, row := range node.Rows {
			builder.WriteString("<tr>")
			for _, cell := range row {
				builder.WriteString("<td>")
				if err := writeDocumentInlineHTML(builder, cell); err != nil {
					return err
				}
				builder.WriteString("</td>")
			}
			builder.WriteString("</tr>")
		}
		builder.WriteString("</tbody></table>")
	case DocumentRenderNodeFootnoteDef:
		builder.WriteString("<aside>")
		builder.WriteString(html.EscapeString(node.Text))
		for _, child := range node.Children {
			if err := writeDocumentHTML(builder, child); err != nil {
				return err
			}
		}
		builder.WriteString("</aside>")
	case DocumentRenderNodeReference:
		fmt.Fprintf(builder, `<aside class="record-ref" data-ref-kind="%s" data-ref-id="%s">`,
			html.EscapeString(node.Kind), html.EscapeString(node.ID))
		if err := writeDocumentInlineHTML(builder, node.Children); err != nil {
			return err
		}
		builder.WriteString("</aside>")
	default:
		return ErrInvalidDocumentMarkdown
	}
	return nil
}

func writeDocumentInlineHTML(builder *strings.Builder, nodes []DocumentRenderNode) error {
	for _, node := range nodes {
		switch node.Type {
		case DocumentRenderNodeText:
			builder.WriteString(html.EscapeString(node.Text))
		case DocumentRenderNodeLineBreak:
			builder.WriteString("<br>")
		case DocumentRenderNodeInlineCode:
			builder.WriteString("<code>")
			builder.WriteString(html.EscapeString(node.Text))
			builder.WriteString("</code>")
		case DocumentRenderNodeEmphasis:
			builder.WriteString("<em>")
			if err := writeDocumentInlineHTML(builder, node.Children); err != nil {
				return err
			}
			builder.WriteString("</em>")
		case DocumentRenderNodeStrong:
			builder.WriteString("<strong>")
			if err := writeDocumentInlineHTML(builder, node.Children); err != nil {
				return err
			}
			builder.WriteString("</strong>")
		case DocumentRenderNodeStrikethrough:
			builder.WriteString("<s>")
			if err := writeDocumentInlineHTML(builder, node.Children); err != nil {
				return err
			}
			builder.WriteString("</s>")
		case DocumentRenderNodeLink:
			fmt.Fprintf(builder, `<a href="%s" rel="noopener noreferrer">`, html.EscapeString(node.Href))
			if err := writeDocumentInlineHTML(builder, node.Children); err != nil {
				return err
			}
			builder.WriteString("</a>")
		case DocumentRenderNodeFootnoteRef:
			builder.WriteString("<span>")
			builder.WriteString(html.EscapeString(node.Text))
			builder.WriteString("</span>")
		default:
			return ErrInvalidDocumentMarkdown
		}
	}
	return nil
}
