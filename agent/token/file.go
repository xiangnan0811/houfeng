package token

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type FileSource struct {
	Path string
}

type fileCredentials struct {
	NodeID    string `json:"node_id"`
	SyncToken string `json:"sync_token"`
}

func (s FileSource) Token(ctx context.Context) (string, error) {
	credentials, err := s.load(ctx)
	if err != nil {
		return "", err
	}
	if credentials.EnrollmentToken == "" {
		return "", fmt.Errorf("token file %q does not contain an enrollment token", s.Path)
	}
	return credentials.EnrollmentToken, nil
}

func (s FileSource) SyncCredentials(ctx context.Context) (string, string, bool, error) {
	credentials, err := s.load(ctx)
	if err != nil {
		return "", "", false, err
	}
	if credentials.NodeID == "" && credentials.SyncToken == "" {
		return "", "", false, nil
	}
	if credentials.NodeID == "" || credentials.SyncToken == "" {
		return "", "", false, fmt.Errorf("token file %q contains incomplete sync credentials", s.Path)
	}
	return credentials.NodeID, credentials.SyncToken, true, nil
}

func (s FileSource) SaveSyncCredentials(_ context.Context, nodeID, syncToken string) error {
	nodeID = strings.TrimSpace(nodeID)
	syncToken = strings.TrimSpace(syncToken)
	if nodeID == "" || syncToken == "" {
		return fmt.Errorf("sync credentials for token file %q must include node_id and sync_token", s.Path)
	}

	payload, err := json.Marshal(fileCredentials{NodeID: nodeID, SyncToken: syncToken})
	if err != nil {
		return fmt.Errorf("encode sync credentials for token file %q: %w", s.Path, err)
	}
	payload = append(payload, '\n')

	file, err := os.OpenFile(s.Path, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open token file %q for sync credential write: %w", s.Path, err)
	}
	defer func() {
		_ = file.Close()
	}()
	if _, err := file.Write(payload); err != nil {
		return fmt.Errorf("write sync credentials to token file %q: %w", s.Path, err)
	}
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("chmod token file %q: %w", s.Path, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync token file %q: %w", s.Path, err)
	}
	return nil
}

type credentials struct {
	EnrollmentToken string
	NodeID          string
	SyncToken       string
}

func (s FileSource) load(context.Context) (credentials, error) {
	payload, err := os.ReadFile(s.Path)
	if err != nil {
		return credentials{}, fmt.Errorf("read token file %q: %w", s.Path, err)
	}

	value := strings.TrimSpace(string(payload))
	if value == "" {
		return credentials{}, fmt.Errorf("token file %q is empty", s.Path)
	}
	if strings.HasPrefix(value, "{") {
		var stored fileCredentials
		if err := json.Unmarshal([]byte(value), &stored); err != nil {
			return credentials{}, fmt.Errorf("parse token file %q sync credentials: %w", s.Path, err)
		}
		return credentials{
			NodeID:    strings.TrimSpace(stored.NodeID),
			SyncToken: strings.TrimSpace(stored.SyncToken),
		}, nil
	}

	return credentials{EnrollmentToken: value}, nil
}
