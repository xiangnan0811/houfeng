package redact

import "regexp"

const replacement = "[redacted]"

var (
	privateKeyBlockPattern = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
	authorizationPattern   = regexp.MustCompile(`(?i)(authorization\s*:\s*bearer\s+)[^\s"']+`)
	keyValuePatterns       = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(token|access_token|refresh_token|api_key|secret|password)\b(\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;&]+)`),
		regexp.MustCompile(`(?i)("(?:token|access_token|refresh_token|api_key|secret|password)"\s*:\s*)("[^"]*"|'[^']*'|[^\s,;&}]+)`),
	}
)

// Secrets redacts common credential-shaped substrings from diagnostic text.
func Secrets(value string) string {
	out := privateKeyBlockPattern.ReplaceAllString(value, replacement)
	out = authorizationPattern.ReplaceAllString(out, "${1}"+replacement)
	out = keyValuePatterns[0].ReplaceAllString(out, "${1}${2}"+replacement)
	out = keyValuePatterns[1].ReplaceAllString(out, "${1}\""+replacement+"\"")
	return out
}
