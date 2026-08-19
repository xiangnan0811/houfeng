package recordsearch

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"houfeng/internal/center/recordmarkdown"
)

// MaxDocumentTextBytes mirrors the plain_text bound in migration 0056. The
// derivation truncates to it so an oversized body degrades to a shorter indexed
// document instead of failing the whole record write.
const MaxDocumentTextBytes = 64 * 1024

// DeriveDocumentText flattens a validated document render model into the text
// the index searches. It keeps prose and code, because command output and error
// strings are exactly what an operator searches for later, and drops link
// targets and reference identifiers, which are machine detail an operator would
// never type.
func DeriveDocumentText(model recordmarkdown.DocumentRenderModel) (string, error) {
	if err := model.Validate(); err != nil {
		return "", err
	}
	var builder strings.Builder
	for _, node := range model.Nodes {
		appendDocumentNodeText(&builder, node)
	}
	return truncateDocumentTextOnRuneBoundary(collapseDocumentSeparators(builder.String())), nil
}

// DeriveDocumentTextFromMarkdown flattens a stored body into indexable text.
// A body the dialect cannot parse yields empty text rather than an error: the
// index is derived, so a record whose searchable text cannot be produced must
// still save and stay findable by its title.
func DeriveDocumentTextFromMarkdown(source string, references []recordmarkdown.DocumentReference) string {
	model, err := recordmarkdown.ParseDocumentMarkdownV1(source, references)
	if err != nil {
		return ""
	}
	text, err := DeriveDocumentText(model)
	if err != nil {
		return ""
	}
	return text
}

func appendDocumentNodeText(builder *strings.Builder, node recordmarkdown.DocumentRenderNode) {
	switch node.Type {
	case recordmarkdown.DocumentRenderNodeText,
		recordmarkdown.DocumentRenderNodeInlineCode,
		recordmarkdown.DocumentRenderNodeFencedCode:
		appendDocumentTextRun(builder, node.Text)
		return
	case recordmarkdown.DocumentRenderNodeFootnoteRef:
		// The marker is a pointer to a definition whose text is indexed on its
		// own, so indexing the marker would only add a bare number.
		return
	}
	// footnote_def carries its marker in Text and its prose in Children, so only
	// the children are worth indexing.
	for _, child := range node.Children {
		appendDocumentNodeText(builder, child)
	}
	for _, item := range node.Items {
		for _, child := range item {
			appendDocumentNodeText(builder, child)
		}
	}
	for _, cell := range node.Header {
		for _, child := range cell {
			appendDocumentNodeText(builder, child)
		}
	}
	for _, row := range node.Rows {
		for _, cell := range row {
			for _, child := range cell {
				appendDocumentNodeText(builder, child)
			}
		}
	}
	for _, item := range node.TaskItems {
		for _, child := range item.Children {
			appendDocumentNodeText(builder, child)
		}
	}
}

func appendDocumentTextRun(builder *strings.Builder, text string) {
	if text == "" {
		return
	}
	if builder.Len() > 0 {
		builder.WriteByte(' ')
	}
	builder.WriteString(text)
}

func collapseDocumentSeparators(text string) string {
	var builder strings.Builder
	builder.Grow(len(text))
	pendingSeparator := false
	for _, runeValue := range text {
		if unicode.IsSpace(runeValue) {
			pendingSeparator = builder.Len() > 0
			continue
		}
		if pendingSeparator {
			builder.WriteByte(' ')
			pendingSeparator = false
		}
		builder.WriteRune(runeValue)
	}
	return builder.String()
}

func truncateDocumentTextOnRuneBoundary(text string) string {
	if len(text) <= MaxDocumentTextBytes {
		return text
	}
	cut := MaxDocumentTextBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}
