//go:build !linux

package main

// Source retirement requires the Linux no-symlink private-file and maintenance
// fence primitives. There is no less restrictive fallback on other systems.
type legacySourcePrivateOutput struct{}

func reserveLegacySourceOutput(string) (*legacySourcePrivateOutput, error) {
	return nil, errLegacySourceInput
}

func (*legacySourcePrivateOutput) close() {}

func (*legacySourcePrivateOutput) publish(legacySourceTokenFile) error {
	return errLegacySourceOutput
}

func readLegacySourceTokenFile(string) ([]byte, error) {
	return nil, errLegacySourceInput
}
