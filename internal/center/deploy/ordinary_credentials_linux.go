//go:build linux

package deploy

import "bytes"

// The root controller reads private secret files into memory only. The returned
// values belong solely in the authenticated native EXECUTE pipe, never in a
// journal, environment, argument vector, error, or status message.
func ordinaryReadUpgradeCredentials(request ordinaryUpgradeRequest) (ComposeInitPasswords, []ordinarySecretIdentity, error) {
	paths := []string{request.Secrets.Bootstrap, request.Secrets.Runtime, request.Secrets.PlatformAdmin, request.Secrets.DeletionWorker, request.Secrets.Migrator, request.Secrets.InitialPassword, request.Secrets.SessionHMACKey}
	values := make([]string, 0, len(paths))
	identities := make([]ordinarySecretIdentity, 0, len(paths))
	for i, path := range paths {
		body, identity, err := readOrdinaryPrivateIdentity(path, 64<<10)
		if err != nil {
			return ComposeInitPasswords{}, nil, errOrdinaryUpgrade
		}
		if i < 5 {
			value := string(body)
			clear(body)
			if !validComposePassword(value) {
				return ComposeInitPasswords{}, nil, errOrdinaryUpgrade
			}
			values = append(values, value)
		} else {
			normalized := bytes.TrimSpace(body)
			valid := len(normalized) > 0 && (i != 6 || len(normalized) >= 32)
			clear(body)
			if !valid {
				return ComposeInitPasswords{}, nil, errOrdinaryUpgrade
			}
		}
		identities = append(identities, identity)
	}
	passwords := ComposeInitPasswords{Bootstrap: values[0], Runtime: values[1], PlatformAdmin: values[2], DeletionWorker: values[3], Migrator: values[4]}
	if _, err := NewComposeInitConfig(passwords); err != nil {
		return ComposeInitPasswords{}, nil, errOrdinaryUpgrade
	}
	return passwords, identities, nil
}
