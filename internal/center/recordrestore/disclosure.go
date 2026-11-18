package recordrestore

import (
	"encoding/json"
	"fmt"

	"houfeng/internal/center/recorddeletion"
)

func EncodeExternalCopies(copies []recorddeletion.SurvivingCopySummary) ([]byte, error) {
	payload := encodedExternalCopies{Copies: make([]encodedExternalCopy, 0, len(copies))}
	for _, copy := range copies {
		if err := copy.Validate(); err != nil {
			return nil, fmt.Errorf("%w: external copy", ErrInvalidRestoreRequest)
		}
		var copyCount *uint64
		if copy.CountStatus == recorddeletion.SurvivingCopyCountKnown {
			count := *copy.CopyCount
			copyCount = &count
		}
		payload.Copies = append(payload.Copies, encodedExternalCopy{
			Scope:       string(copy.Scope),
			Kind:        string(copy.Kind),
			CountStatus: string(copy.CountStatus),
			CopyCount:   copyCount,
		})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: external copy", ErrInvalidRestoreRequest)
	}
	return encoded, nil
}

type encodedExternalCopies struct {
	Copies []encodedExternalCopy `json:"copies"`
}

// encodedExternalCopy is identity-free: unknown copies encode copy_count as
// null rather than a fabricated count.
type encodedExternalCopy struct {
	Scope       string  `json:"scope"`
	Kind        string  `json:"kind"`
	CountStatus string  `json:"count_status"`
	CopyCount   *uint64 `json:"copy_count"`
}
