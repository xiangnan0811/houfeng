package portability

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"

	"houfeng/internal/center/recordmarkdown"
)

type DocumentPDFRenderer interface {
	Render(context.Context, string) ([]byte, error)
}

type isolatedDocumentPDFRenderer struct {
	processorBinary string
}

func NewIsolatedDocumentPDFRenderer(processorBinary string) DocumentPDFRenderer {
	return isolatedDocumentPDFRenderer{processorBinary: strings.TrimSpace(processorBinary)}
}

func (renderer isolatedDocumentPDFRenderer) Render(ctx context.Context, html string) ([]byte, error) {
	if ctx == nil {
		return nil, recordmarkdown.ErrInvalidDerivedPDF
	}
	workspace, err := os.MkdirTemp("", "houfeng-derived-pdf-")
	if err != nil {
		return nil, ErrExportUnavailable
	}
	defer os.RemoveAll(workspace)
	command, err := recordmarkdown.NewIsolatedDerivedPDFCommand(renderer.processorName(), workspace)
	if err != nil {
		return nil, err
	}
	if err := command.ValidateIsolation(); err != nil {
		return nil, err
	}
	if renderer.processorBinary == "" {
		return recordmarkdown.WriteDerivedPDF(html)
	}
	process := exec.CommandContext(ctx, command.Name, command.Args...)
	process.Dir = command.Dir
	process.Env = append([]string(nil), command.Env...)
	process.Stdin = strings.NewReader(html)
	output, err := process.Output()
	if err != nil || !bytes.Contains(output, []byte(recordmarkdown.DerivedPDFFormatV1)) {
		return nil, ErrExportUnavailable
	}
	return output, nil
}

func (renderer isolatedDocumentPDFRenderer) processorName() string {
	if renderer.processorBinary != "" {
		return renderer.processorBinary
	}
	return "houfeng-content-processor"
}
