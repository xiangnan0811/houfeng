package recordcollaboration

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

type commentMarkdownCorpus struct {
	ContractVersion string                      `json:"contract_version"`
	Cases           []commentMarkdownCorpusCase `json:"cases"`
	HostileModels   []commentMarkdownModelCase  `json:"hostile_models"`
}

type commentMarkdownModelCase struct {
	Name  string          `json:"name"`
	Model json.RawMessage `json:"model"`
}

type commentMarkdownCorpusCase struct {
	Name           string                         `json:"name"`
	Source         string                         `json:"source"`
	SourceBase64   string                         `json:"source_base64"`
	SourceTemplate *commentMarkdownSourceTemplate `json:"source_template"`
	Valid          bool                           `json:"valid"`
	Model          json.RawMessage                `json:"model"`
}

type commentMarkdownSourceTemplate struct {
	Prefix string `json:"prefix"`
	Repeat string `json:"repeat"`
	Count  int    `json:"count"`
	Suffix string `json:"suffix"`
}

func TestCommentMarkdownV1SharedCorpus(t *testing.T) {
	corpus := loadCommentMarkdownCorpus(t)
	if corpus.ContractVersion != CommentRenderContractVersionV1 {
		t.Fatalf("contract_version = %q", corpus.ContractVersion)
	}
	for _, test := range corpus.Cases {
		t.Run(test.Name, func(t *testing.T) {
			source := corpusCaseSource(t, test)
			model, err := ParseCommentMarkdownV1(source)
			if !test.Valid {
				if !errors.Is(err, ErrInvalidCommentMarkdown) {
					t.Fatalf("ParseCommentMarkdownV1() error = %v, want ErrInvalidCommentMarkdown", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCommentMarkdownV1() error = %v", err)
			}
			want, err := DecodeCommentRenderModelV1(test.Model)
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
			clone := model.Clone()
			if !model.Equal(clone) {
				t.Fatal("Clone() changed model")
			}
		})
	}
}

func TestCommentMarkdownV1ModelRejectsUntrustedShapes(t *testing.T) {
	for _, test := range loadCommentMarkdownCorpus(t).HostileModels {
		t.Run(test.Name, func(t *testing.T) {
			if _, err := DecodeCommentRenderModelV1(test.Model); !errors.Is(err, ErrInvalidCommentMarkdown) {
				t.Fatalf("DecodeCommentRenderModelV1(%s) error = %v", test.Model, err)
			}
		})
	}
}

func loadCommentMarkdownCorpus(t *testing.T) commentMarkdownCorpus {
	t.Helper()
	raw, err := os.ReadFile("testdata/comment_markdown_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus commentMarkdownCorpus
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&corpus); err != nil {
		t.Fatal(err)
	}
	return corpus
}

func corpusCaseSource(t *testing.T, test commentMarkdownCorpusCase) string {
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
