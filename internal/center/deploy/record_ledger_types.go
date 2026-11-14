package deploy

import (
	"errors"

	"houfeng/internal/center/recorddeletion"
)

// ErrRecordLedgerFence means that the offline ledger ceremony cannot establish
// its complete, current authority. No caller should infer absence from it.
var ErrRecordLedgerFence = errors.New("record ledger maintenance fence unavailable")

// ErrRecordLedgerUnexpectedSession identifies an actual unregistered APP
// backend observation, separately from catalog, transport, or query failures.
var ErrRecordLedgerUnexpectedSession = errors.Join(ErrRecordLedgerFence, errors.New("unregistered APP PostgreSQL session"))

// RecordLedgerApplicationIdentity is the physical identity of the isolated APP
// database. It is checked against both the persisted domain row and PostgreSQL.
type RecordLedgerApplicationIdentity struct {
	ID               string
	SystemIdentifier string
	DatabaseName     string
	DatabaseOID      uint32
	Epoch            uint64
}

// RecordLedgerDomainRequest describes one independently administered ledger.
// URLs are secrets and must come from the management command's private input;
// they are never written into the maintenance journal or included in errors.
type RecordLedgerDomainRequest struct {
	Identity        recorddeletion.LedgerPhysicalIdentity
	BootstrapURL    string
	ReaderURL       string
	WriterURL       string
	PreviousWriters []string
}

// RecordLedgerMaintenanceRequest is the explicitly registered inventory for a
// single offline ceremony. ComposeContainerIDs includes the isolated APP DB.
// HostCgroups are complete cgroup-v2 subtrees, not individual process IDs.
// A protected journal preserves the original inventory across interruption.
type RecordLedgerMaintenanceRequest struct {
	Compose             ComposeOfflineFenceRequest
	AppBootstrapURL     string
	AppPreviousAdminURL string
	AppAdminURL         string
	AppIdentity         RecordLedgerApplicationIdentity
	AppAdminRole        string
	AppPreviousLogins   []string
	Primary             RecordLedgerDomainRequest
	Witness             RecordLedgerDomainRequest
	ComposeContainerIDs []string
	HostCgroups         []string
}

// LegacyC80ServiceRequest selects only a record and actor in the already bound
// APP. RealmFD must be the live root supervisor's private seqpacket endpoint.
type LegacyC80ServiceRequest struct {
	ActorUserID string
	RecordID    string
	RealmFD     int
}

type LegacyC80ServiceResult struct {
	Status      string
	OperationID string
	Reason      string
}
