package recordmarkdown

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"houfeng/internal/center/recordcollaboration"
)

type documentMarkdownCorpus struct {
	ContractVersion       string                       `json:"contract_version"`
	SharedCommentContract string                       `json:"shared_comment_contract"`
	Cases                 []documentMarkdownCorpusCase `json:"cases"`
	HostileModels         []documentMarkdownModelCase  `json:"hostile_models"`
}

type documentMarkdownModelCase struct {
	Name  string          `json:"name"`
	Model json.RawMessage `json:"model"`
}

type documentMarkdownCorpusCase struct {
	Name                 string              `json:"name"`
	Source               string              `json:"source"`
	SourceBase64         string              `json:"source_base64"`
	Valid                bool                `json:"valid"`
	AuthorizedReferences []DocumentReference `json:"authorized_references"`
	Model                json.RawMessage     `json:"model"`
}

func TestDocumentMarkdownV1SharedCommentCorpus(t *testing.T) {
	corpus := loadCommentMarkdownCorpus(t)
	for _, test := range corpus.Cases {
		if !test.Valid {
			continue
		}
		t.Run(test.Name, func(t *testing.T) {
			source := commentCorpusCaseSource(t, test)
			commentModel, err := recordcollaboration.ParseCommentMarkdownV1(source)
			if err != nil {
				t.Fatalf("ParseCommentMarkdownV1() error = %v", err)
			}
			documentModel, err := ParseDocumentMarkdownV1(source, nil)
			if err != nil {
				t.Fatalf("ParseDocumentMarkdownV1() error = %v", err)
			}
			if documentModel.Version != DocumentRenderContractVersionV1 {
				t.Fatalf("version = %q", documentModel.Version)
			}
			got := documentModel.CommentProjection()
			if !got.Equal(commentModel) {
				gotJSON, _ := json.Marshal(got)
				wantJSON, _ := json.Marshal(commentModel)
				t.Fatalf("shared projection drifted\ngot  %s\nwant %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestDocumentMarkdownV1SharedHostileCommentCasesRemainRejected(t *testing.T) {
	corpus := loadCommentMarkdownCorpus(t)
	documentAllowed := map[string]struct{}{
		"empty":                 {},
		"heading":               {},
		"setext heading":        {},
		"table":                 {},
		"leading pipe table":    {},
		"task list":             {},
		"ordered task list":     {},
		"footnote":              {},
		"blockquote":            {},
		"thematic break":        {},
		"source byte limit":     {},
		"render node limit":     {},
		"render depth limit":    {},
		"serialized link limit": {},
	}
	for _, test := range corpus.Cases {
		if test.Valid {
			continue
		}
		if _, allowed := documentAllowed[test.Name]; allowed {
			continue
		}
		t.Run(test.Name, func(t *testing.T) {
			source := commentCorpusCaseSource(t, test)
			_, err := ParseDocumentMarkdownV1(source, nil)
			if !errors.Is(err, ErrInvalidDocumentMarkdown) {
				t.Fatalf("ParseDocumentMarkdownV1() error = %v, want ErrInvalidDocumentMarkdown", err)
			}
		})
	}
}

func TestDocumentMarkdownV1Corpus(t *testing.T) {
	corpus := loadDocumentMarkdownCorpus(t)
	if corpus.ContractVersion != DocumentRenderContractVersionV1 {
		t.Fatalf("contract_version = %q", corpus.ContractVersion)
	}
	if corpus.SharedCommentContract != recordcollaboration.CommentRenderContractVersionV1 {
		t.Fatalf("shared_comment_contract = %q", corpus.SharedCommentContract)
	}
	for _, test := range corpus.Cases {
		t.Run(test.Name, func(t *testing.T) {
			source := documentCorpusCaseSource(t, test)
			model, err := ParseDocumentMarkdownV1(source, test.AuthorizedReferences)
			if !test.Valid {
				if !errors.Is(err, ErrInvalidDocumentMarkdown) {
					t.Fatalf("ParseDocumentMarkdownV1() error = %v, want ErrInvalidDocumentMarkdown", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseDocumentMarkdownV1() error = %v", err)
			}
			want, err := DecodeDocumentRenderModelV1(test.Model)
			if err != nil {
				t.Fatalf("decode expected model: %v", err)
			}
			if !model.Equal(want) {
				gotJSON, _ := json.Marshal(model)
				wantJSON, _ := json.Marshal(want)
				t.Fatalf("model = %s, want %s", gotJSON, wantJSON)
			}
			if err := model.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestDocumentMarkdownV1RejectsHostileModels(t *testing.T) {
	corpus := loadDocumentMarkdownCorpus(t)
	for _, test := range corpus.HostileModels {
		t.Run(test.Name, func(t *testing.T) {
			_, err := DecodeDocumentRenderModelV1(test.Model)
			if !errors.Is(err, ErrInvalidDocumentMarkdown) {
				t.Fatalf("DecodeDocumentRenderModelV1() error = %v, want ErrInvalidDocumentMarkdown", err)
			}
		})
	}
}

type commentMarkdownCorpus struct {
	Cases []commentMarkdownCorpusCase `json:"cases"`
}

type commentMarkdownCorpusCase struct {
	Name           string                         `json:"name"`
	Source         string                         `json:"source"`
	SourceBase64   string                         `json:"source_base64"`
	SourceTemplate *commentMarkdownSourceTemplate `json:"source_template"`
	Valid          bool                           `json:"valid"`
}

type commentMarkdownSourceTemplate struct {
	Prefix string `json:"prefix"`
	Repeat string `json:"repeat"`
	Count  int    `json:"count"`
	Suffix string `json:"suffix"`
}

func loadDocumentMarkdownCorpus(t *testing.T) documentMarkdownCorpus {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "markdown", "houfeng-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus documentMarkdownCorpus
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	return corpus
}

func loadCommentMarkdownCorpus(t *testing.T) commentMarkdownCorpus {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "internal", "center", "recordcollaboration", "testdata", "comment_markdown_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus commentMarkdownCorpus
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	return corpus
}

func documentCorpusCaseSource(t *testing.T, test documentMarkdownCorpusCase) string {
	t.Helper()
	if test.SourceBase64 != "" {
		raw, err := base64.StdEncoding.DecodeString(test.SourceBase64)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	return test.Source
}

func commentCorpusCaseSource(t *testing.T, test commentMarkdownCorpusCase) string {
	t.Helper()
	if test.SourceBase64 != "" {
		raw, err := base64.StdEncoding.DecodeString(test.SourceBase64)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	if test.SourceTemplate != nil {
		return test.SourceTemplate.Prefix + strings.Repeat(test.SourceTemplate.Repeat, test.SourceTemplate.Count) + test.SourceTemplate.Suffix
	}
	return test.Source
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}
