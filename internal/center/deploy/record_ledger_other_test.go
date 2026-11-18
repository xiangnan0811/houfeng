//go:build !linux

package deploy

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"houfeng/internal/center/deletionledger"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/sourceretirement"
)

var _ interface {
	PreviewLegacySourceRetirement(context.Context, string, sourceretirement.SourceKey, recordauth.VisibilityScope) (deletionledger.SourceRetirementRequest, [32]byte, error)
	RetireLegacySourceRetirement(context.Context, deletionledger.SourceRetirementRequest, [32]byte) (LegacySourceRetirementResult, error)
	ReconcileLegacySourceRetirement(context.Context, deletionledger.SourceRetirementRequest) (LegacySourceRetirementResult, error)
} = (*RecordLedgerFence)(nil)

func TestRecordLedgerSourceRetirementUnavailable(t *testing.T) {
	fence := &RecordLedgerFence{}
	request, token, err := fence.PreviewLegacySourceRetirement(context.Background(), "actor", sourceretirement.SourceKey{}, recordauth.VisibilityScope{})
	if !errors.Is(err, ErrRecordLedgerFence) || !reflect.DeepEqual(request, deletionledger.SourceRetirementRequest{}) || token != [32]byte{} {
		t.Fatal("unsupported preview did not fail closed")
	}
	result, err := fence.RetireLegacySourceRetirement(context.Background(), request, token)
	if !errors.Is(err, ErrRecordLedgerFence) || !reflect.DeepEqual(result, LegacySourceRetirementResult{}) {
		t.Fatal("unsupported retirement did not fail closed")
	}
	result, err = fence.ReconcileLegacySourceRetirement(context.Background(), request)
	if !errors.Is(err, ErrRecordLedgerFence) || !reflect.DeepEqual(result, LegacySourceRetirementResult{}) {
		t.Fatal("unsupported reconciliation did not fail closed")
	}
}
