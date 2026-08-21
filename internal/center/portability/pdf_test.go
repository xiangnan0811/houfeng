package portability

import (
	"context"
	"os"
	"path/filepath"
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

func TestIsolatedDocumentPDFRendererInvokesConfiguredProcessor(t *testing.T) {
	t.Parallel()

	html, status, err := recordmarkdown.SafeDocumentHTML("# Disk notes\n\nRecovered.\n", nil)
	if err != nil || status != recordmarkdown.DocumentRenderReady {
		t.Fatalf("SafeDocumentHTML() status=%q err=%v", status, err)
	}
	want, err := recordmarkdown.WriteDerivedPDF(html)
	if err != nil {
		t.Fatalf("WriteDerivedPDF() error = %v", err)
	}
	dir := t.TempDir()
	output := filepath.Join(dir, "derived.pdf")
	if err := os.WriteFile(output, want, 0o600); err != nil {
		t.Fatalf("WriteFile(pdf) error = %v", err)
	}
	script := filepath.Join(dir, "houfeng-content-processor")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat \""+output+"\"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(processor) error = %v", err)
	}
	got, err := NewIsolatedDocumentPDFRenderer(script).Render(context.Background(), html)
	if err != nil {
		t.Fatalf("Render(processor) error = %v", err)
	}
	extracted, err := recordmarkdown.ExtractDerivedHTML(got)
	if err != nil || extracted != html {
		t.Fatalf("processor PDF drifted: html=%q err=%v", extracted, err)
	}
}
