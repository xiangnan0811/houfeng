package deploy

import "errors"

// ErrMaintenanceGuardMismatch means the configured guard is missing, unsafe,
// or no longer matches its explicitly provisioned identity.
var ErrMaintenanceGuardMismatch = errors.New("maintenance guard identity mismatch")

// MaintenanceGuardIdentity anchors a persistent directory and lock file outside
// the recovery unit. It establishes exclusion only in one shared flock domain;
// endpoint and concurrently running clone fencing are separate requirements.
type MaintenanceGuardIdentity struct {
	DirectoryDevice uint64   `json:"directory_device"`
	DirectoryInode  uint64   `json:"directory_inode"`
	FileDevice      uint64   `json:"file_device"`
	FileInode       uint64   `json:"file_inode"`
	Nonce           [32]byte `json:"nonce"`
}
