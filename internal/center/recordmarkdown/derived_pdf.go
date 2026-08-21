package recordmarkdown

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	DerivedPDFFormatV1         = "houfeng-derived-presentation/v1"
	derivedPDFHTMLBegin        = "%HOUFENG_DERIVED_HTML_BEGIN\n"
	derivedPDFHTMLEnd          = "\n%HOUFENG_DERIVED_HTML_END\n"
	maxDerivedPDFHTMLBytes     = 1 << 20
	derivedPDFProcessorCommand = "render-document-pdf"
	derivedPDFNotice           = "Derived presentation of the Markdown RenderModel. Not machine authority."
)

var ErrInvalidDerivedPDF = errors.New("invalid derived record pdf")

type IsolatedPDFCommand struct {
	Name              string
	Args              []string
	Env               []string
	Dir               string
	NetworkDisabled   bool
	AllowProxyEnv     bool
	AllowNetworkHosts []string
}

func DerivedPDFIsolationSpec() IsolatedPDFCommand {
	return IsolatedPDFCommand{
		Name:            derivedPDFProcessorCommand,
		Args:            []string{derivedPDFProcessorCommand},
		NetworkDisabled: true,
		AllowProxyEnv:   false,
	}
}

func NewIsolatedDerivedPDFCommand(processorBinary, workspace string) (IsolatedPDFCommand, error) {
	if processorBinary == "" || workspace == "" {
		return IsolatedPDFCommand{}, ErrInvalidDerivedPDF
	}
	spec := DerivedPDFIsolationSpec()
	spec.Name = processorBinary
	spec.Dir = workspace
	spec.Env = []string{"HOME=" + workspace, "TMPDIR=" + workspace}
	return spec, nil
}

func (command IsolatedPDFCommand) ValidateIsolation() error {
	if !command.NetworkDisabled || command.AllowProxyEnv || len(command.AllowNetworkHosts) > 0 {
		return ErrInvalidDerivedPDF
	}
	for _, value := range command.Env {
		key, _, _ := strings.Cut(value, "=")
		switch strings.ToUpper(key) {
		case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY":
			return ErrInvalidDerivedPDF
		}
	}
	return nil
}

func WriteDerivedPDF(html string) ([]byte, error) {
	if html == "" || !utf8.ValidString(html) || len(html) > maxDerivedPDFHTMLBytes {
		return nil, ErrInvalidDerivedPDF
	}
	if strings.Contains(html, derivedPDFHTMLBegin) || strings.Contains(html, derivedPDFHTMLEnd) {
		return nil, ErrInvalidDerivedPDF
	}
	embedded := derivedPDFHTMLBegin + html + derivedPDFHTMLEnd
	content := "BT /F1 12 Tf 72 720 Td (" + escapePDFString(derivedPDFNotice) + ") Tj ET\n"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles 6 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		"<< /Length " + strconv.Itoa(len(content)) + " >>\nstream\n" + content + "endstream",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Names [(document.html) 7 0 R] >>",
		"<< /Type /Filespec /F (document.html) /EF << /F 8 0 R >> >>",
		"<< /Type /EmbeddedFile /Subtype /text#2Fhtml /Length " + strconv.Itoa(len(embedded)) + " >>\nstream\n" + embedded + "endstream",
	}

	var document bytes.Buffer
	document.WriteString("%PDF-1.4\n%")
	document.WriteString(DerivedPDFFormatV1)
	document.WriteByte('\n')
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = document.Len()
		fmt.Fprintf(&document, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	startxref := document.Len()
	fmt.Fprintf(&document, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for index := 1; index <= len(objects); index++ {
		fmt.Fprintf(&document, "%010d 00000 n \n", offsets[index])
	}
	fmt.Fprintf(&document, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, startxref)
	return document.Bytes(), nil
}

func ExtractDerivedHTML(pdf []byte) (string, error) {
	begin := bytes.Index(pdf, []byte(derivedPDFHTMLBegin))
	end := bytes.Index(pdf, []byte(derivedPDFHTMLEnd))
	if begin < 0 || end <= begin {
		return "", ErrInvalidDerivedPDF
	}
	html := pdf[begin+len(derivedPDFHTMLBegin) : end]
	if len(html) == 0 || !utf8.Valid(html) {
		return "", ErrInvalidDerivedPDF
	}
	return string(html), nil
}

func escapePDFString(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "(", "\\(", ")", "\\)")
	return replacer.Replace(value)
}
