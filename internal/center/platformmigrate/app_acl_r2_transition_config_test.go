package platformmigrate

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestLoadAppACLR2BootstrapConfigAcceptsOnlyBootstrapDatabaseURL(t *testing.T) {
	const bootstrapDSN = "postgres://bootstrap:bootstrap-secret@example.invalid/houfeng"

	var lookedUp []string
	config, err := LoadAppACLR2BootstrapConfig(func(key string) (string, bool) {
		lookedUp = append(lookedUp, key)
		switch key {
		case AppBootstrapDatabaseURLEnv:
			return bootstrapDSN, true
		default:
			t.Fatalf("LoadAppACLR2BootstrapConfig() queried forbidden environment key %q", key)
			return "", false
		}
	})
	if err != nil {
		t.Fatalf("LoadAppACLR2BootstrapConfig() error = %v", err)
	}
	if config.BootstrapDatabaseURL != bootstrapDSN {
		t.Fatalf("LoadAppACLR2BootstrapConfig() = %#v, want bootstrap DSN", config)
	}
	if want := []string{AppBootstrapDatabaseURLEnv}; !reflect.DeepEqual(lookedUp, want) {
		t.Fatalf("LoadAppACLR2BootstrapConfig() looked up %#v, want %#v", lookedUp, want)
	}
}

func TestLoadAppACLR2BootstrapConfigRejectsMissingOrBlankWithoutLeakingValue(t *testing.T) {
	const secretDSN = "postgres://bootstrap:bootstrap-secret@example.invalid/houfeng"

	for _, tc := range []struct {
		name  string
		value string
		ok    bool
	}{
		{name: "missing", value: secretDSN, ok: false},
		{name: "empty", ok: true},
		{name: "blank", value: " \t\n ", ok: true},
		{name: "nil_lookup"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var lookup func(string) (string, bool)
			if tc.name != "nil_lookup" {
				lookup = func(key string) (string, bool) {
					if key != AppBootstrapDatabaseURLEnv {
						t.Fatalf("LoadAppACLR2BootstrapConfig() queried forbidden environment key %q", key)
					}
					return tc.value, tc.ok
				}
			}

			_, err := LoadAppACLR2BootstrapConfig(lookup)
			if !errors.Is(err, ErrInvalidAppACLR2BootstrapConfig) {
				t.Fatalf("LoadAppACLR2BootstrapConfig() error = %v, want ErrInvalidAppACLR2BootstrapConfig", err)
			}
			for _, value := range []string{tc.value, secretDSN} {
				if value != "" && strings.Contains(err.Error(), value) {
					t.Fatalf("LoadAppACLR2BootstrapConfig() error leaked value %q: %q", value, err)
				}
			}
		})
	}
}
