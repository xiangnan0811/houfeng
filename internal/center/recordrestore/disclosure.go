package recordrestore

import (
	"encoding/json"
	"fmt"

	"houfeng/internal/center/recorddeletion"
)

func EncodeExternalCopies(copies []recorddeletion.SurvivingCopySummary) ([]byte, error) {
	payload := encodedExternalCopies{Copies: make([]encodedExternalCopy, 0, len(copies))}
	for _, copy := range copies {
		if copy.Kind == "" || copy.CopyCount == 0 || copy.Scope == "" {
			return nil, fmt.Errorf("%w: external copy", ErrInvalidRestoreRequest)
		}
		payload.Copies = append(payload.Copies, encodedExternalCopy{
			Scope:     string(copy.Scope),
			Kind:      string(copy.Kind),
			CopyCount: copy.CopyCount,
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

type encodedExternalCopy struct {
	Scope     string `json:"scope"`
	Kind      string `json:"kind"`
	CopyCount uint64 `json:"copy_count"`
}
