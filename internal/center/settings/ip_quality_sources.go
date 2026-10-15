package settings

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// MaxMindCredentialFingerprint namespaces caches by the complete credential pair.
// JSON tuple encoding prevents ambiguous account/key concatenations.
func MaxMindCredentialFingerprint(accountID, licenseKey string) string {
	if accountID == "" || licenseKey == "" {
		return ""
	}
	data, _ := json.Marshal([2]string{accountID, licenseKey})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validateMaxMindCredentials(sources IPQualitySources) error {
	id, key := sources.MaxMindAccountID, sources.MaxMindLicenseKey
	if (id == "") != (key == "") {
		return invalidSettings("maxmind account and license key must be configured together")
	}
	if len(id) > 256 || len(key) > 256 || strings.ContainsAny(key, "\x00\r\n") {
		return invalidSettings("invalid maxmind credentials")
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return invalidSettings("invalid maxmind account id")
		}
	}
	return nil
}
