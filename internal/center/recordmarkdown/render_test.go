package recordmarkdown

import (
	"strings"
	"testing"
)

func TestScanDocumentSourceRejectsRawHTMLAndUnsafeLinks(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"before <script>alert(1)</script>",
		"<!-- hidden -->",
		"![secret](https://example.com/a.png)",
		"[run](javascript:alert(1))",
		"[run](data:text/html,boom)",
	} {
		if err := ScanDocumentSource(source); err != ErrInvalidDocumentMarkdown {
			t.Fatalf("ScanDocumentSource(%q) error = %v", source, err)
		}
	}
}

func TestScanDocumentSourceAllowsAuthorizedRefsAndHeadings(t *testing.T) {
	t.Parallel()

	source := "# Outage\n\n<!-- houfeng-ref:v1 evidence ev_7K2P -->\n[系统证据：第三晚 TCP 观测](houfeng-evidence:ev_7K2P)\n"
	if err := ScanDocumentSource(source); err != nil {
		t.Fatalf("ScanDocumentSource() error = %v", err)
	}
}

func TestRenderSafeHTMLEscapesTextAndDropsScripts(t *testing.T) {
	t.Parallel()

	model := DocumentRenderModel{
		Version: DocumentRenderContractVersionV1,
		Nodes: []DocumentRenderNode{
			{
				Type: DocumentRenderNodeParagraph,
				Children: []DocumentRenderNode{
					{Type: DocumentRenderNodeText, Text: "<script>alert(1)</script>"},
				},
			},
			{
				Type:  DocumentRenderNodeHeading,
				Level: 1,
				Children: []DocumentRenderNode{
					{Type: DocumentRenderNodeText, Text: "Outage"},
				},
			},
		},
	}
	rendered, err := RenderSafeHTML(model)
	if err != nil {
		t.Fatalf("RenderSafeHTML() error = %v", err)
	}
	if strings.Contains(rendered, "<script>") || strings.Contains(rendered, "alert(1)</script>") {
		t.Fatalf("raw script leaked: %s", rendered)
	}
	if !strings.Contains(rendered, "&lt;script&gt;") {
		t.Fatalf("expected escaped script text, got %s", rendered)
	}
	if !strings.Contains(rendered, "<h1>Outage</h1>") {
		t.Fatalf("expected heading, got %s", rendered)
	}
}

// Export and print need one call that always returns safe HTML, because the write
// path stores bodies the dialect cannot model and losing them on export would be
// worse than rendering them as text.
func TestSafeDocumentHTMLReportsReadyAndUnsupported(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		source      string
		wantStatus  DocumentRenderStatus
		wantContain string
	}{
		{
			name:        "modelable body",
			source:      "# Outage\n\nRecovered.",
			wantStatus:  DocumentRenderReady,
			wantContain: "<h1>Outage</h1>",
		},
		{
			name:        "nested list body",
			source:      "- outer\n  - inner",
			wantStatus:  DocumentRenderUnsupported,
			wantContain: "<pre><code>- outer\n  - inner</code></pre>",
		},
		{
			name:        "unterminated fence body",
			source:      "# Outage\n\n```sh\nls",
			wantStatus:  DocumentRenderUnsupported,
			wantContain: "ls</code></pre>",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			rendered, status, err := SafeDocumentHTML(test.source, nil)
			if err != nil {
				t.Fatalf("SafeDocumentHTML() error = %v", err)
			}
			if status != test.wantStatus {
				t.Fatalf("status = %q, want %q", status, test.wantStatus)
			}
			if !strings.Contains(rendered, test.wantContain) {
				t.Fatalf("rendered = %q, want it to contain %q", rendered, test.wantContain)
			}
		})
	}
}

// The fallback must not become a looser Markdown path: hostile source that the model
// path rejects has to come back inert, not interpreted.
func TestSafeDocumentHTMLKeepsUnsupportedSourceInert(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"- a\n  - <script>alert(1)</script>",
		"```\n<img src=x onerror=alert(1)>",
		"- a\n  [run](javascript:alert(1))",
	} {
		rendered, status, err := SafeDocumentHTML(source, nil)
		if err != nil {
			t.Fatalf("SafeDocumentHTML(%q) error = %v", source, err)
		}
		if status != DocumentRenderUnsupported {
			t.Fatalf("status = %q, want %q for %q", status, DocumentRenderUnsupported, source)
		}
		body := strings.TrimSuffix(strings.TrimPrefix(rendered, "<pre><code>"), "</code></pre>")
		if body == rendered {
			t.Fatalf("rendered = %q, want the preformatted wrapper", rendered)
		}
		// Everything between the wrapper must be escaped text, so a hostile body
		// cannot contribute a tag or an attribute to the exported document.
		if strings.ContainsAny(body, "<>") {
			t.Fatalf("rendered body = %q still carries markup characters", body)
		}
	}
}
