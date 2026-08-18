package recordmarkdown

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"

	"houfeng/internal/center/recordcollaboration"
)

func (node DocumentRenderNode) MarshalJSON() ([]byte, error) {
	if err := validateDocumentRenderNode(node, 1, new(int), false); err != nil {
		return nil, err
	}
	switch node.Type {
	case DocumentRenderNodeParagraph, DocumentRenderNodeEmphasis, DocumentRenderNodeStrong, DocumentRenderNodeStrikethrough, DocumentRenderNodeBlockquote:
		return json.Marshal(struct {
			Type     string               `json:"type"`
			Children []DocumentRenderNode `json:"children"`
		}{Type: node.Type, Children: node.Children})
	case DocumentRenderNodeText, DocumentRenderNodeInlineCode, DocumentRenderNodeFencedCode, DocumentRenderNodeFootnoteRef:
		return json.Marshal(struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: node.Type, Text: node.Text})
	case DocumentRenderNodeLineBreak, DocumentRenderNodeThematicBreak:
		return json.Marshal(struct {
			Type string `json:"type"`
		}{Type: node.Type})
	case DocumentRenderNodeOrderedList:
		return json.Marshal(struct {
			Type  string                 `json:"type"`
			Start uint64                 `json:"start"`
			Items [][]DocumentRenderNode `json:"items"`
		}{Type: node.Type, Start: node.Start, Items: node.Items})
	case DocumentRenderNodeUnorderedList:
		return json.Marshal(struct {
			Type  string                 `json:"type"`
			Items [][]DocumentRenderNode `json:"items"`
		}{Type: node.Type, Items: node.Items})
	case DocumentRenderNodeLink:
		return json.Marshal(struct {
			Type     string               `json:"type"`
			Href     string               `json:"href"`
			Children []DocumentRenderNode `json:"children"`
		}{Type: node.Type, Href: node.Href, Children: node.Children})
	case DocumentRenderNodeHeading:
		return json.Marshal(struct {
			Type     string               `json:"type"`
			Level    uint64               `json:"level"`
			Children []DocumentRenderNode `json:"children"`
		}{Type: node.Type, Level: node.Level, Children: node.Children})
	case DocumentRenderNodeTable:
		return json.Marshal(struct {
			Type   string                   `json:"type"`
			Header [][]DocumentRenderNode   `json:"header"`
			Rows   [][][]DocumentRenderNode `json:"rows"`
		}{Type: node.Type, Header: node.Header, Rows: node.Rows})
	case DocumentRenderNodeTaskList:
		return json.Marshal(struct {
			Type  string             `json:"type"`
			Items []DocumentTaskItem `json:"items"`
		}{Type: node.Type, Items: node.TaskItems})
	case DocumentRenderNodeFootnoteDef:
		return json.Marshal(struct {
			Type     string               `json:"type"`
			Text     string               `json:"text"`
			Children []DocumentRenderNode `json:"children"`
		}{Type: node.Type, Text: node.Text, Children: node.Children})
	case DocumentRenderNodeReference:
		return json.Marshal(struct {
			Type     string               `json:"type"`
			Kind     string               `json:"kind"`
			ID       string               `json:"id"`
			Children []DocumentRenderNode `json:"children"`
		}{Type: node.Type, Kind: node.Kind, ID: node.ID, Children: node.Children})
	default:
		return nil, ErrInvalidDocumentMarkdown
	}
}

func (node *DocumentRenderNode) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&fields); err != nil || fields == nil || ensureDocumentJSONEOF(decoder) != nil {
		return ErrInvalidDocumentMarkdown
	}
	var nodeType string
	if err := json.Unmarshal(fields["type"], &nodeType); err != nil || !documentRenderNodeHasExactKeys(fields, nodeType) {
		return ErrInvalidDocumentMarkdown
	}
	switch nodeType {
	case DocumentRenderNodeTaskList:
		var decoded struct {
			Type  string             `json:"type"`
			Items []DocumentTaskItem `json:"items"`
		}
		if err := decodeExactDocumentJSON(raw, &decoded); err != nil {
			return ErrInvalidDocumentMarkdown
		}
		*node = DocumentRenderNode{Type: decoded.Type, TaskItems: decoded.Items}
		return nil
	default:
		type documentRenderNodeWire DocumentRenderNode
		var decoded documentRenderNodeWire
		if err := decodeExactDocumentJSON(raw, &decoded); err != nil {
			return ErrInvalidDocumentMarkdown
		}
		*node = DocumentRenderNode(decoded)
		return nil
	}
}

func decodeExactDocumentJSON(raw []byte, dest any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil || ensureDocumentJSONEOF(decoder) != nil {
		return ErrInvalidDocumentMarkdown
	}
	return nil
}

func ensureDocumentJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ErrInvalidDocumentMarkdown
	}
	return nil
}

func documentRenderNodeHasExactKeys(fields map[string]json.RawMessage, nodeType string) bool {
	var expected []string
	switch nodeType {
	case DocumentRenderNodeParagraph, DocumentRenderNodeEmphasis, DocumentRenderNodeStrong, DocumentRenderNodeStrikethrough, DocumentRenderNodeBlockquote:
		expected = []string{"children", "type"}
	case DocumentRenderNodeText, DocumentRenderNodeInlineCode, DocumentRenderNodeFencedCode, DocumentRenderNodeFootnoteRef:
		expected = []string{"text", "type"}
	case DocumentRenderNodeLineBreak, DocumentRenderNodeThematicBreak:
		expected = []string{"type"}
	case DocumentRenderNodeOrderedList:
		expected = []string{"items", "start", "type"}
	case DocumentRenderNodeUnorderedList, DocumentRenderNodeTaskList:
		expected = []string{"items", "type"}
	case DocumentRenderNodeLink:
		expected = []string{"children", "href", "type"}
	case DocumentRenderNodeHeading:
		expected = []string{"children", "level", "type"}
	case DocumentRenderNodeTable:
		expected = []string{"header", "rows", "type"}
	case DocumentRenderNodeFootnoteDef:
		expected = []string{"children", "text", "type"}
	case DocumentRenderNodeReference:
		expected = []string{"children", "id", "kind", "type"}
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
	return true
}

func DecodeDocumentRenderModelV1(raw json.RawMessage) (DocumentRenderModel, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var model DocumentRenderModel
	if err := decoder.Decode(&model); err != nil || ensureDocumentJSONEOF(decoder) != nil {
		return DocumentRenderModel{}, ErrInvalidDocumentMarkdown
	}
	if model.Validate() != nil {
		return DocumentRenderModel{}, ErrInvalidDocumentMarkdown
	}
	return model.Clone(), nil
}

func (model DocumentRenderModel) Validate() error {
	if model.Version != DocumentRenderContractVersionV1 {
		return ErrInvalidDocumentMarkdown
	}
	if model.Nodes == nil {
		return ErrInvalidDocumentMarkdown
	}
	count := 0
	for _, node := range model.Nodes {
		if !documentRootNodeType(node.Type) {
			return ErrInvalidDocumentMarkdown
		}
		if err := validateDocumentRenderNode(node, 1, &count, false); err != nil {
			return err
		}
	}
	return nil
}

func documentRootNodeType(nodeType string) bool {
	switch nodeType {
	case DocumentRenderNodeParagraph, DocumentRenderNodeFencedCode, DocumentRenderNodeOrderedList,
		DocumentRenderNodeUnorderedList, DocumentRenderNodeHeading, DocumentRenderNodeBlockquote,
		DocumentRenderNodeThematicBreak, DocumentRenderNodeTable, DocumentRenderNodeTaskList,
		DocumentRenderNodeFootnoteDef, DocumentRenderNodeReference:
		return true
	default:
		return false
	}
}

func validateDocumentRenderNode(node DocumentRenderNode, depth int, count *int, inline bool) error {
	*count++
	if depth > MaxDocumentRenderDepth || *count > MaxDocumentRenderNodes {
		return ErrInvalidDocumentMarkdown
	}
	if inline && documentRootNodeType(node.Type) && node.Type != DocumentRenderNodeParagraph {
		return ErrInvalidDocumentMarkdown
	}
	switch node.Type {
	case DocumentRenderNodeParagraph, DocumentRenderNodeEmphasis, DocumentRenderNodeStrong, DocumentRenderNodeStrikethrough:
		if node.Text != "" || node.Href != "" || node.Start != 0 || node.Level != 0 || node.Kind != "" || node.ID != "" ||
			node.Items != nil || node.Header != nil || node.Rows != nil || node.TaskItems != nil || len(node.Children) == 0 {
			return ErrInvalidDocumentMarkdown
		}
		return validateDocumentInlineChildren(node.Children, depth+1, count)
	case DocumentRenderNodeBlockquote, DocumentRenderNodeFootnoteDef:
		if node.Href != "" || node.Start != 0 || node.Level != 0 || node.Kind != "" || node.ID != "" ||
			node.Items != nil || node.Header != nil || node.Rows != nil || node.TaskItems != nil || len(node.Children) == 0 {
			return ErrInvalidDocumentMarkdown
		}
		if node.Type == DocumentRenderNodeFootnoteDef && node.Text == "" {
			return ErrInvalidDocumentMarkdown
		}
		if node.Type == DocumentRenderNodeBlockquote && node.Text != "" {
			return ErrInvalidDocumentMarkdown
		}
		for _, child := range node.Children {
			if !documentRootNodeType(child.Type) {
				return ErrInvalidDocumentMarkdown
			}
			if err := validateDocumentRenderNode(child, depth+1, count, false); err != nil {
				return err
			}
		}
		return nil
	case DocumentRenderNodeText, DocumentRenderNodeInlineCode, DocumentRenderNodeFencedCode, DocumentRenderNodeFootnoteRef:
		if node.Href != "" || node.Start != 0 || node.Level != 0 || node.Kind != "" || node.ID != "" ||
			node.Children != nil || node.Items != nil || node.Header != nil || node.Rows != nil || node.TaskItems != nil {
			return ErrInvalidDocumentMarkdown
		}
		if node.Type != DocumentRenderNodeFencedCode && node.Text == "" {
			return ErrInvalidDocumentMarkdown
		}
		return nil
	case DocumentRenderNodeLineBreak, DocumentRenderNodeThematicBreak:
		if node.Text != "" || node.Href != "" || node.Start != 0 || node.Level != 0 || node.Kind != "" || node.ID != "" ||
			node.Children != nil || node.Items != nil || node.Header != nil || node.Rows != nil || node.TaskItems != nil {
			return ErrInvalidDocumentMarkdown
		}
		return nil
	case DocumentRenderNodeLink:
		if recordcollaboration.ValidateCanonicalHTTPLink(node.Href) != nil || node.Text != "" || node.Start != 0 ||
			node.Level != 0 || node.Kind != "" || node.ID != "" || node.Items != nil || node.Header != nil ||
			node.Rows != nil || node.TaskItems != nil || len(node.Children) == 0 {
			return ErrInvalidDocumentMarkdown
		}
		return validateDocumentInlineChildren(node.Children, depth+1, count)
	case DocumentRenderNodeHeading:
		if node.Level < 1 || node.Level > 6 || node.Text != "" || node.Href != "" || node.Start != 0 ||
			node.Kind != "" || node.ID != "" || node.Items != nil || node.Header != nil || node.Rows != nil ||
			node.TaskItems != nil || len(node.Children) == 0 {
			return ErrInvalidDocumentMarkdown
		}
		return validateDocumentInlineChildren(node.Children, depth+1, count)
	case DocumentRenderNodeOrderedList, DocumentRenderNodeUnorderedList:
		if node.Text != "" || node.Href != "" || node.Level != 0 || node.Kind != "" || node.ID != "" ||
			node.Children != nil || node.Header != nil || node.Rows != nil || node.TaskItems != nil || len(node.Items) == 0 {
			return ErrInvalidDocumentMarkdown
		}
		if node.Type == DocumentRenderNodeUnorderedList && node.Start != 0 {
			return ErrInvalidDocumentMarkdown
		}
		if node.Type == DocumentRenderNodeOrderedList && node.Start == 0 {
			return ErrInvalidDocumentMarkdown
		}
		for _, item := range node.Items {
			if len(item) == 0 || validateDocumentInlineChildren(item, depth+1, count) != nil {
				return ErrInvalidDocumentMarkdown
			}
		}
		return nil
	case DocumentRenderNodeTaskList:
		if node.Text != "" || node.Href != "" || node.Start != 0 || node.Level != 0 || node.Kind != "" || node.ID != "" ||
			node.Children != nil || node.Items != nil || node.Header != nil || node.Rows != nil || len(node.TaskItems) == 0 {
			return ErrInvalidDocumentMarkdown
		}
		for _, item := range node.TaskItems {
			if len(item.Children) == 0 || validateDocumentInlineChildren(item.Children, depth+1, count) != nil {
				return ErrInvalidDocumentMarkdown
			}
		}
		return nil
	case DocumentRenderNodeTable:
		if node.Text != "" || node.Href != "" || node.Start != 0 || node.Level != 0 || node.Kind != "" || node.ID != "" ||
			node.Children != nil || node.Items != nil || node.TaskItems != nil || len(node.Header) == 0 {
			return ErrInvalidDocumentMarkdown
		}
		width := len(node.Header)
		if width == 0 {
			return ErrInvalidDocumentMarkdown
		}
		for _, cell := range node.Header {
			if len(cell) == 0 || validateDocumentInlineChildren(cell, depth+1, count) != nil {
				return ErrInvalidDocumentMarkdown
			}
		}
		for _, row := range node.Rows {
			if len(row) != width {
				return ErrInvalidDocumentMarkdown
			}
			for _, cell := range row {
				if len(cell) == 0 || validateDocumentInlineChildren(cell, depth+1, count) != nil {
					return ErrInvalidDocumentMarkdown
				}
			}
		}
		return nil
	case DocumentRenderNodeReference:
		if (node.Kind != "evidence" && node.Kind != "attachment") || node.ID == "" || node.Text != "" ||
			node.Href != "" || node.Start != 0 || node.Level != 0 || node.Items != nil || node.Header != nil ||
			node.Rows != nil || node.TaskItems != nil || len(node.Children) == 0 {
			return ErrInvalidDocumentMarkdown
		}
		return validateDocumentInlineChildren(node.Children, depth+1, count)
	default:
		return ErrInvalidDocumentMarkdown
	}
}

func validateDocumentInlineChildren(nodes []DocumentRenderNode, depth int, count *int) error {
	for _, node := range nodes {
		switch node.Type {
		case DocumentRenderNodeText, DocumentRenderNodeLineBreak, DocumentRenderNodeEmphasis, DocumentRenderNodeStrong,
			DocumentRenderNodeStrikethrough, DocumentRenderNodeInlineCode, DocumentRenderNodeLink, DocumentRenderNodeFootnoteRef:
		default:
			return ErrInvalidDocumentMarkdown
		}
		if err := validateDocumentRenderNode(node, depth, count, true); err != nil {
			return err
		}
	}
	return nil
}

func (model DocumentRenderModel) Clone() DocumentRenderModel {
	return DocumentRenderModel{Version: model.Version, Nodes: cloneDocumentNodes(model.Nodes)}
}

func (model DocumentRenderModel) Equal(other DocumentRenderModel) bool {
	return reflect.DeepEqual(model.Clone(), other.Clone())
}

func cloneDocumentNodes(nodes []DocumentRenderNode) []DocumentRenderNode {
	if nodes == nil {
		return nil
	}
	cloned := make([]DocumentRenderNode, len(nodes))
	for index, node := range nodes {
		cloned[index] = cloneDocumentNode(node)
	}
	return cloned
}

func cloneDocumentNode(node DocumentRenderNode) DocumentRenderNode {
	cloned := node
	cloned.Children = cloneDocumentNodes(node.Children)
	if node.Items != nil {
		cloned.Items = make([][]DocumentRenderNode, len(node.Items))
		for index, item := range node.Items {
			cloned.Items[index] = cloneDocumentNodes(item)
		}
	}
	if node.Header != nil {
		cloned.Header = make([][]DocumentRenderNode, len(node.Header))
		for index, cell := range node.Header {
			cloned.Header[index] = cloneDocumentNodes(cell)
		}
	}
	if node.Rows != nil {
		cloned.Rows = make([][][]DocumentRenderNode, len(node.Rows))
		for rowIndex, row := range node.Rows {
			cloned.Rows[rowIndex] = make([][]DocumentRenderNode, len(row))
			for cellIndex, cell := range row {
				cloned.Rows[rowIndex][cellIndex] = cloneDocumentNodes(cell)
			}
		}
	}
	if node.TaskItems != nil {
		cloned.TaskItems = make([]DocumentTaskItem, len(node.TaskItems))
		for index, item := range node.TaskItems {
			cloned.TaskItems[index] = DocumentTaskItem{Checked: item.Checked, Children: cloneDocumentNodes(item.Children)}
		}
	}
	return cloned
}

func (model DocumentRenderModel) CommentProjection() recordcollaboration.CommentRenderModel {
	return recordcollaboration.CommentRenderModel{
		Version: recordcollaboration.CommentRenderContractVersionV1,
		Nodes:   commentNodesFromDocument(model.Nodes),
	}
}

func commentNodesFromDocument(nodes []DocumentRenderNode) []recordcollaboration.CommentRenderNode {
	converted := make([]recordcollaboration.CommentRenderNode, 0, len(nodes))
	for _, node := range nodes {
		converted = append(converted, commentNodeFromDocument(node))
	}
	return converted
}

func commentNodeFromDocument(node DocumentRenderNode) recordcollaboration.CommentRenderNode {
	converted := recordcollaboration.CommentRenderNode{
		Type:  node.Type,
		Text:  node.Text,
		Href:  node.Href,
		Start: node.Start,
	}
	if node.Children != nil {
		converted.Children = commentNodesFromDocument(node.Children)
	}
	if node.Items != nil {
		converted.Items = make([][]recordcollaboration.CommentRenderNode, len(node.Items))
		for index, item := range node.Items {
			converted.Items[index] = commentNodesFromDocument(item)
		}
	}
	return converted
}
