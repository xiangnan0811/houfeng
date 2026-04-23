package fingerprint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

const machineIDPath = "/etc/machine-id"

type Provider struct{}

func (Provider) Fingerprint(context.Context) (string, error) {
	if machineID, err := os.ReadFile(machineIDPath); err == nil {
		if value := strings.TrimSpace(string(machineID)); value != "" {
			return stableFingerprint(value), nil
		}
	}

	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("hostname: %w", err)
	}

	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return "", fmt.Errorf("hostname is empty")
	}

	return stableFingerprint(hostname), nil
}

func stableFingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
