package recordmarkdown

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"houfeng/internal/center/recordcollaboration"
)

var (
	atxHeadingPattern       = regexp.MustCompile(`^(#{1,6})[ \t]+(.+?)[ \t]*#*[ \t]*$`)
	setextHeadingPattern    = regexp.MustCompile(`^[ \t]{0,3}(=+|-+)[ \t]*$`)
	taskItemPattern         = regexp.MustCompile(`^[ \t]{0,3}[-*+] \[([ xX])\][ \t]+(.+)$`)
	orderedTaskItemPattern  = regexp.MustCompile(`^[ \t]{0,3}[1-9][0-9]{0,8}\. \[([ xX])\][ \t]+(.+)$`)
	footnoteDefPattern      = regexp.MustCompile(`^\[\^([^]]+)\]:[ \t]+(.+)$`)
	footnoteRefPattern      = regexp.MustCompile(`\[\^([^]]+)\]`)
	tableRulePattern        = regexp.MustCompile(`^[ \t]*\|?[ \t]*:?-{3,}:?[ \t]*(\|[ \t]*:?-{3,}:?[ \t]*)+\|?[ \t]*$`)
	rawHTMLPattern          = regexp.MustCompile(`(?i)<[/!?]?[a-z][^>]*>`)
	htmlCommentPattern      = regexp.MustCompile(`<!--|-->`)
	blockquotePrefixPattern = regexp.MustCompile(`^[ \t]{0,3}>[ \t]?`)
	listItemMarkerPattern   = regexp.MustCompile(`^([ \t]*)(?:[-*+]|[0-9]{1,9}\.)[ \t]`)
)

func ParseDocumentMarkdownV1(source string, authorized []DocumentReference) (DocumentRenderModel, error) {
	if !utf8.ValidString(source) || strings.ContainsRune(source, '\r') || hasUnsafeDocumentControl(source) {
		return DocumentRenderModel{}, ErrInvalidDocumentMarkdown
	}
	if len(source) > MaxDocumentMarkdownSourceBytes {
		return DocumentRenderModel{}, ErrInvalidDocumentMarkdown
	}
	if source == "" {
		return DocumentRenderModel{Version: DocumentRenderContractVersionV1, Nodes: []DocumentRenderNode{}}, nil
	}
	if err := ScanDocumentSource(source); err != nil {
		return DocumentRenderModel{}, ErrInvalidDocumentMarkdown
	}
	if hasUnrepresentableListStructure(strings.Split(source, "\n")) {
		return DocumentRenderModel{}, ErrInvalidDocumentMarkdown
	}
	if comment, err := recordcollaboration.ParseCommentMarkdownV1(source); err == nil {
		return documentFromComment(comment), nil
	}
	nodes, err := parseDocumentSource(source, authorizedReferenceSet(authorized))
	if err != nil {
		return DocumentRenderModel{}, ErrInvalidDocumentMarkdown
	}
	model := DocumentRenderModel{Version: DocumentRenderContractVersionV1, Nodes: nodes}
	if model.Validate() != nil {
		return DocumentRenderModel{}, ErrInvalidDocumentMarkdown
	}
	return model, nil
}

// hasUnrepresentableListStructure reports whether the source uses list shapes the
// shared dialect cannot express: an indented list item, or paragraph text that
// lazily continues an item. Both would flatten into literal text, so the document
// dialect refuses the source rather than publishing a render model that disagrees
// with how the Markdown actually reads.
func hasUnrepresentableListStructure(lines []string) bool {
	fenced := recordcollaboration.FencedCodeLineMaskV1(lines)
	insideList := false
	for index, line := range lines {
		if fenced[index] {
			insideList = false
			continue
		}
		if strings.TrimSpace(line) == "" {
			insideList = false
			continue
		}
		if indent, ok := listItemIndent(line); ok {
			if indent > 0 {
				return true
			}
			insideList = true
			continue
		}
		if insideList && !interruptsListItem(line) {
			return true
		}
		insideList = false
	}
	return false
}

// interruptsListItem reports whether line starts a block that ends a list item
// outright instead of continuing its paragraph.
func interruptsListItem(line string) bool {
	if _, fenced := recordcollaboration.FencedCodeOpeningV1(line); fenced {
		return true
	}
	return atxHeadingPattern.MatchString(line) || blockquotePrefixPattern.MatchString(line) ||
		isThematicBreak(line) || footnoteDefPattern.MatchString(line) ||
		houfengRefCommentPattern.MatchString(strings.TrimSpace(line))
}

func listItemIndent(line string) (int, bool) {
	matches := listItemMarkerPattern.FindStringSubmatch(line)
	if matches == nil {
		return 0, false
	}
	indent := 0
	for _, character := range matches[1] {
		if character == '\t' {
			indent += 4
			continue
		}
		indent++
	}
	return indent, true
}

func hasUnsafeDocumentControl(source string) bool {
	for _, character := range source {
		if unicode.IsControl(character) && character != '\n' && character != '\t' {
			return true
		}
	}
	return false
}

func documentFromComment(model recordcollaboration.CommentRenderModel) DocumentRenderModel {
	return DocumentRenderModel{
		Version: DocumentRenderContractVersionV1,
		Nodes:   documentNodesFromComment(model.Nodes),
	}
}

func documentNodesFromComment(nodes []recordcollaboration.CommentRenderNode) []DocumentRenderNode {
	converted := make([]DocumentRenderNode, 0, len(nodes))
	for _, node := range nodes {
		converted = append(converted, documentNodeFromComment(node))
	}
	return converted
}

func documentNodeFromComment(node recordcollaboration.CommentRenderNode) DocumentRenderNode {
	converted := DocumentRenderNode{Type: node.Type, Text: node.Text, Href: node.Href, Start: node.Start}
	if node.Children != nil {
		converted.Children = documentNodesFromComment(node.Children)
	}
	if node.Items != nil {
		converted.Items = make([][]DocumentRenderNode, len(node.Items))
		for index, item := range node.Items {
			converted.Items[index] = documentNodesFromComment(item)
		}
	}
	return converted
}

func parseDocumentSource(source string, allowed map[string]struct{}) ([]DocumentRenderNode, error) {
	parser := documentParser{lines: strings.Split(source, "\n"), allowed: allowed}
	return parser.parseBlocks()
}

type documentParser struct {
	lines   []string
	index   int
	allowed map[string]struct{}
}

func (parser *documentParser) parseBlocks() ([]DocumentRenderNode, error) {
	nodes := make([]DocumentRenderNode, 0)
	for parser.index < len(parser.lines) {
		if strings.TrimSpace(parser.lines[parser.index]) == "" {
			parser.index++
			continue
		}
		if parser.documentOnlyStart() {
			node, err := parser.parseDocumentBlock()
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, node)
			continue
		}
		shared, err := parser.parseSharedRegion()
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, shared...)
	}
	return nodes, nil
}

func (parser *documentParser) documentOnlyStart() bool {
	if parser.index >= len(parser.lines) {
		return false
	}
	line := parser.lines[parser.index]
	if _, fenced := recordcollaboration.FencedCodeOpeningV1(line); fenced {
		return true
	}
	if atxHeadingPattern.MatchString(line) || taskItemPattern.MatchString(line) || orderedTaskItemPattern.MatchString(line) ||
		footnoteDefPattern.MatchString(line) || houfengRefCommentPattern.MatchString(strings.TrimSpace(line)) ||
		blockquotePrefixPattern.MatchString(line) || isThematicBreak(line) ||
		(footnoteRefPattern.MatchString(line) && !footnoteDefPattern.MatchString(line)) {
		return true
	}
	if parser.index+1 < len(parser.lines) && strings.TrimSpace(line) != "" && setextHeadingPattern.MatchString(parser.lines[parser.index+1]) &&
		!tableRulePattern.MatchString(parser.lines[parser.index+1]) {
		return true
	}
	return parser.isTableStart()
}

func (parser *documentParser) isTableStart() bool {
	if parser.index+1 >= len(parser.lines) {
		return false
	}
	return looksLikeTableRow(parser.lines[parser.index]) && tableRulePattern.MatchString(parser.lines[parser.index+1])
}

func isThematicBreak(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 {
		return false
	}
	marker := rune(0)
	count := 0
	for _, character := range trimmed {
		if character == ' ' || character == '\t' {
			continue
		}
		if character != '-' && character != '*' && character != '_' {
			return false
		}
		if marker == 0 {
			marker = character
		}
		if character != marker {
			return false
		}
		count++
	}
	return count >= 3
}

func looksLikeTableRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.Count(trimmed, "|") >= 1 && !tableRulePattern.MatchString(line)
}

func (parser *documentParser) parseSharedRegion() ([]DocumentRenderNode, error) {
	start := parser.index
	for parser.index < len(parser.lines) && !parser.documentOnlyStart() {
		parser.index++
	}
	nodes := make([]DocumentRenderNode, 0)
	// Blank lines already separate blocks in the shared dialect, so parsing each
	// group on its own is equivalent to parsing the whole region while keeping
	// every delegated call well inside the shared per-region node ceiling.
	for _, group := range sharedBlockGroups(parser.lines[start:parser.index]) {
		converted, err := parseSharedBlocks(group)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, converted...)
	}
	return nodes, nil
}

func sharedBlockGroups(lines []string) []string {
	groups := make([]string, 0)
	current := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if len(current) > 0 {
				groups = append(groups, strings.Join(current, "\n"))
				current = current[:0]
			}
			continue
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		groups = append(groups, strings.Join(current, "\n"))
	}
	return groups
}

func (parser *documentParser) parseDocumentBlock() (DocumentRenderNode, error) {
	line := parser.lines[parser.index]
	if _, fenced := recordcollaboration.FencedCodeOpeningV1(line); fenced {
		return parser.parseFencedCode()
	}
	if houfengRefCommentPattern.MatchString(strings.TrimSpace(line)) {
		return parser.parseReference()
	}
	if matches := atxHeadingPattern.FindStringSubmatch(line); matches != nil {
		parser.index++
		children, err := parseSharedInlines(matches[2])
		if err != nil {
			return DocumentRenderNode{}, err
		}
		return DocumentRenderNode{Type: DocumentRenderNodeHeading, Level: uint64(len(matches[1])), Children: children}, nil
	}
	if parser.index+1 < len(parser.lines) && setextHeadingPattern.MatchString(parser.lines[parser.index+1]) &&
		!tableRulePattern.MatchString(parser.lines[parser.index+1]) {
		children, err := parseSharedInlines(strings.TrimSpace(line))
		if err != nil {
			return DocumentRenderNode{}, err
		}
		level := uint64(2)
		if strings.Contains(parser.lines[parser.index+1], "=") {
			level = 1
		}
		parser.index += 2
		return DocumentRenderNode{Type: DocumentRenderNodeHeading, Level: level, Children: children}, nil
	}
	if taskItemPattern.MatchString(line) || orderedTaskItemPattern.MatchString(line) {
		return parser.parseTaskList()
	}
	if footnoteDefPattern.MatchString(line) {
		return parser.parseFootnoteDef()
	}
	// Tables are matched before footnote references so a cell citing a footnote
	// still renders as a table instead of degrading into a paragraph.
	if parser.isTableStart() {
		return parser.parseTable()
	}
	if footnoteRefPattern.MatchString(line) {
		return parser.parseParagraphWithFootnotes()
	}
	if blockquotePrefixPattern.MatchString(line) {
		return parser.parseBlockquote()
	}
	if isThematicBreak(line) {
		parser.index++
		return DocumentRenderNode{Type: DocumentRenderNodeThematicBreak}, nil
	}
	return DocumentRenderNode{}, ErrInvalidDocumentMarkdown
}

// parseFencedCode owns fenced blocks at document level. Delegating them to the
// shared region parser would both split the fence body on document-looking lines
// and charge the fence against the comment source ceiling.
func (parser *documentParser) parseFencedCode() (DocumentRenderNode, error) {
	opening, ok := recordcollaboration.FencedCodeOpeningV1(parser.lines[parser.index])
	if !ok {
		return DocumentRenderNode{}, ErrInvalidDocumentMarkdown
	}
	parser.index++
	var body strings.Builder
	for parser.index < len(parser.lines) {
		line := parser.lines[parser.index]
		if recordcollaboration.IsFencedCodeClosingV1(line, opening) {
			parser.index++
			return DocumentRenderNode{Type: DocumentRenderNodeFencedCode, Text: body.String()}, nil
		}
		body.WriteString(line)
		body.WriteByte('\n')
		parser.index++
	}
	return DocumentRenderNode{}, ErrInvalidDocumentMarkdown
}

func (parser *documentParser) parseReference() (DocumentRenderNode, error) {
	commentLine := parser.lines[parser.index]
	parser.index++
	for parser.index < len(parser.lines) && strings.TrimSpace(parser.lines[parser.index]) == "" {
		parser.index++
	}
	if parser.index >= len(parser.lines) {
		return DocumentRenderNode{}, ErrInvalidDocumentMarkdown
	}
	linkLine := parser.lines[parser.index]
	parser.index++
	return parseHoufengReference(commentLine, linkLine, parser.allowed)
}

func (parser *documentParser) parseTaskList() (DocumentRenderNode, error) {
	items := make([]DocumentTaskItem, 0)
	for parser.index < len(parser.lines) {
		line := parser.lines[parser.index]
		matches := taskItemPattern.FindStringSubmatch(line)
		if matches == nil {
			matches = orderedTaskItemPattern.FindStringSubmatch(line)
		}
		if matches == nil {
			break
		}
		children, err := parseSharedInlines(matches[2])
		if err != nil {
			return DocumentRenderNode{}, err
		}
		items = append(items, DocumentTaskItem{Checked: strings.EqualFold(matches[1], "x"), Children: children})
		parser.index++
	}
	if len(items) == 0 {
		return DocumentRenderNode{}, ErrInvalidDocumentMarkdown
	}
	return DocumentRenderNode{Type: DocumentRenderNodeTaskList, TaskItems: items}, nil
}

func (parser *documentParser) parseFootnoteDef() (DocumentRenderNode, error) {
	matches := footnoteDefPattern.FindStringSubmatch(parser.lines[parser.index])
	if matches == nil {
		return DocumentRenderNode{}, ErrInvalidDocumentMarkdown
	}
	parser.index++
	children, err := parseSharedBlocks(matches[2])
	if err != nil {
		return DocumentRenderNode{}, err
	}
	return DocumentRenderNode{Type: DocumentRenderNodeFootnoteDef, Text: matches[1], Children: children}, nil
}

func (parser *documentParser) parseBlockquote() (DocumentRenderNode, error) {
	inner := make([]string, 0)
	for parser.index < len(parser.lines) {
		line := parser.lines[parser.index]
		if !blockquotePrefixPattern.MatchString(line) {
			break
		}
		inner = append(inner, blockquotePrefixPattern.ReplaceAllString(line, ""))
		parser.index++
	}
	children, err := parseSharedBlocks(strings.Join(inner, "\n"))
	if err != nil {
		return DocumentRenderNode{}, err
	}
	return DocumentRenderNode{Type: DocumentRenderNodeBlockquote, Children: children}, nil
}

func (parser *documentParser) parseTable() (DocumentRenderNode, error) {
	header, err := parseTableRow(parser.lines[parser.index])
	if err != nil {
		return DocumentRenderNode{}, err
	}
	parser.index += 2
	rows := make([][][]DocumentRenderNode, 0)
	for parser.index < len(parser.lines) {
		line := parser.lines[parser.index]
		if strings.TrimSpace(line) == "" || !looksLikeTableRow(line) {
			break
		}
		row, err := parseTableRow(line)
		if err != nil || len(row) != len(header) {
			return DocumentRenderNode{}, ErrInvalidDocumentMarkdown
		}
		rows = append(rows, row)
		parser.index++
	}
	return DocumentRenderNode{Type: DocumentRenderNodeTable, Header: header, Rows: rows}, nil
}

func parseTableRow(line string) ([][]DocumentRenderNode, error) {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	cells := strings.Split(trimmed, "|")
	row := make([][]DocumentRenderNode, 0, len(cells))
	for _, cell := range cells {
		children, err := parseTableCell(strings.TrimSpace(cell))
		if err != nil {
			return nil, err
		}
		row = append(row, children)
	}
	if len(row) == 0 {
		return nil, ErrInvalidDocumentMarkdown
	}
	return row, nil
}

// parseTableCell keeps blank cells representable: the render contract requires at
// least one node per cell, so an empty cell carries a single space.
func parseTableCell(cell string) ([]DocumentRenderNode, error) {
	if cell == "" {
		return []DocumentRenderNode{{Type: DocumentRenderNodeText, Text: " "}}, nil
	}
	if footnoteRefPattern.MatchString(cell) {
		return parseInlinesWithFootnotes(cell)
	}
	return parseSharedInlines(cell)
}

func parseSharedInlines(source string) ([]DocumentRenderNode, error) {
	if strings.TrimSpace(source) == "" || containsForbiddenDocumentHTML(source) {
		return nil, ErrInvalidDocumentMarkdown
	}
	comment, err := recordcollaboration.ParseCommentMarkdownV1(source)
	if err != nil || len(comment.Nodes) != 1 || comment.Nodes[0].Type != recordcollaboration.CommentRenderNodeParagraph {
		return nil, ErrInvalidDocumentMarkdown
	}
	return documentNodesFromComment(comment.Nodes[0].Children), nil
}

func parseSharedBlocks(source string) ([]DocumentRenderNode, error) {
	if strings.TrimSpace(source) == "" || containsForbiddenDocumentHTML(source) {
		return nil, ErrInvalidDocumentMarkdown
	}
	comment, err := recordcollaboration.ParseSharedMarkdownRegionV1(strings.TrimRight(source, "\n"), MaxDocumentMarkdownSourceBytes)
	if err != nil {
		return nil, ErrInvalidDocumentMarkdown
	}
	return documentNodesFromComment(comment.Nodes), nil
}

func containsForbiddenDocumentHTML(source string) bool {
	withoutRefs := houfengRefCommentPattern.ReplaceAllString(source, "")
	return rawHTMLPattern.MatchString(withoutRefs) || htmlCommentPattern.MatchString(withoutRefs) || strings.Contains(source, "![")
}

func (parser *documentParser) parseParagraphWithFootnotes() (DocumentRenderNode, error) {
	lines := make([]string, 0)
	for parser.index < len(parser.lines) {
		line := parser.lines[parser.index]
		if strings.TrimSpace(line) == "" || footnoteDefPattern.MatchString(line) {
			break
		}
		if parser.documentOnlyStart() && !footnoteRefPattern.MatchString(line) {
			break
		}
		lines = append(lines, line)
		parser.index++
	}
	children, err := parseInlinesWithFootnotes(strings.Join(lines, "\n"))
	if err != nil {
		return DocumentRenderNode{}, err
	}
	return DocumentRenderNode{Type: DocumentRenderNodeParagraph, Children: children}, nil
}

func parseInlinesWithFootnotes(source string) ([]DocumentRenderNode, error) {
	if containsForbiddenDocumentHTML(source) {
		return nil, ErrInvalidDocumentMarkdown
	}
	children := make([]DocumentRenderNode, 0)
	last := 0
	for _, match := range footnoteRefPattern.FindAllStringSubmatchIndex(source, -1) {
		segment, err := footnoteSegmentInlines(source[last:match[0]])
		if err != nil {
			return nil, err
		}
		children = append(children, segment...)
		children = append(children, DocumentRenderNode{Type: DocumentRenderNodeFootnoteRef, Text: source[match[2]:match[3]]})
		last = match[1]
	}
	segment, err := footnoteSegmentInlines(source[last:])
	if err != nil {
		return nil, err
	}
	children = append(children, segment...)
	if len(children) == 0 {
		return nil, ErrInvalidDocumentMarkdown
	}
	return children, nil
}

// footnoteSegmentInlines parses the text between footnote references. Spacing
// around a reference is meaningful, so a whitespace-only segment is kept as text
// rather than rejected as empty inline content.
func footnoteSegmentInlines(segment string) ([]DocumentRenderNode, error) {
	if segment == "" {
		return nil, nil
	}
	if strings.TrimSpace(segment) == "" {
		return []DocumentRenderNode{{Type: DocumentRenderNodeText, Text: segment}}, nil
	}
	return parseSharedInlines(segment)
}
