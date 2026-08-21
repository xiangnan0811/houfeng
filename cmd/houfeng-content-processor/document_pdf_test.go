package main

import (
	"strings"
	"testing"

	"houfeng/internal/center/recordmarkdown"
)

func TestRunRenderDocumentPDFUsesSameRenderModel(t *testing.T) {
	t.Parallel()

	html, status, err := recordmarkdown.SafeDocumentHTML("# Disk notes\n\nRecovered.\n", nil)
	if err != nil || status != recordmarkdown.DocumentRenderReady {
		t.Fatalf("SafeDocumentHTML() status=%q err=%v", status, err)
	}
	pdf, err := recordmarkdown.WriteDerivedPDF(html)
	if err != nil {
		t.Fatalf("WriteDerivedPDF() error = %v", err)
	}
	got, err := recordmarkdown.ExtractDerivedHTML(pdf)
	if err != nil {
		t.Fatalf("ExtractDerivedHTML() error = %v", err)
	}
	if got != html || !strings.Contains(got, "Disk notes") {
		t.Fatalf("processor PDF drifted from RenderModel: %s", got)
	}
}
