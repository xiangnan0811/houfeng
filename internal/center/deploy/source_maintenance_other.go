//go:build !linux

package deploy

import "context"

// The maintenance protocol depends on Linux openat2 and flock. Never emulate it
// with an unlocked fallback on another platform.
func ProvisionMaintenanceGuard(string) (MaintenanceGuardIdentity, error) {
	return MaintenanceGuardIdentity{}, ErrMaintenanceGuardMismatch
}

func WithMaintenanceGuard(context.Context, string, MaintenanceGuardIdentity, bool, func() error) error {
	return ErrMaintenanceGuardMismatch
}

func loadSourceMaintenanceConfig(string) (SourceMaintenanceConfig, error) {
	return SourceMaintenanceConfig{}, ErrMaintenanceGuardMismatch
}

func checkSourceConfigParent(string) error { return ErrMaintenanceGuardMismatch }
func publishSourceMaintenanceConfig(string, SourceMaintenanceConfig) error {
	return ErrMaintenanceGuardMismatch
}
func syncSourceConfigReplay(string, SourceMaintenanceConfig) error {
	return ErrMaintenanceGuardMismatch
}
func publishSourceMaintenanceConfigReplacement(string, SourceMaintenanceConfig, SourceMaintenanceConfig) error {
	return ErrMaintenanceGuardMismatch
}
func finishSourceConfigPublication(string) error { return ErrMaintenanceGuardMismatch }
