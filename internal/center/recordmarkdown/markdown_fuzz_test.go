package recordmarkdown

import (
	"errors"
	"testing"
)

func FuzzParseDocumentMarkdownV1(f *testing.F) {
	f.Add("# title\n\nhello", "evidence", "ev_1")
	f.Add("<script>x</script>", "attachment", "att_1")
	f.Add("[run](javascript:alert(1))", "evidence", "ev_7K2P")
	f.Add("<!-- houfeng-ref:v1 evidence ev_7K2P -->\n[系统证据：x](houfeng-evidence:ev_7K2P)", "evidence", "ev_7K2P")
	f.Add("[ok](https://example.com/path)", "", "")
	f.Fuzz(func(t *testing.T, source, kind, id string) {
		var authorized []DocumentReference
		if kind != "" && id != "" {
			authorized = []DocumentReference{{Kind: kind, ID: id}}
		}
		model, err := ParseDocumentMarkdownV1(source, authorized)
		if err != nil {
			if !errors.Is(err, ErrInvalidDocumentMarkdown) {
				t.Fatalf("unexpected error %v", err)
			}
			return
		}
		if err := model.Validate(); err != nil {
			t.Fatalf("valid parse failed Validate: %v", err)
		}
		if _, err := RenderSafeHTML(model); err != nil {
			t.Fatalf("RenderSafeHTML() error = %v", err)
		}
	})
}
