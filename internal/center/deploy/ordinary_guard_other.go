//go:build !linux

package deploy

import (
	"context"
	"errors"
)

const OrdinaryComposeGuardRoot = "/var/lib/houfeng-maintenance/ordinary"

const OrdinaryComposeInitializationMarker = "ordinary-initialization.json"

// ProvisionOrdinaryComposeGuard requires Linux protected file semantics.
func ProvisionOrdinaryComposeGuard(string) error {
	return errors.New("ordinary Compose guard requires Linux")
}

// WithOrdinaryComposeGuard requires Linux protected file and flock semantics.
func WithOrdinaryComposeGuard(context.Context, string, bool, func() error) error {
	return errors.New("ordinary Compose guard requires Linux")
}

// RunOrdinaryNativeParticipant requires Linux process and guard semantics.
func RunOrdinaryNativeParticipant(context.Context) error {
	return errors.New("ordinary native participant requires Linux")
}

// RunOrdinaryComposeUpgrade requires Linux Docker and process fencing.
func RunOrdinaryComposeUpgrade(context.Context, string) error {
	return errors.New("ordinary Compose upgrade requires Linux")
}
