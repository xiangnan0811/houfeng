package recordmarkdown

import (
	"strings"
	"testing"
)

func TestWriteDerivedPDFKeepsSafeDocumentHTMLAndIsNotAuthority(t *testing.T) {
	t.Parallel()

	source := "# Disk notes\n\nRecovered.\n\n## 不可用材料\n\n- evidence `evs_denied`：unauthorized\n"
	html, status, err := SafeDocumentHTML(source, nil)
	if err != nil {
		t.Fatalf("SafeDocumentHTML() error = %v", err)
	}
	if status != DocumentRenderReady {
		t.Fatalf("status = %q, want ready", status)
	}
	pdf, err := WriteDerivedPDF(html)
	if err != nil {
		t.Fatalf("WriteDerivedPDF() error = %v", err)
	}
	if !strings.HasPrefix(string(pdf), "%PDF-1.4\n%"+DerivedPDFFormatV1) {
		t.Fatalf("pdf header = %q", pdf[:min(len(pdf), 80)])
	}
	if !strings.Contains(string(pdf), derivedPDFNotice) {
		t.Fatal("derived PDF omitted the non-authority notice")
	}
	got, err := ExtractDerivedHTML(pdf)
	if err != nil {
		t.Fatalf("ExtractDerivedHTML() error = %v", err)
	}
	if got != html {
		t.Fatalf("extracted HTML drifted from SafeDocumentHTML")
	}
	if !strings.Contains(got, "Disk notes") || !strings.Contains(got, "不可用材料") {
		t.Fatalf("semantic tokens missing from derived HTML: %s", got)
	}
}

func TestIsolatedDerivedPDFCommandDisablesNetworkAndProxy(t *testing.T) {
	t.Parallel()

	command, err := NewIsolatedDerivedPDFCommand("/usr/bin/houfeng-content-processor", "/tmp/houfeng-derived-pdf")
	if err != nil {
		t.Fatalf("NewIsolatedDerivedPDFCommand() error = %v", err)
	}
	if err := command.ValidateIsolation(); err != nil {
		t.Fatalf("ValidateIsolation() error = %v", err)
	}
	if command.Args[0] != derivedPDFProcessorCommand || !command.NetworkDisabled || command.AllowProxyEnv {
		t.Fatalf("command = %#v", command)
	}
}

func TestWriteDerivedPDFRejectsEmbeddedMarkerCollision(t *testing.T) {
	t.Parallel()

	if _, err := WriteDerivedPDF(derivedPDFHTMLBegin + "<p>x</p>"); err != ErrInvalidDerivedPDF {
		t.Fatalf("WriteDerivedPDF(marker) error = %v, want ErrInvalidDerivedPDF", err)
	}
}
