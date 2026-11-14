package deploy

import "houfeng/internal/center/deletionledger"

// LegacySourceRetirementResult reports retirement of an authorization identity.
// The source asset and associated business content are not physically purged.
type LegacySourceRetirementResult struct {
	Entry            deletionledger.SourceRetirementEntry
	HandoffInstalled bool
}
