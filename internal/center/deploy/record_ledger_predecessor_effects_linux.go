//go:build linux

package deploy

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

func (call *ledgerPredecessorCall) effect(ctx context.Context, op string, body []byte) (any, error) {
	response := map[string]any{"op": op + "_ack", "phase_generation": call.intent.PhaseGeneration}
	switch op {
	case "preview_intent":
		fields, err := ledgerCallbackFields(body, "op", "phase_generation", "command")
		if err != nil {
			return nil, err
		}
		command, digest, err := ledgerC80DecodeCreate(fields["command"])
		if err != nil {
			return nil, err
		}
		if command.ProjectID != call.config.ProjectID || command.ObjectID != call.config.RecordID || command.ActorScopeDigest != call.actorDigest || call.denied || command.WitnessSequence != call.witness.Sequence || command.WitnessHash != hexDigest(call.witness.EntryHash) {
			return nil, ErrRecordLedgerFence
		}
		intent := ledgerPredecessorCreateIntent{ledgerPredecessorEffectBinding: call.binding(), Command: command, CommandDigest: digest}
		if call.create != nil {
			if *call.create != intent || ledgerReadExact(call.fence.control, ledgerPredecessorName("create-intent", call.intent.PhaseGeneration), intent) != nil {
				return nil, ErrRecordLedgerFence
			}
		} else {
			err := ledgerPredecessorReadonly(ctx, call.fence, func(tx pgx.Tx) error {
				_, found, err := ledgerC80ReadPreview(ctx, tx, command)
				if err != nil {
					return err
				}
				if found {
					return ErrRecordLedgerFence
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
			if _, err := ledgerPublishExact(call.fence.control, ledgerPredecessorName("create-intent", call.intent.PhaseGeneration), intent); err != nil {
				return nil, err
			}
			call.create = &intent
			if err := call.cutpoint(ctx, "create-intent", "create-intent"); err != nil {
				return nil, err
			}
		}
		response["command_digest"] = digest
	case "preview_result":
		fields, err := ledgerCallbackFields(body, "op", "phase_generation", "command_digest", "reservation_id", "expires_at")
		if err != nil || call.create == nil {
			return nil, ErrRecordLedgerFence
		}
		var digest, id, expires string
		if ledgerCallbackDecode(fields, "command_digest", &digest) != nil || digest != call.create.CommandDigest || ledgerCallbackDecode(fields, "reservation_id", &id) != nil || ledgerCallbackDecode(fields, "expires_at", &expires) != nil {
			return nil, ErrRecordLedgerFence
		}
		expiry, err := time.Parse(time.RFC3339Nano, expires)
		if err != nil || expiry.Nanosecond()%1000 != 0 || expiry.UTC().Format(time.RFC3339Nano) != expires {
			return nil, ErrRecordLedgerFence
		}
		receipt := ledgerPredecessorCreateResult{ledgerPredecessorEffectBinding: call.binding(), CommandDigest: digest, ReservationID: id, ExpiresAt: expiry.UTC()}
		if call.preview != nil {
			if *call.preview != receipt || ledgerReadExact(call.fence.control, ledgerPredecessorName("create-result", call.intent.PhaseGeneration), receipt) != nil {
				return nil, ErrRecordLedgerFence
			}
			response["command_digest"], response["reservation_id"] = digest, id
			return response, nil
		}
		err = ledgerPredecessorReadonly(ctx, call.fence, func(tx pgx.Tx) error {
			preview, found, err := ledgerC80ReadPreview(ctx, tx, call.create.Command)
			if err != nil || !found || preview.ReservationID != id || !preview.ExpiresAt.Equal(expiry) || preview.State != "previewed" || preview.FenceEpoch != 0 {
				return ErrRecordLedgerFence
			}
			return ledgerC80PreviewAbandonable(ctx, tx, call.create.Command, preview)
		})
		if err != nil {
			return nil, err
		}
		if err := call.cutpoint(ctx, "create-commit-before-result", "create-intent"); err != nil {
			return nil, err
		}
		if _, err := ledgerPublishExact(call.fence.control, ledgerPredecessorName("create-result", call.intent.PhaseGeneration), receipt); err != nil {
			return nil, err
		}
		call.preview = &receipt
		if err := call.cutpoint(ctx, "preview-ack-loss", "create-result"); err != nil {
			return nil, err
		}
		response["command_digest"], response["reservation_id"] = digest, id
	case "reserve_intent":
		fields, err := ledgerCallbackFields(body, "op", "phase_generation", "command")
		if err != nil || call.create == nil || call.preview == nil {
			return nil, ErrRecordLedgerFence
		}
		command, digest, err := ledgerC80DecodeReserve(fields["command"])
		if err != nil {
			return nil, err
		}
		if command.CreateCommandDigest != call.create.CommandDigest || command.ReservationID != call.preview.ReservationID || command.DeploymentID != call.config.DeploymentID || command.ActorID != call.config.ActorUserID || command.RequestFingerprint != call.create.Command.RequestFingerprint || command.ExpectedBindingDigest != call.create.Command.BindingDigest || command.ObservedWitnessSequence != call.witness.Sequence || command.ObservedWitnessHash != hexDigest(call.witness.EntryHash) {
			return nil, ErrRecordLedgerFence
		}
		intent := ledgerPredecessorReserveIntent{ledgerPredecessorEffectBinding: call.binding(), Command: command, CommandDigest: digest}
		if call.reserve != nil {
			if *call.reserve != intent || ledgerReadExact(call.fence.control, ledgerPredecessorName("reserve-intent", call.intent.PhaseGeneration), intent) != nil {
				return nil, ErrRecordLedgerFence
			}
		} else {
			err := ledgerPredecessorReadonly(ctx, call.fence, func(tx pgx.Tx) error {
				preview, found, err := ledgerC80ReadPreview(ctx, tx, call.create.Command)
				if err != nil || !found || preview.ReservationID != call.preview.ReservationID {
					return ErrRecordLedgerFence
				}
				return ledgerC80PreviewAbandonable(ctx, tx, call.create.Command, preview)
			})
			if err != nil {
				return nil, err
			}
			if _, err := ledgerPublishExact(call.fence.control, ledgerPredecessorName("reserve-intent", call.intent.PhaseGeneration), intent); err != nil {
				return nil, err
			}
			call.reserve = &intent
			if err := call.cutpoint(ctx, "reserve-intent", "reserve-intent"); err != nil {
				return nil, err
			}
		}
		response["command_digest"] = digest
	case "reserve_result":
		fields, err := ledgerCallbackFields(body, "op", "phase_generation", "command_digest", "operation_id")
		if err != nil || call.reserve == nil {
			return nil, ErrRecordLedgerFence
		}
		var digest, id string
		if ledgerCallbackDecode(fields, "command_digest", &digest) != nil || digest != call.reserve.CommandDigest || ledgerCallbackDecode(fields, "operation_id", &id) != nil {
			return nil, ErrRecordLedgerFence
		}
		err = ledgerPredecessorReadonly(ctx, call.fence, func(tx pgx.Tx) error {
			actual, found, err := ledgerC80ReadOperation(ctx, tx, call.create.Command, call.reserve.Command)
			if err != nil || !found || actual != id {
				return ErrRecordLedgerFence
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		receipt := ledgerPredecessorReserveResult{ledgerPredecessorEffectBinding: call.binding(), CreateCommandDigest: call.create.CommandDigest, CommandDigest: digest, ReservationID: call.preview.ReservationID, OperationID: id}
		if call.committed != nil && *call.committed != receipt {
			return nil, ErrRecordLedgerFence
		}
		if _, err := ledgerPublishExact(call.fence.control, ledgerPredecessorName("reserve-result", call.intent.PhaseGeneration), receipt); err != nil {
			return nil, err
		}
		call.committed = &receipt
		if err := call.cutpoint(ctx, "reserve-commit-before-stdout", "reserve-result"); err != nil {
			return nil, err
		}
		response["command_digest"], response["operation_id"] = digest, id
	default:
		return nil, ErrRecordLedgerFence
	}
	return response, nil
}

func (call *ledgerPredecessorCall) finishOutput(ctx context.Context, body []byte) (bool, error) {
	if call.resultDigest == "" || len(body) == 0 || body[len(body)-1] != '\n' {
		return false, nil
	}
	result, err := ledgerPredecessorParseOutput(body, call.intent.PhaseGeneration)
	if err != nil || ledgerPredecessorHash(body) != call.resultDigest {
		return false, ErrRecordLedgerFence
	}
	switch result.Status {
	case "committed":
		if result.Reason != "app_reserved" || call.committed == nil || result.OperationID != call.committed.OperationID || call.denied {
			return false, ErrRecordLedgerFence
		}
		err := ledgerPredecessorReadonly(ctx, call.fence, func(tx pgx.Tx) error {
			operation, found, err := ledgerC80ReadOperation(ctx, tx, call.create.Command, call.reserve.Command)
			if err != nil || !found || operation != result.OperationID {
				return ErrRecordLedgerFence
			}
			return nil
		})
		if err != nil {
			return false, err
		}
	case "rejected":
		if result.Reason != "source_denied" || result.OperationID != "" || !call.denied || call.create != nil || call.preview != nil || call.reserve != nil || call.committed != nil {
			return false, ErrRecordLedgerFence
		}
		if err := call.unchangedDenied(ctx); err != nil {
			return false, err
		}
	case "unavailable":
		if result.OperationID != "" || (result.Reason != "inventory_unavailable" && result.Reason != "admission_unavailable" && result.Reason != "service_unavailable") {
			return false, ErrRecordLedgerFence
		}
	default:
		return false, ErrRecordLedgerFence
	}
	receipt := ledgerPredecessorWrittenResult{Version: 1, PhaseGeneration: call.intent.PhaseGeneration, RecoveryID: call.intent.RecoveryID, Application: call.intent.Application, RealmDigest: call.intent.RealmDigest, ResultDigest: call.resultDigest, Result: result}
	if call.written != nil && *call.written != receipt {
		return false, ErrRecordLedgerFence
	}
	if err := ledgerPredecessorPublishStdout(call.fence.control, call.intent, call.resultDigest, body); err != nil {
		return false, err
	}
	if _, err := ledgerPublishExact(call.fence.control, ledgerPredecessorName("result-written", call.intent.PhaseGeneration), receipt); err != nil {
		return false, err
	}
	call.written = &receipt
	if err := call.cutpoint(ctx, "stdout-loss", "result-written"); err != nil {
		return false, err
	}
	if err := call.child.channel.send(ctx, map[string]any{"op": "result_written_ack", "phase_generation": call.intent.PhaseGeneration, "result_digest": call.resultDigest}); err != nil {
		return false, err
	}
	return true, nil
}
