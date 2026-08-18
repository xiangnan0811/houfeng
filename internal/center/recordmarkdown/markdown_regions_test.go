package recordmarkdown

import (
	"encoding/json"
	"strings"
	"testing"

	"houfeng/internal/center/recordcollaboration"
)

// Fenced code is document-owned. Before that, block scanning split fence bodies on
// any line that merely looked like document structure, which rejected ordinary
// operator content such as a shell snippet with comments.
func TestDocumentMarkdownV1KeepsFencedCodeIntact(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "shell comment", body: "# step one\nls -al\n"},
		{name: "table lines", body: "| host | state |\n| --- | --- |\n"},
		{name: "quote and rule", body: "> quoted\n---\n"},
		{name: "raw html", body: "<div>raw</div>\n"},
		{name: "image syntax", body: "![alt](x)\n"},
		{name: "footnote syntax", body: "see [^1] here\n"},
		{name: "task syntax", body: "- [ ] not a task\n"},
		{name: "nested list syntax", body: "- outer\n  - inner\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := "# Title\n\n```sh\n" + test.body + "```"
			model, err := ParseDocumentMarkdownV1(source, nil)
			if err != nil {
				t.Fatalf("ParseDocumentMarkdownV1() error = %v", err)
			}
			if len(model.Nodes) != 2 {
				encoded, _ := json.Marshal(model.Nodes)
				t.Fatalf("nodes = %s, want heading + fenced_code", encoded)
			}
			if model.Nodes[1].Type != DocumentRenderNodeFencedCode {
				t.Fatalf("second node type = %q, want %q", model.Nodes[1].Type, DocumentRenderNodeFencedCode)
			}
			if model.Nodes[1].Text != test.body {
				t.Fatalf("fenced text = %q, want %q", model.Nodes[1].Text, test.body)
			}
		})
	}
}

// Document-owned fences must stay byte-identical to the shared dialect so a fence
// means the same thing in a comment and in a record body.
func TestDocumentMarkdownV1FenceMatchesSharedDialect(t *testing.T) {
	tests := []string{
		"```go\nfmt.Println(\"safe\")\n```",
		"```\n```",
		"~~~\nplain\n~~~",
		"````\n```\ninner\n```\n````",
	}
	for _, source := range tests {
		comment, err := recordcollaboration.ParseCommentMarkdownV1(source)
		if err != nil {
			t.Fatalf("ParseCommentMarkdownV1(%q) error = %v", source, err)
		}
		document, err := ParseDocumentMarkdownV1("# Title\n\n"+source, nil)
		if err != nil {
			t.Fatalf("ParseDocumentMarkdownV1(%q) error = %v", source, err)
		}
		if len(document.Nodes) != 2 {
			t.Fatalf("nodes = %d, want heading + fence for %q", len(document.Nodes), source)
		}
		if len(comment.Nodes) != 1 || document.Nodes[1].Text != comment.Nodes[0].Text {
			t.Fatalf("fence text drifted for %q: document %q comment %q", source, document.Nodes[1].Text, comment.Nodes[0].Text)
		}
	}
}

func TestDocumentMarkdownV1RejectsUnterminatedFence(t *testing.T) {
	if _, err := ParseDocumentMarkdownV1("# Title\n\n```sh\nls", nil); err == nil {
		t.Fatal("ParseDocumentMarkdownV1() error = nil, want rejection")
	}
}

// Shared regions no longer inherit the comment source ceiling, so a long record
// body keeps its render model instead of silently losing it.
func TestDocumentMarkdownV1AcceptsRegionsBeyondCommentLimit(t *testing.T) {
	proseLine := "Routine check completed without deviation.\n\n"
	prose := strings.Repeat(proseLine, (recordcollaboration.MaxCommentMarkdownSourceBytes/len(proseLine))+8)
	logLine := "2026-08-18T10:00:00Z probe ok latency=12ms\n"
	logBody := strings.Repeat(logLine, (recordcollaboration.MaxCommentMarkdownSourceBytes/len(logLine))+8)

	tests := []struct {
		name      string
		source    string
		wantTypes []string
	}{
		{
			name:   "prose region",
			source: "# Title\n\n" + prose,
		},
		{
			name:      "fenced log",
			source:    "# Title\n\n```\n" + logBody + "```",
			wantTypes: []string{DocumentRenderNodeHeading, DocumentRenderNodeFencedCode},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if len(test.source) <= recordcollaboration.MaxCommentMarkdownSourceBytes {
				t.Fatalf("source is %d bytes, want more than the comment ceiling", len(test.source))
			}
			model, err := ParseDocumentMarkdownV1(test.source, nil)
			if err != nil {
				t.Fatalf("ParseDocumentMarkdownV1() error = %v", err)
			}
			if test.wantTypes != nil {
				if len(model.Nodes) != len(test.wantTypes) {
					t.Fatalf("nodes = %d, want %d", len(model.Nodes), len(test.wantTypes))
				}
				for index, want := range test.wantTypes {
					if model.Nodes[index].Type != want {
						t.Fatalf("node %d type = %q, want %q", index, model.Nodes[index].Type, want)
					}
				}
				return
			}
			if len(model.Nodes) < 100 {
				t.Fatalf("nodes = %d, want the whole region preserved", len(model.Nodes))
			}
		})
	}
}

// A cell that cites a footnote used to demote the whole table to a paragraph.
func TestDocumentMarkdownV1TableKeepsFootnoteReferences(t *testing.T) {
	model, err := ParseDocumentMarkdownV1("| host | note |\n| --- | --- |\n| a | see [^1] |\n\n[^1]: detail", nil)
	if err != nil {
		t.Fatalf("ParseDocumentMarkdownV1() error = %v", err)
	}
	if len(model.Nodes) != 2 || model.Nodes[0].Type != DocumentRenderNodeTable {
		encoded, _ := json.Marshal(model.Nodes)
		t.Fatalf("nodes = %s, want table + footnote_def", encoded)
	}
	cell := model.Nodes[0].Rows[0][1]
	if len(cell) != 2 || cell[1].Type != DocumentRenderNodeFootnoteRef || cell[1].Text != "1" {
		encoded, _ := json.Marshal(cell)
		t.Fatalf("cell = %s, want text + footnote_ref 1", encoded)
	}
}

func TestDocumentMarkdownV1TableAcceptsEmptyCell(t *testing.T) {
	model, err := ParseDocumentMarkdownV1("| host | note |\n| --- | --- |\n| a |  |", nil)
	if err != nil {
		t.Fatalf("ParseDocumentMarkdownV1() error = %v", err)
	}
	cell := model.Nodes[0].Rows[0][1]
	if len(cell) != 1 || cell[0].Type != DocumentRenderNodeText || strings.TrimSpace(cell[0].Text) != "" {
		encoded, _ := json.Marshal(cell)
		t.Fatalf("cell = %s, want a single blank text node", encoded)
	}
}

func TestDocumentMarkdownV1KeepsSpacingAroundFootnoteReferences(t *testing.T) {
	model, err := ParseDocumentMarkdownV1("See [^1] and [^2] now.\n\n[^1]: a\n[^2]: b", nil)
	if err != nil {
		t.Fatalf("ParseDocumentMarkdownV1() error = %v", err)
	}
	var text strings.Builder
	for _, child := range model.Nodes[0].Children {
		switch child.Type {
		case DocumentRenderNodeText:
			text.WriteString(child.Text)
		case DocumentRenderNodeFootnoteRef:
			text.WriteString("[^" + child.Text + "]")
		}
	}
	if text.String() != "See [^1] and [^2] now." {
		t.Fatalf("reassembled paragraph = %q", text.String())
	}
}

// The shared dialect has no nested list shape. Emitting a flattened model would
// disagree with how the Markdown reads, so these sources are refused and the
// client renders them from source instead.
func TestDocumentMarkdownV1RejectsUnrepresentableListStructure(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "nested by two spaces", source: "- a\n  - b\n- c"},
		{name: "nested by four spaces", source: "- a\n    - b"},
		{name: "nested ordered under bullet", source: "- a\n  1. b"},
		{name: "nested task under bullet", source: "- a\n  - [ ] b"},
		{name: "indented flat list", source: "  - a\n  - b"},
		{name: "indented list after blank line", source: "- a\n\n  - b"},
		{name: "lazy continuation", source: "- a\n  continued"},
		{name: "table interrupting item", source: "- a\n| h |\n| --- |"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseDocumentMarkdownV1(test.source, nil); err == nil {
				t.Fatal("ParseDocumentMarkdownV1() error = nil, want rejection")
			}
		})
	}
}

func TestDocumentMarkdownV1AcceptsRepresentableListStructure(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "flat unordered", source: "- a\n- b"},
		{name: "flat ordered", source: "1. a\n2. b"},
		{name: "list then heading", source: "- a\n# Title"},
		{name: "list then fence", source: "- a\n```\ncode\n```"},
		{name: "list then blockquote", source: "- a\n> note"},
		{name: "list then rule", source: "- a\n***"},
		{name: "list markers inside fence", source: "```\n- a\n  - b\n```"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseDocumentMarkdownV1(test.source, nil); err != nil {
				t.Fatalf("ParseDocumentMarkdownV1() error = %v", err)
			}
		})
	}
}
