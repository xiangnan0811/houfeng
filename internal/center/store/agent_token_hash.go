package store

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

const (
	agentTokenHashPrefix     = "hmac-sha256:v1:"
	defaultAgentTokenHMACKey = "houfeng-agent-token-default-test-key"
)

type agentTokenHasher struct {
	enrollmentKey []byte
	syncKey       []byte
}

func defaultAgentTokenHasher() agentTokenHasher {
	return newAgentTokenHasher(nil)
}

func newAgentTokenHasher(rootKey []byte) agentTokenHasher {
	if len(rootKey) == 0 {
		rootKey = []byte(defaultAgentTokenHMACKey)
	}
	return agentTokenHasher{
		enrollmentKey: deriveAgentTokenKey(rootKey, "houfeng-enrollment-token-v1"),
		syncKey:       deriveAgentTokenKey(rootKey, "houfeng-agent-sync-token-v1"),
	}
}

func deriveAgentTokenKey(rootKey []byte, label string) []byte {
	mac := hmac.New(sha256.New, rootKey)
	_, _ = mac.Write([]byte(label))
	return mac.Sum(nil)
}

func (h agentTokenHasher) hashEnrollmentToken(token string) string {
	h = h.withDefaults()
	return h.hashToken(h.enrollmentKey, token)
}

func (h agentTokenHasher) hashSyncToken(token string) string {
	h = h.withDefaults()
	return h.hashToken(h.syncKey, token)
}

func (h agentTokenHasher) withDefaults() agentTokenHasher {
	if len(h.enrollmentKey) == 0 || len(h.syncKey) == 0 {
		return defaultAgentTokenHasher()
	}
	return h
}

func (h agentTokenHasher) hashToken(key []byte, token string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(token))
	return agentTokenHashPrefix + hex.EncodeToString(mac.Sum(nil))
}

func (h agentTokenHasher) enrollmentTokenMatches(storedHash, token string) bool {
	h = h.withDefaults()
	return agentTokenHashMatches(storedHash, h.hashEnrollmentToken(token), hashOpaqueToken(token))
}

func (h agentTokenHasher) syncTokenMatches(storedHash, token string) bool {
	h = h.withDefaults()
	return agentTokenHashMatches(storedHash, h.hashSyncToken(token), hashOpaqueToken(token))
}

func agentTokenHashMatches(storedHash, currentHash, legacyHash string) bool {
	switch {
	case isHMACAgentTokenHash(storedHash):
		return constantTimeStringEqual(storedHash, currentHash)
	default:
		return constantTimeStringEqual(storedHash, legacyHash)
	}
}

func isHMACAgentTokenHash(value string) bool {
	hash := strings.TrimSpace(value)
	return strings.HasPrefix(hash, agentTokenHashPrefix) && len(strings.TrimPrefix(hash, agentTokenHashPrefix)) == sha256.Size*2
}

func isLegacySHA256TokenHash(value string) bool {
	hash := strings.TrimSpace(value)
	if len(hash) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(hash)
	return err == nil
}

func constantTimeStringEqual(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
