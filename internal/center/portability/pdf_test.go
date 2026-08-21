package portability

import (
	"context"
	"strings"
	"testing"

	"houfeng/internal/center/recordmarkdown"
)

func TestIsolatedDocumentPDFRendererDisablesNetworkAndKeepsHTML(t *testing.T) {
	t.Parallel()

	html, status, err := recordmarkdown.SafeDocumentHTML("# Disk notes\n\nRecovered.\n", nil)
	if err != nil || status != recordmarkdown.DocumentRenderReady {
		t.Fatalf("SafeDocumentHTML() status=%q err=%v", status, err)
	}
	pdf, err := NewIsolatedDocumentPDFRenderer("").Render(context.Background(), html)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got, err := recordmarkdown.ExtractDerivedHTML(pdf)
	if err != nil {
		t.Fatalf("ExtractDerivedHTML() error = %v", err)
	}
	if got != html || !strings.Contains(string(pdf), recordmarkdown.DerivedPDFFormatV1) {
		t.Fatal("isolated renderer drifted from SafeDocumentHTML")
	}
	command, err := recordmarkdown.NewIsolatedDerivedPDFCommand("houfeng-content-processor", t.TempDir())
	if err != nil {
		t.Fatalf("NewIsolatedDerivedPDFCommand() error = %v", err)
	}
	if err := command.ValidateIsolation(); err != nil {
		t.Fatalf("ValidateIsolation() error = %v", err)
	}
}
