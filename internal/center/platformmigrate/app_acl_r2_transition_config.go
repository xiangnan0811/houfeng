package platformmigrate

import (
	"errors"
	"strings"
)

const AppBootstrapDatabaseURLEnv = "HOUFENG_RECORD_PLATFORM_APP_BOOTSTRAP_DATABASE_URL"

var ErrInvalidAppACLR2BootstrapConfig = errors.New("app ACL R2 bootstrap configuration is invalid")

// AppACLR2BootstrapConfig contains the sole environment-derived value accepted
// by the APP ACL R2 bootstrap command.
type AppACLR2BootstrapConfig struct {
	BootstrapDatabaseURL string
}

// LoadAppACLR2BootstrapConfig reads only the bootstrap database URL. It does
// not use shared configuration because that could resolve unrelated inputs.
func LoadAppACLR2BootstrapConfig(lookup func(string) (string, bool)) (AppACLR2BootstrapConfig, error) {
	if lookup == nil {
		return AppACLR2BootstrapConfig{}, ErrInvalidAppACLR2BootstrapConfig
	}

	databaseURL, ok := lookup(AppBootstrapDatabaseURLEnv)
	databaseURL = strings.TrimSpace(databaseURL)
	if !ok || databaseURL == "" {
		return AppACLR2BootstrapConfig{}, ErrInvalidAppACLR2BootstrapConfig
	}
	return AppACLR2BootstrapConfig{BootstrapDatabaseURL: databaseURL}, nil
}
