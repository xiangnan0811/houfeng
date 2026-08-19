package recordsearch

import (
	"strings"
	"testing"

	"houfeng/internal/center/recordmarkdown"
)

// The index stores what an operator would read, not what they typed. Syntax and
// link targets are noise; prose and code are what someone searches for.
func TestDeriveDocumentTextKeepsProseAndCodeAndDropsSyntax(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		markdown string
		want     []string
		absent   []string
	}{
		{
			name:     "heading and emphasis lose their markers",
			markdown: "# 磁盘故障排查\n\n**根因**是 *NVMe* 掉盘",
			want:     []string{"磁盘故障排查", "根因", "NVMe", "掉盘"},
			absent:   []string{"#", "**", "*"},
		},
		{
			name:     "fenced code is searchable because it carries error strings",
			markdown: "排查记录\n\n```\nnvme0n1: I/O error, dev nvme0n1\n```",
			want:     []string{"排查记录", "nvme0n1: I/O error"},
			absent:   []string{"```"},
		},
		{
			name:     "inline code is searchable",
			markdown: "执行 `systemctl restart houfeng-agent` 后恢复",
			want:     []string{"systemctl restart houfeng-agent", "后恢复"},
			absent:   []string{"`"},
		},
		{
			name:     "link text stays and the target does not",
			markdown: "见 [服务商面板](https://panel.example.com/vps/1?token=abc)",
			want:     []string{"服务商面板"},
			absent:   []string{"https://panel.example.com", "token=abc", "[", "]("},
		},
		{
			name:     "list and task items are prose",
			markdown: "- 检查电源\n- 检查磁盘\n\n- [x] 已联系服务商\n- [ ] 等待更换",
			want:     []string{"检查电源", "检查磁盘", "已联系服务商", "等待更换"},
			absent:   []string{"- [x]", "- [ ]"},
		},
		{
			name:     "table cells are prose",
			markdown: "| 主机 | 状态 |\n| --- | --- |\n| web-01 | 告警 |",
			want:     []string{"主机", "状态", "web-01", "告警"},
			absent:   []string{"|", "---"},
		},
		{
			name:     "blockquote and thematic break",
			markdown: "> 服务商回复：硬件已更换\n\n---\n\n后续观察",
			want:     []string{"服务商回复", "硬件已更换", "后续观察"},
			absent:   []string{">", "---"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			model, err := recordmarkdown.ParseDocumentMarkdownV1(tt.markdown, nil)
			if err != nil {
				t.Fatalf("ParseDocumentMarkdownV1() error = %v", err)
			}
			got, err := DeriveDocumentText(model)
			if err != nil {
				t.Fatalf("DeriveDocumentText() error = %v", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("DeriveDocumentText() = %q, missing %q", got, want)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(got, absent) {
					t.Errorf("DeriveDocumentText() = %q, leaks syntax %q", got, absent)
				}
			}
		})
	}
}

// The derived column feeds an ILIKE filter, so runs of separators only cost
// bytes and make the stored text depend on incidental layout.
func TestDeriveDocumentTextCollapsesSeparatorsAndTrims(t *testing.T) {
	t.Parallel()

	model, err := recordmarkdown.ParseDocumentMarkdownV1("# 标题\n\n\n段落一\n\n段落二\n", nil)
	if err != nil {
		t.Fatalf("ParseDocumentMarkdownV1() error = %v", err)
	}
	got, err := DeriveDocumentText(model)
	if err != nil {
		t.Fatalf("DeriveDocumentText() error = %v", err)
	}
	if got != "标题 段落一 段落二" {
		t.Fatalf("DeriveDocumentText() = %q, want single-spaced prose", got)
	}
}

// An empty body is normal for a short record and must not become an error or a
// stray separator.
func TestDeriveDocumentTextAcceptsEmptyBody(t *testing.T) {
	t.Parallel()

	model, err := recordmarkdown.ParseDocumentMarkdownV1("", nil)
	if err != nil {
		t.Fatalf("ParseDocumentMarkdownV1() error = %v", err)
	}
	got, err := DeriveDocumentText(model)
	if err != nil || got != "" {
		t.Fatalf("DeriveDocumentText() = %q, %v, want empty", got, err)
	}
}

// The column is bounded at 64 KiB by the migration, so the derivation has to cut
// on a rune boundary instead of letting Postgres reject the whole projection.
func TestDeriveDocumentTextTruncatesOnRuneBoundaryWithinColumnBound(t *testing.T) {
	t.Parallel()

	model, err := recordmarkdown.ParseDocumentMarkdownV1(strings.Repeat("磁盘故障 ", 12000), nil)
	if err != nil {
		t.Fatalf("ParseDocumentMarkdownV1() error = %v", err)
	}
	got, err := DeriveDocumentText(model)
	if err != nil {
		t.Fatalf("DeriveDocumentText() error = %v", err)
	}
	if len(got) > MaxDocumentTextBytes {
		t.Fatalf("DeriveDocumentText() length = %d, want <= %d", len(got), MaxDocumentTextBytes)
	}
	if len(got) == 0 {
		t.Fatal("DeriveDocumentText() dropped an oversized body entirely")
	}
	for index, runeValue := range got {
		if runeValue == '\uFFFD' {
			t.Fatalf("DeriveDocumentText() split a rune at byte %d", index)
		}
	}
}

// An unvalidated model would let a malformed body reach a stored generated
// column, so the derivation refuses it rather than storing partial text.
func TestDeriveDocumentTextRejectsInvalidModel(t *testing.T) {
	t.Parallel()

	if _, err := DeriveDocumentText(recordmarkdown.DocumentRenderModel{}); err == nil {
		t.Fatal("DeriveDocumentText() error = nil, want rejection of an unvalidated model")
	}
}
