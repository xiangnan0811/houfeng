package deploy

// ComposeOfflineFenceRequest identifies one explicitly authorized offline operation.
type ComposeOfflineFenceRequest struct {
	RecoveryID          string
	Project             string
	WorkingDirectory    string
	DatabaseContainerID string
	ControlConfigPath   string
}

// ComposeOfflineFence is an observation, not a durable proof that the host remains
// offline. The operator must keep external restart controllers, unsupervised host
// writers and maintenance commands stopped; Docker cannot establish their absence.
// The coordinator must perform the protected ceremony in the callback while
// the same exclusive maintenance guard remains held.
type ComposeOfflineFence struct {
	RecoveryID           string
	DatabaseContainerID  string
	MaintenanceNetworkID string
	DatabaseAddress      string
	InventoryDigest      [32]byte
}
