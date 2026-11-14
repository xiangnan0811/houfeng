//go:build !linux

package deploy

import "context"

// FenceComposeSource is unavailable without the Linux guard and filesystem APIs.
func FenceComposeSource(context.Context, ComposeOfflineFenceRequest, func(ComposeOfflineFence) error) error {
	return ErrMaintenanceGuardMismatch
}
