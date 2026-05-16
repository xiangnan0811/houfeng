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
