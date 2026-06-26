package installer

import (
	"strings"
	"testing"
)

func TestScriptMatchesReleaseChecksumManifestLines(t *testing.T) {
	t.Parallel()

	if !strings.Contains(Script, `awk -v asset="$ASSET" '$2 == asset`) {
		t.Fatalf("installer script should select checksum manifest rows by asset field, not whitespace-sensitive grep")
	}
	if strings.Contains(Script, `grep "  ${ASSET}$"`) {
		t.Fatalf("installer script should not require a GNU sha256sum two-space manifest separator")
	}
}

func TestScriptRestartsActiveServiceAfterInstall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string
	}{
		{name: "daemon reload", want: "systemctl daemon-reload"},
		{name: "enable only", want: "systemctl enable houfeng-agent"},
		{name: "active check", want: "if systemctl is-active --quiet houfeng-agent; then"},
		{name: "restart active", want: "systemctl restart houfeng-agent"},
		{name: "start inactive", want: "systemctl start houfeng-agent"},
	}

	previousIndex := -1
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			index := strings.Index(Script, tt.want)
			if index == -1 {
				t.Fatalf("installer script missing %q", tt.want)
			}
			if index <= previousIndex {
				t.Fatalf("installer script should run %q after the previous systemd step", tt.want)
			}
			previousIndex = index
		})
	}
	if strings.Contains(Script, "systemctl enable --now houfeng-agent") {
		t.Fatal("installer script should not use enable --now because it does not restart active services")
	}
}

func TestScriptPreservesCurrentAndLegacyPostEnrollmentTokens(t *testing.T) {
	t.Parallel()

	if !strings.Contains(Script, `grep -Eq '"(monitoring_instance_id|node_id)"' /etc/houfeng-agent/token`) {
		t.Fatal("installer script should preserve post-enrollment tokens with current monitoring_instance_id or legacy node_id")
	}
	if !strings.Contains(Script, `grep -q '"sync_token"' /etc/houfeng-agent/token`) {
		t.Fatal("installer script should require sync_token before preserving the token file")
	}
}

func TestScriptRequiresExplicitInsecureAllowHTTP(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"--insecure-allow-http",
		"INSECURE_ALLOW_HTTP=0",
		"INSECURE_ALLOW_HTTP=1",
		`http://*)`,
		`[ "$INSECURE_ALLOW_HTTP" = "1" ] || fail "--server-url http requires --insecure-allow-http"`,
		`https://*) ;;`,
	} {
		if !strings.Contains(Script, want) {
			t.Fatalf("installer script missing HTTP hardening snippet %q", want)
		}
	}
	if strings.Contains(Script, "http://*|https://*) ;;") {
		t.Fatal("installer script should not accept http:// server URLs without an explicit insecure flag")
	}
}

func TestScriptSupportsSaferEnrollmentTokenSources(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"--enrollment-token TOKEN",
		"--enrollment-token-file PATH",
		"--enrollment-token-stdin",
		"ENROLLMENT_TOKEN_FILE=\"\"",
		"READ_ENROLLMENT_TOKEN_STDIN=0",
		"TOKEN_SOURCE_COUNT=0",
		`[ "$TOKEN_SOURCE_COUNT" -eq 1 ] || fail "exactly one enrollment token source is required"`,
		`ENROLLMENT_TOKEN="$(tr -d '\r\n' < "$ENROLLMENT_TOKEN_FILE")"`,
		`ENROLLMENT_TOKEN="$(tr -d '\r\n')"`,
	} {
		if !strings.Contains(Script, want) {
			t.Fatalf("installer script missing safer token source snippet %q", want)
		}
	}
	if !strings.Contains(Script, "process list") || !strings.Contains(Script, "shell history") {
		t.Fatal("installer usage should warn about command-line enrollment token exposure")
	}
}

func TestScriptRequiresSignedChecksumManifest(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"HOUFENG_CHECKSUM_MINISIGN_PUBLIC_KEY=",
		`command -v minisign >/dev/null 2>&1 || fail "minisign is required to verify release checksums"`,
		`download "${BASE_URL}/sha256sums.txt.minisig" "${TMPDIR}/sha256sums.txt.minisig"`,
		`minisign -Vm "${TMPDIR}/sha256sums.txt" -P "$HOUFENG_CHECKSUM_MINISIGN_PUBLIC_KEY" -x "${TMPDIR}/sha256sums.txt.minisig"`,
		`info "checksum manifest signature verified"`,
	} {
		if !strings.Contains(Script, want) {
			t.Fatalf("installer script missing signed checksum manifest snippet %q", want)
		}
	}

	signatureIndex := strings.Index(Script, `minisign -Vm "${TMPDIR}/sha256sums.txt"`)
	checksumIndex := strings.Index(Script, `EXPECTED_SUM="$(awk -v asset="$ASSET"`)
	if signatureIndex == -1 || checksumIndex == -1 {
		t.Fatal("installer script should verify manifest signature and then extract checksum")
	}
	if signatureIndex > checksumIndex {
		t.Fatal("installer script should verify sha256sums.txt signature before reading checksum entries")
	}
	if strings.Contains(Script, "checksum-only") {
		t.Fatal("installer script must not describe a checksum-only fallback")
	}
	for _, line := range strings.Split(Script, "\n") {
		if strings.Contains(line, "minisign") && strings.Contains(line, "|| true") {
			t.Fatalf("installer script must not ignore minisign failure: %q", line)
		}
	}
}
