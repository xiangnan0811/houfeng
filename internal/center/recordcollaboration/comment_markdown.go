package recordcollaboration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxCommentMarkdownSourceBytes = 16_384
	MaxCommentRenderNodes         = 512
	MaxCommentRenderDepth         = 8
	MaxCommentLinkBytes           = 2_048
)

var ErrInvalidCommentMarkdown = errors.New("invalid comment markdown")

const (
	CommentRenderNodeParagraph     = "paragraph"
	CommentRenderNodeText          = "text"
	CommentRenderNodeLineBreak     = "line_break"
	CommentRenderNodeEmphasis      = "emphasis"
	CommentRenderNodeStrong        = "strong"
	CommentRenderNodeStrikethrough = "strikethrough"
	CommentRenderNodeInlineCode    = "inline_code"
	CommentRenderNodeFencedCode    = "fenced_code"
	CommentRenderNodeOrderedList   = "ordered_list"
	CommentRenderNodeUnorderedList = "unordered_list"
	CommentRenderNodeLink          = "link"
)

// CommentRenderModel is the only durable render shape for
// comment_markdown/v1. It deliberately cannot carry HTML or arbitrary JSON.
type CommentRenderModel struct {
	Version string              `json:"version"`
	Nodes   []CommentRenderNode `json:"nodes"`
}

// CommentRenderNode is a closed tagged union. Validate rejects every field
// that is not meaningful for the selected Type.
type CommentRenderNode struct {
	Type     string                `json:"type"`
	Text     string                `json:"text,omitempty"`
	Href     string                `json:"href,omitempty"`
	Start    uint64                `json:"start,omitempty"`
	Children []CommentRenderNode   `json:"children,omitempty"`
	Items    [][]CommentRenderNode `json:"items,omitempty"`
}

func (node CommentRenderNode) MarshalJSON() ([]byte, error) {
	count := 0
	if err := validateCommentRenderNode(node, 1, &count, false); err != nil {
		return nil, err
	}
	switch node.Type {
	case CommentRenderNodeParagraph, CommentRenderNodeEmphasis, CommentRenderNodeStrong, CommentRenderNodeStrikethrough:
		return json.Marshal(struct {
			Type     string              `json:"type"`
			Children []CommentRenderNode `json:"children"`
		}{Type: node.Type, Children: node.Children})
	case CommentRenderNodeText, CommentRenderNodeInlineCode, CommentRenderNodeFencedCode:
		return json.Marshal(struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: node.Type, Text: node.Text})
	case CommentRenderNodeLineBreak:
		return json.Marshal(struct {
			Type string `json:"type"`
		}{Type: node.Type})
	case CommentRenderNodeOrderedList:
		return json.Marshal(struct {
			Type  string                `json:"type"`
			Start uint64                `json:"start"`
			Items [][]CommentRenderNode `json:"items"`
		}{Type: node.Type, Start: node.Start, Items: node.Items})
	case CommentRenderNodeUnorderedList:
		return json.Marshal(struct {
			Type  string                `json:"type"`
			Items [][]CommentRenderNode `json:"items"`
		}{Type: node.Type, Items: node.Items})
	case CommentRenderNodeLink:
		return json.Marshal(struct {
			Type     string              `json:"type"`
			Href     string              `json:"href"`
			Children []CommentRenderNode `json:"children"`
		}{Type: node.Type, Href: node.Href, Children: node.Children})
	default:
		return nil, ErrInvalidCommentMarkdown
	}
}

func (node *CommentRenderNode) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&fields); err != nil || fields == nil || ensureCommentJSONEOF(decoder) != nil {
		return ErrInvalidCommentMarkdown
	}
	var nodeType string
	if err := json.Unmarshal(fields["type"], &nodeType); err != nil || !commentRenderNodeHasExactKeys(fields, nodeType) {
		return ErrInvalidCommentMarkdown
	}
	type commentRenderNodeWire CommentRenderNode
	var decoded commentRenderNodeWire
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil || ensureCommentJSONEOF(decoder) != nil {
		return ErrInvalidCommentMarkdown
	}
	*node = CommentRenderNode(decoded)
	return nil
}

func commentRenderNodeHasExactKeys(fields map[string]json.RawMessage, nodeType string) bool {
	var expected []string
	switch nodeType {
	case CommentRenderNodeParagraph, CommentRenderNodeEmphasis, CommentRenderNodeStrong, CommentRenderNodeStrikethrough:
		expected = []string{"children", "type"}
	case CommentRenderNodeText, CommentRenderNodeInlineCode, CommentRenderNodeFencedCode:
		expected = []string{"text", "type"}
	case CommentRenderNodeLineBreak:
		expected = []string{"type"}
	case CommentRenderNodeOrderedList:
		expected = []string{"items", "start", "type"}
	case CommentRenderNodeUnorderedList:
		expected = []string{"items", "type"}
	case CommentRenderNodeLink:
		expected = []string{"children", "href", "type"}
	default:
		return false
	}
	if len(fields) != len(expected) {
		return false
	}
	for _, key := range expected {
		if _, ok := fields[key]; !ok {
			return false
		}
	}
	switch nodeType {
	case CommentRenderNodeText, CommentRenderNodeInlineCode, CommentRenderNodeFencedCode:
		return commentJSONRawString(fields["text"])
	case CommentRenderNodeLink:
		return commentJSONRawString(fields["href"])
	}
	return true
}

func commentJSONRawString(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) >= 2 && trimmed[0] == '"'
}

func (model CommentRenderModel) Validate() error {
	if model.Version != CommentRenderContractVersionV1 || len(model.Nodes) == 0 {
		return ErrInvalidCommentMarkdown
	}
	count := 0
	for _, node := range model.Nodes {
		switch node.Type {
		case CommentRenderNodeParagraph, CommentRenderNodeFencedCode, CommentRenderNodeOrderedList, CommentRenderNodeUnorderedList:
		default:
			return ErrInvalidCommentMarkdown
		}
		if err := validateCommentRenderNode(node, 1, &count, false); err != nil {
			return err
		}
	}
	return nil
}

func (model CommentRenderModel) Clone() CommentRenderModel {
	cloned := CommentRenderModel{Version: model.Version, Nodes: cloneCommentRenderNodes(model.Nodes)}
	return cloned
}

func (model CommentRenderModel) Equal(other CommentRenderModel) bool {
	return reflect.DeepEqual(model, other)
}

func DecodeCommentRenderModelV1(raw []byte) (CommentRenderModel, error) {
	if len(raw) == 0 {
		return CommentRenderModel{}, ErrInvalidCommentMarkdown
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var model CommentRenderModel
	if err := decoder.Decode(&model); err != nil {
		return CommentRenderModel{}, ErrInvalidCommentMarkdown
	}
	if err := ensureCommentJSONEOF(decoder); err != nil || model.Validate() != nil {
		return CommentRenderModel{}, ErrInvalidCommentMarkdown
	}
	return model.Clone(), nil
}

func ensureCommentJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ErrInvalidCommentMarkdown
	}
	return nil
}

func ParseCommentMarkdownV1(source string) (CommentRenderModel, error) {
	return parseCommentMarkdownV1(source, MaxCommentMarkdownSourceBytes)
}

// ParseSharedMarkdownRegionV1 parses a region that uses only comment_markdown/v1
// constructs while applying the caller's source budget. Document Markdown reuses
// this shared block/inline core for its shared regions and must not inherit the
// comment source ceiling; per-region node and depth ceilings still apply.
func ParseSharedMarkdownRegionV1(source string, maxSourceBytes int) (CommentRenderModel, error) {
	if maxSourceBytes < MaxCommentMarkdownSourceBytes {
		maxSourceBytes = MaxCommentMarkdownSourceBytes
	}
	return parseCommentMarkdownV1(source, maxSourceBytes)
}

// FencedCodeOpeningV1 returns the fence marker run when line opens a fenced code
// block. Document Markdown owns its own fenced blocks but must recognise them
// with exactly these rules so both dialects agree on fence boundaries.
func FencedCodeOpeningV1(line string) (string, bool) {
	fence := commentFencePattern.FindStringSubmatch(line)
	if fence == nil {
		return "", false
	}
	return fence[1], true
}

// IsFencedCodeClosingV1 reports whether line closes a fence opened by opening.
func IsFencedCodeClosingV1(line string, opening string) bool {
	if opening == "" {
		return false
	}
	return isClosingCommentFence(line, opening[0], len(opening))
}

// FencedCodeLineMaskV1 marks every line that belongs to a fenced code block,
// fence delimiters included. Callers that inspect Markdown line by line use it so
// code content is never mistaken for document structure.
func FencedCodeLineMaskV1(lines []string) []bool {
	mask := make([]bool, len(lines))
	for index := 0; index < len(lines); index++ {
		opening, ok := FencedCodeOpeningV1(lines[index])
		if !ok {
			continue
		}
		mask[index] = true
		for index+1 < len(lines) {
			index++
			mask[index] = true
			if IsFencedCodeClosingV1(lines[index], opening) {
				break
			}
		}
	}
	return mask
}

func parseCommentMarkdownV1(source string, maxSourceBytes int) (CommentRenderModel, error) {
	if len(source) == 0 || len(source) > maxSourceBytes || !utf8.ValidString(source) ||
		strings.ContainsRune(source, '\r') || hasUnsafeCommentControl(source) {
		return CommentRenderModel{}, ErrInvalidCommentMarkdown
	}
	parser := commentMarkdownParser{lines: strings.Split(source, "\n")}
	nodes, err := parser.parseBlocks()
	if err != nil {
		return CommentRenderModel{}, ErrInvalidCommentMarkdown
	}
	model := CommentRenderModel{Version: CommentRenderContractVersionV1, Nodes: nodes}
	if model.Validate() != nil {
		return CommentRenderModel{}, ErrInvalidCommentMarkdown
	}
	return model, nil
}

func hasUnsafeCommentControl(source string) bool {
	for _, character := range source {
		if unicode.IsControl(character) && character != '\n' && character != '\t' {
			return true
		}
	}
	return false
}

var (
	commentFencePattern       = regexp.MustCompile(`^(` + "`{3,}" + `|~{3,})([A-Za-z0-9_+.-]{0,32})[ \t]*$`)
	commentOrderedItemPattern = regexp.MustCompile(`^([1-9][0-9]{0,8})\. (.*)$`)
	commentTableRulePattern   = regexp.MustCompile(`^[ \t]*\|?[ \t]*:?-{3,}:?[ \t]*(\|[ \t]*:?-{3,}:?[ \t]*)+\|?[ \t]*$`)
	commentHeadingPattern     = regexp.MustCompile(`^[ \t]{0,3}#{1,6}([ \t]+|$)`)
	commentTaskItemPattern    = regexp.MustCompile(`^[ \t]{0,3}[-+*] \[[ xX]\]([ \t]+|$)`)
	commentOrderedTaskPattern = regexp.MustCompile(`^[ \t]{0,3}[1-9][0-9]{0,8}\. \[[ xX]\]([ \t]+|$)`)
	commentSetextPattern      = regexp.MustCompile(`^[ \t]{0,3}=+[ \t]*$`)
	commentFootnotePattern    = regexp.MustCompile(`(^|[^\\])\[\^[^]]+\]`)
	commentRawHTMLPattern     = regexp.MustCompile(`(?i)<[/!?]?[a-z][^>]*>`)
	commentHTMLCommentPattern = regexp.MustCompile(`<!--|-->`)
)

type commentMarkdownParser struct {
	lines []string
	index int
}

func (parser *commentMarkdownParser) parseBlocks() ([]CommentRenderNode, error) {
	nodes := make([]CommentRenderNode, 0)
	for parser.index < len(parser.lines) {
		line := parser.lines[parser.index]
		if strings.TrimSpace(line) == "" {
			parser.index++
			continue
		}
		if fence := commentFencePattern.FindStringSubmatch(line); fence != nil {
			node, err := parser.parseFence(fence[1])
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, node)
			continue
		}
		if err := validateCommentBlockLine(line); err != nil {
			return nil, err
		}
		if unorderedCommentItem(line) != "" {
			node, err := parser.parseUnorderedList()
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, node)
			continue
		}
		if commentOrderedItemPattern.MatchString(line) {
			node, err := parser.parseOrderedList()
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, node)
			continue
		}
		node, err := parser.parseParagraph()
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func (parser *commentMarkdownParser) parseFence(opening string) (CommentRenderNode, error) {
	marker := opening[0]
	minimum := len(opening)
	parser.index++
	var body strings.Builder
	for parser.index < len(parser.lines) {
		line := parser.lines[parser.index]
		if isClosingCommentFence(line, marker, minimum) {
			parser.index++
			return CommentRenderNode{Type: CommentRenderNodeFencedCode, Text: body.String()}, nil
		}
		body.WriteString(line)
		body.WriteByte('\n')
		parser.index++
	}
	return CommentRenderNode{}, ErrInvalidCommentMarkdown
}

func isClosingCommentFence(line string, marker byte, minimum int) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < minimum {
		return false
	}
	for index := range trimmed {
		if trimmed[index] != marker {
			return false
		}
	}
	return true
}

func (parser *commentMarkdownParser) parseUnorderedList() (CommentRenderNode, error) {
	items := make([][]CommentRenderNode, 0)
	for parser.index < len(parser.lines) {
		line := parser.lines[parser.index]
		item := unorderedCommentItem(line)
		if item == "" {
			break
		}
		if err := validateCommentBlockLine(line); err != nil {
			return CommentRenderNode{}, err
		}
		children, err := parseCommentInline(item)
		if err != nil || len(children) == 0 {
			return CommentRenderNode{}, ErrInvalidCommentMarkdown
		}
		items = append(items, children)
		parser.index++
	}
	return CommentRenderNode{Type: CommentRenderNodeUnorderedList, Items: items}, nil
}

func unorderedCommentItem(line string) string {
	if len(line) >= 3 && (line[0] == '-' || line[0] == '+' || line[0] == '*') && line[1] == ' ' {
		return line[2:]
	}
	return ""
}

func (parser *commentMarkdownParser) parseOrderedList() (CommentRenderNode, error) {
	items := make([][]CommentRenderNode, 0)
	start := uint64(0)
	expected := uint64(0)
	for parser.index < len(parser.lines) {
		match := commentOrderedItemPattern.FindStringSubmatch(parser.lines[parser.index])
		if match == nil {
			break
		}
		ordinal, err := strconv.ParseUint(match[1], 10, 64)
		if err != nil || ordinal == 0 || (expected != 0 && ordinal != expected) {
			return CommentRenderNode{}, ErrInvalidCommentMarkdown
		}
		if start == 0 {
			start = ordinal
		}
		expected = ordinal + 1
		children, err := parseCommentInline(match[2])
		if err != nil || len(children) == 0 {
			return CommentRenderNode{}, ErrInvalidCommentMarkdown
		}
		items = append(items, children)
		parser.index++
	}
	return CommentRenderNode{Type: CommentRenderNodeOrderedList, Start: start, Items: items}, nil
}

func (parser *commentMarkdownParser) parseParagraph() (CommentRenderNode, error) {
	children := make([]CommentRenderNode, 0)
	for parser.index < len(parser.lines) {
		line := parser.lines[parser.index]
		if strings.TrimSpace(line) == "" || (len(children) > 0 && isCommentBlockStart(line)) {
			break
		}
		if err := validateCommentBlockLine(line); err != nil {
			return CommentRenderNode{}, err
		}
		inline, err := parseCommentInline(line)
		if err != nil || len(inline) == 0 {
			return CommentRenderNode{}, ErrInvalidCommentMarkdown
		}
		if len(children) > 0 {
			children = append(children, CommentRenderNode{Type: CommentRenderNodeLineBreak})
		}
		children = append(children, inline...)
		parser.index++
	}
	return CommentRenderNode{Type: CommentRenderNodeParagraph, Children: children}, nil
}

func isCommentBlockStart(line string) bool {
	return commentFencePattern.MatchString(line) || unorderedCommentItem(line) != "" || commentOrderedItemPattern.MatchString(line)
}

func validateCommentBlockLine(line string) error {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(line)
	if commentHeadingPattern.MatchString(line) || commentTaskItemPattern.MatchString(line) || commentOrderedTaskPattern.MatchString(line) ||
		commentSetextPattern.MatchString(line) ||
		commentTableRulePattern.MatchString(line) || commentFootnotePattern.MatchString(line) ||
		commentRawHTMLPattern.MatchString(line) || commentHTMLCommentPattern.MatchString(line) ||
		strings.HasPrefix(strings.TrimLeft(line, " \t"), ">") ||
		trimmed == "---" || trimmed == "***" || trimmed == "___" ||
		strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") ||
		strings.Contains(lower, "[[attachment:") || strings.Contains(lower, "[[evidence:") ||
		strings.Contains(line, "![") {
		return ErrInvalidCommentMarkdown
	}
	return nil
}

func parseCommentInline(source string) ([]CommentRenderNode, error) {
	if source == "" {
		return nil, ErrInvalidCommentMarkdown
	}
	if nested, ok := parseRepeatedStrongWrapper(source); ok {
		return []CommentRenderNode{nested}, nil
	}
	nodes := make([]CommentRenderNode, 0)
	for len(source) > 0 {
		if source[0] == '\\' && len(source) > 1 && strings.ContainsRune(`\\`+"`*_{}[]()#+-.!~", rune(source[1])) {
			nodes = appendCommentText(nodes, source[1:2])
			source = source[2:]
			continue
		}
		if strings.HasPrefix(source, "![") {
			return nil, ErrInvalidCommentMarkdown
		}
		if strings.HasPrefix(source, "[") {
			node, consumed, ok, err := parseCommentLink(source)
			if err != nil {
				return nil, err
			}
			if ok {
				nodes = append(nodes, node)
				source = source[consumed:]
				continue
			}
		}
		if source[0] == '`' {
			closing := strings.IndexByte(source[1:], '`')
			if closing >= 0 {
				text := source[1 : closing+1]
				if text == "" || strings.ContainsRune(text, '\n') {
					return nil, ErrInvalidCommentMarkdown
				}
				nodes = append(nodes, CommentRenderNode{Type: CommentRenderNodeInlineCode, Text: text})
				source = source[closing+2:]
				continue
			}
		}
		matched := false
		for _, delimiter := range []struct{ marker, kind string }{
			{"**", CommentRenderNodeStrong}, {"__", CommentRenderNodeStrong},
			{"~~", CommentRenderNodeStrikethrough}, {"*", CommentRenderNodeEmphasis}, {"_", CommentRenderNodeEmphasis},
		} {
			if !strings.HasPrefix(source, delimiter.marker) {
				continue
			}
			closing := strings.Index(source[len(delimiter.marker):], delimiter.marker)
			if closing < 0 {
				continue
			}
			closing += len(delimiter.marker)
			content := source[len(delimiter.marker):closing]
			if content == "" {
				continue
			}
			children, err := parseCommentInline(content)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, CommentRenderNode{Type: delimiter.kind, Children: children})
			source = source[closing+len(delimiter.marker):]
			matched = true
			break
		}
		if matched {
			continue
		}
		next := nextCommentInlineMarker(source)
		nodes = appendCommentText(nodes, source[:next])
		source = source[next:]
	}
	return nodes, nil
}

func parseRepeatedStrongWrapper(source string) (CommentRenderNode, bool) {
	leading := 0
	for leading < len(source) && source[leading] == '*' {
		leading++
	}
	trailing := 0
	for trailing < len(source)-leading && source[len(source)-1-trailing] == '*' {
		trailing++
	}
	if leading < 4 || leading != trailing || leading%2 != 0 {
		return CommentRenderNode{}, false
	}
	content := source[leading : len(source)-trailing]
	if content == "" || strings.ContainsRune(content, '*') {
		return CommentRenderNode{}, false
	}
	node := CommentRenderNode{Type: CommentRenderNodeText, Text: content}
	for index := 0; index < leading/2; index++ {
		node = CommentRenderNode{Type: CommentRenderNodeStrong, Children: []CommentRenderNode{node}}
	}
	return node, true
}

func parseCommentLink(source string) (CommentRenderNode, int, bool, error) {
	separator := strings.Index(source, "](")
	if separator <= 1 {
		return CommentRenderNode{}, 0, false, nil
	}
	closing := findCommentLinkClosingParen(source, separator+2)
	if closing < 0 {
		return CommentRenderNode{}, 0, false, nil
	}
	label := source[1:separator]
	if strings.Contains(label, "[") || strings.Contains(label, "]") {
		return CommentRenderNode{}, 0, false, ErrInvalidCommentMarkdown
	}
	href := source[separator+2 : closing]
	if err := validateCanonicalCommentLink(href); err != nil {
		return CommentRenderNode{}, 0, false, err
	}
	children, err := parseCommentInline(label)
	if err != nil || len(children) == 0 {
		return CommentRenderNode{}, 0, false, ErrInvalidCommentMarkdown
	}
	return CommentRenderNode{Type: CommentRenderNodeLink, Href: href, Children: children}, closing + 1, true, nil
}

func findCommentLinkClosingParen(source string, start int) int {
	depth := 0
	for index := start; index < len(source); index++ {
		switch source[index] {
		case '\\':
			index++
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return index
			}
			depth--
		}
	}
	return -1
}

// ValidateCanonicalHTTPLink reports whether href is a byte-canonical http(s)
// URL accepted by comment_markdown/v1. Document Markdown reuses this contract
// for ordinary links.
func ValidateCanonicalHTTPLink(href string) error {
	return validateCanonicalCommentLink(href)
}

func validateCanonicalCommentLink(href string) error {
	if len(href) == 0 || len(href) > MaxCommentLinkBytes || !utf8.ValidString(href) ||
		strings.ContainsAny(href, " \t\n\r\\\"'<>\x00") {
		return ErrInvalidCommentMarkdown
	}
	for index := 0; index < len(href); index++ {
		if href[index] > unicode.MaxASCII || href[index] < 0x21 {
			return ErrInvalidCommentMarkdown
		}
		if href[index] == '%' {
			if index+2 >= len(href) || !isUpperHexPair(href[index+1], href[index+2]) {
				return ErrInvalidCommentMarkdown
			}
			index += 2
		}
	}
	parsed, err := url.Parse(href)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil ||
		parsed.Host == "" || parsed.Hostname() == "" || parsed.Host != strings.ToLower(parsed.Host) || parsed.String() != href {
		return ErrInvalidCommentMarkdown
	}
	if !isCanonicalCommentHost(parsed.Hostname()) {
		return ErrInvalidCommentMarkdown
	}
	if !isCanonicalCommentPort(parsed.Scheme, parsed.Host) {
		return ErrInvalidCommentMarkdown
	}
	if hasCommentDotPathSegment(parsed.EscapedPath()) {
		return ErrInvalidCommentMarkdown
	}
	return nil
}

func isCanonicalCommentHost(host string) bool {
	if address, err := netip.ParseAddr(host); err == nil {
		return address.Zone() == "" && address.String() == host
	}
	if isLegacyNumericCommentHost(host) {
		return false
	}
	name := strings.TrimSuffix(host, ".")
	if name == "" || len(name) > 253 || (name != host && len(host) > 254) {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for index := range label {
			character := label[index]
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}

func isLegacyNumericCommentHost(host string) bool {
	candidate := strings.TrimSuffix(host, ".")
	parts := strings.Split(candidate, ".")
	if candidate == "" {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		if strings.HasPrefix(part, "0x") {
			if len(part) == 2 || !isCommentHostDigits(part[2:], 16) {
				return false
			}
			continue
		}
		if !isCommentHostDigits(part, 10) {
			return false
		}
	}
	return true
}

func isCommentHostDigits(value string, base int) bool {
	for index := range value {
		character := value[index]
		if character >= '0' && character <= '9' {
			continue
		}
		if base == 16 && character >= 'a' && character <= 'f' {
			continue
		}
		return false
	}
	return value != ""
}

func isCanonicalCommentPort(scheme, authority string) bool {
	port, present, ok := commentAuthorityPort(authority)
	if !ok || (present && (port == "" || (len(port) > 1 && port[0] == '0'))) {
		return false
	}
	if !present {
		return true
	}
	for index := range port {
		if port[index] < '0' || port[index] > '9' {
			return false
		}
	}
	value, err := strconv.ParseUint(port, 10, 16)
	if err != nil || value == 0 || (scheme == "http" && value == 80) || (scheme == "https" && value == 443) {
		return false
	}
	return true
}

func commentAuthorityPort(authority string) (string, bool, bool) {
	if strings.HasPrefix(authority, "[") {
		closing := strings.LastIndexByte(authority, ']')
		if closing < 0 {
			return "", false, false
		}
		suffix := authority[closing+1:]
		if suffix == "" {
			return "", false, true
		}
		if suffix[0] != ':' {
			return "", false, false
		}
		return suffix[1:], true, true
	}
	colon := strings.LastIndexByte(authority, ':')
	if colon < 0 {
		return "", false, true
	}
	if strings.ContainsRune(authority[:colon], ':') {
		return "", false, false
	}
	return authority[colon+1:], true, true
}

func hasCommentDotPathSegment(escapedPath string) bool {
	for _, segment := range strings.Split(escapedPath, "/") {
		normalized := strings.ReplaceAll(segment, "%2E", ".")
		if normalized == "." || normalized == ".." {
			return true
		}
	}
	return false
}

func isUpperHexPair(first, second byte) bool {
	return isUpperHex(first) && isUpperHex(second)
}

func isUpperHex(value byte) bool {
	return (value >= '0' && value <= '9') || (value >= 'A' && value <= 'F')
}

func nextCommentInlineMarker(source string) int {
	for index := 1; index < len(source); index++ {
		if strings.ContainsRune("\\![`*_~", rune(source[index])) {
			return index
		}
	}
	return len(source)
}

func appendCommentText(nodes []CommentRenderNode, text string) []CommentRenderNode {
	if text == "" {
		return nodes
	}
	if len(nodes) > 0 && nodes[len(nodes)-1].Type == CommentRenderNodeText {
		nodes[len(nodes)-1].Text += text
		return nodes
	}
	return append(nodes, CommentRenderNode{Type: CommentRenderNodeText, Text: text})
}

func validateCommentRenderNode(node CommentRenderNode, depth int, count *int, inlineOnly bool) error {
	*count++
	if depth > MaxCommentRenderDepth || *count > MaxCommentRenderNodes {
		return ErrInvalidCommentMarkdown
	}
	emptyChildren := len(node.Children) == 0
	emptyItems := len(node.Items) == 0
	noScalar := node.Text == "" && node.Href == "" && node.Start == 0
	switch node.Type {
	case CommentRenderNodeParagraph:
		if inlineOnly || emptyChildren || !emptyItems || !noScalar {
			return ErrInvalidCommentMarkdown
		}
		return validateCommentInlineChildren(node.Children, depth+1, count)
	case CommentRenderNodeText:
		if node.Text == "" || node.Href != "" || node.Start != 0 || !emptyChildren || !emptyItems || !utf8.ValidString(node.Text) {
			return ErrInvalidCommentMarkdown
		}
		return nil
	case CommentRenderNodeLineBreak:
		if !noScalar || !emptyChildren || !emptyItems {
			return ErrInvalidCommentMarkdown
		}
		return nil
	case CommentRenderNodeEmphasis, CommentRenderNodeStrong, CommentRenderNodeStrikethrough:
		if emptyChildren || !emptyItems || !noScalar {
			return ErrInvalidCommentMarkdown
		}
		return validateCommentInlineChildren(node.Children, depth+1, count)
	case CommentRenderNodeInlineCode:
		if node.Text == "" || node.Href != "" || node.Start != 0 || !emptyChildren || !emptyItems || !utf8.ValidString(node.Text) {
			return ErrInvalidCommentMarkdown
		}
		return nil
	case CommentRenderNodeFencedCode:
		if inlineOnly || node.Href != "" || node.Start != 0 || !emptyChildren || !emptyItems || !utf8.ValidString(node.Text) {
			return ErrInvalidCommentMarkdown
		}
		return nil
	case CommentRenderNodeLink:
		if emptyChildren || !emptyItems || node.Text != "" || node.Start != 0 || validateCanonicalCommentLink(node.Href) != nil {
			return ErrInvalidCommentMarkdown
		}
		return validateCommentInlineChildren(node.Children, depth+1, count)
	case CommentRenderNodeOrderedList, CommentRenderNodeUnorderedList:
		if inlineOnly || !emptyChildren || node.Text != "" || node.Href != "" || emptyItems ||
			(node.Type == CommentRenderNodeOrderedList && node.Start == 0) ||
			(node.Type == CommentRenderNodeUnorderedList && node.Start != 0) {
			return ErrInvalidCommentMarkdown
		}
		for _, item := range node.Items {
			if len(item) == 0 || validateCommentInlineChildren(item, depth+1, count) != nil {
				return ErrInvalidCommentMarkdown
			}
		}
		return nil
	default:
		return fmt.Errorf("%w: node type", ErrInvalidCommentMarkdown)
	}
}

func validateCommentInlineChildren(children []CommentRenderNode, depth int, count *int) error {
	for _, child := range children {
		switch child.Type {
		case CommentRenderNodeText, CommentRenderNodeLineBreak, CommentRenderNodeEmphasis,
			CommentRenderNodeStrong, CommentRenderNodeStrikethrough, CommentRenderNodeInlineCode, CommentRenderNodeLink:
		default:
			return ErrInvalidCommentMarkdown
		}
		if err := validateCommentRenderNode(child, depth, count, true); err != nil {
			return err
		}
	}
	return nil
}

func cloneCommentRenderNodes(nodes []CommentRenderNode) []CommentRenderNode {
	if nodes == nil {
		return nil
	}
	cloned := make([]CommentRenderNode, len(nodes))
	for index := range nodes {
		cloned[index] = nodes[index]
		cloned[index].Children = cloneCommentRenderNodes(nodes[index].Children)
		if nodes[index].Items != nil {
			cloned[index].Items = make([][]CommentRenderNode, len(nodes[index].Items))
			for item := range nodes[index].Items {
				cloned[index].Items[item] = cloneCommentRenderNodes(nodes[index].Items[item])
			}
		}
	}
	return cloned
}
