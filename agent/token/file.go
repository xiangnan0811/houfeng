package token

import (
	"context"
	"fmt"
	"os"
	"strings"
)

type FileSource struct {
	Path string
}

func (s FileSource) Token(context.Context) (string, error) {
	payload, err := os.ReadFile(s.Path)
	if err != nil {
		return "", fmt.Errorf("read token file %q: %w", s.Path, err)
	}

	token := strings.TrimSpace(string(payload))
	if token == "" {
		return "", fmt.Errorf("token file %q is empty", s.Path)
	}

	return token, nil
}
