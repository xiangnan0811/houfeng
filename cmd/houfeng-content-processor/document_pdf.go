package main

import (
	"io"
	"os"

	"houfeng/internal/center/recordmarkdown"
)

const renderDocumentPDFCommand = "render-document-pdf"

func runRenderDocumentPDF() error {
	html, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20+1))
	if err != nil {
		return err
	}
	pdf, err := recordmarkdown.WriteDerivedPDF(string(html))
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(pdf)
	return err
}
