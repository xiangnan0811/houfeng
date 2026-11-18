//go:build linux

package deploy

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"houfeng/internal/center/auth"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
)

type ledgerPredecessorSource struct {
	DeploymentID string `json:"deployment_id"`
	ProjectID    string `json:"project_id"`
	Kind         string `json:"kind"`
	ID           string `json:"id"`
}

type ledgerPredecessorEvent struct {
	Sequence uint64                   `json:"sequence"`
	Op       string                   `json:"op"`
	Source   *ledgerPredecessorSource `json:"source,omitempty"`
	Allowed  *bool                    `json:"allowed,omitempty"`
	Replay   bool                     `json:"replay,omitempty"`
}

type ledgerPredecessorCall struct {
	fence          *RecordLedgerFence
	realm          *ledgerRealmChannel
	observation    ledgerRealmObservation
	intent         ledgerPredecessorIntent
	roles          ledgerPredecessorRoles
	config         ledgerPredecessorConfig
	child          *ledgerPredecessorChild
	backend        ledgerPredecessorBackend
	actorDigest    string
	ready          bool
	denied         bool
	events         []ledgerPredecessorEvent
	create         *ledgerPredecessorCreateIntent
	fault          *ledgerPredecessorFault
	initialEffects ledgerPredecessorEffectCounts
	preview        *ledgerPredecessorCreateResult
	reserve        *ledgerPredecessorReserveIntent
	committed      *ledgerPredecessorReserveResult
	written        *ledgerPredecessorWrittenResult
	resultDigest   string
	witness        recorddeletion.WitnessHead
}

func (call *ledgerPredecessorCall) binding() ledgerPredecessorEffectBinding {
	i := call.intent
	return ledgerPredecessorEffectBinding{Version: 1, PhaseGeneration: i.PhaseGeneration, RecoveryID: i.RecoveryID, Application: i.Application, RealmDigest: i.RealmDigest, Backend: call.backend}
}

func (call *ledgerPredecessorCall) observe(ctx context.Context) error {
	actual, err := call.realm.observe(ctx, call.intent.PhaseGeneration)
	if err != nil {
		return err
	}
	if actual != call.observation {
		return ErrRecordLedgerFence
	}
	return nil
}

func (call *ledgerPredecessorCall) verify(ctx context.Context) error {
	f := call.fence
	phases, err := ledgerReadMaintenancePhases(f.control)
	if err != nil || len(phases) == 0 || phases[len(phases)-1] != f.phase || f.phase.Phase != ledgerPhasePredecessor || f.phase.Generation != call.intent.PhaseGeneration {
		return ErrRecordLedgerFence
	}
	if !ledgerControlMatches(f.request.Compose.ControlConfigPath, f.control) || !ledgerSessionMatches(f.control, f.session, f.request.Compose.RecoveryID) {
		return ErrRecordLedgerFence
	}
	if err := f.verifyCompose(ctx); err != nil {
		return err
	}
	if err := ledgerVerifyCgroups(f.groups); err != nil {
		return err
	}
	if err := f.verifyPredecessorRoles(ctx, call.roles); err != nil {
		return err
	}
	if err := call.child.verify(call.intent); err != nil {
		return err
	}
	return nil
}

func ledgerPredecessorActor(ctx context.Context, f *RecordLedgerFence, project, user string) (string, error) {
	var id, role string
	var disabled *time.Time
	if f.app.QueryRow(ctx, `select user_id,role,disabled_at from public.users where user_id=$1`, user).Scan(&id, &role, &disabled) != nil || id != user || role != auth.RoleAdmin || disabled != nil {
		return "", ErrRecordLedgerFence
	}
	groups, err := store.NewPostgresRecordAuthorizationRepository(f.app).ListActorGroupIDs(ctx, recordauth.ProjectID(project), user)
	if err != nil {
		return "", ErrRecordLedgerFence
	}
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{UserID: user, Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectID(project), GroupIDs: groups})
	if err != nil {
		return "", ErrRecordLedgerFence
	}
	digest := actor.CanonicalHash()
	return hex.EncodeToString(digest[:]), nil
}

func (call *ledgerPredecessorCall) inventory(ctx context.Context) (legacyInventoryObservation, error) {
	unavailable := legacyInventoryObservation{Op: "inventory_observe", PhaseGeneration: call.intent.PhaseGeneration}
	if err := call.observe(ctx); err != nil {
		return unavailable, err
	}
	var result legacyInventoryObservation
	err := ledgerPredecessorReadonly(ctx, call.fence, func(tx pgx.Tx) error {
		var err error
		result, err = ledgerReadC80Inventory(ctx, tx, call.fence.appInventory, call.roles.Normal, call.intent.PhaseGeneration, call.observation, call.fence.appSessions)
		return err
	})
	if err != nil {
		return unavailable, err
	}
	if err := call.observe(ctx); err != nil {
		return unavailable, err
	}
	return result, nil
}

func (call *ledgerPredecessorCall) ledger(ctx context.Context) (recorddeletion.LedgerSnapshot, []ledgerSourcePending, error) {
	f := call.fence
	f.work.Lock()
	defer f.work.Unlock()
	if err := f.checkAnchorsLocked(); err != nil {
		return recorddeletion.LedgerSnapshot{}, nil, err
	}
	p, w, err := f.bootstrapReaders()
	if err != nil {
		return recorddeletion.LedgerSnapshot{}, nil, err
	}
	anchor := f.anchors[len(f.anchors)-1]
	primary, witness, err := f.pair(ctx, p, w, anchor)
	if err != nil {
		return recorddeletion.LedgerSnapshot{}, nil, err
	}
	items, err := f.sourceJournalPair(primary, witness, anchor)
	if err != nil {
		return recorddeletion.LedgerSnapshot{}, nil, err
	}
	for _, item := range items {
		anchored, err := f.sourceAnchored(item)
		if err != nil || !anchored || !item.found || item.entry.Sequence > witness.Head.Sequence {
			return recorddeletion.LedgerSnapshot{}, nil, ErrRecordLedgerFence
		}
		if err := store.ReadLegacySourceRetirementProjection(ctx, f.app, item.preview.Request); err != nil {
			return recorddeletion.LedgerSnapshot{}, nil, ErrRecordLedgerFence
		}
	}
	return witness, items, nil
}

func (call *ledgerPredecessorCall) dispatch(ctx context.Context, body []byte) (any, error) {
	var envelope struct {
		Op    string `json:"op"`
		Phase uint64 `json:"phase_generation"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Phase != call.intent.PhaseGeneration {
		return nil, ErrRecordLedgerFence
	}
	fieldsFor := func(names ...string) (map[string]json.RawMessage, error) {
		return ledgerCallbackFields(body, append([]string{"op", "phase_generation"}, names...)...)
	}
	response := map[string]any{"op": envelope.Op, "phase_generation": envelope.Phase}
	event := ledgerPredecessorEvent{Sequence: uint64(len(call.events) + 1), Op: envelope.Op}
	if len(call.events) >= 2048 {
		return nil, ErrRecordLedgerFence
	}
	if envelope.Op == "ready" {
		fields, err := fieldsFor("backend_pid", "backend_started_at", "role")
		if err != nil || call.ready {
			return nil, ErrRecordLedgerFence
		}
		var backend ledgerPredecessorBackend
		if ledgerCallbackDecode(fields, "backend_pid", &backend.PID) != nil || ledgerCallbackDecode(fields, "backend_started_at", &backend.Started) != nil || ledgerCallbackDecode(fields, "role", &backend.Role) != nil || backend.PID <= 0 || backend.Started.IsZero() || backend.Started.Nanosecond()%1000 != 0 {
			return nil, ErrRecordLedgerFence
		}
		runtime, err := ledgerPredecessorRuntime(call.fence.appInventory)
		if err != nil || backend.Role != runtime {
			return nil, ErrRecordLedgerFence
		}
		var actual ledgerAppSession
		if call.fence.app.QueryRow(ctx, `select pid,backend_start,usename::text from pg_catalog.pg_stat_activity where pid=$1 and backend_type='client backend' and datname=current_database()`, backend.PID).Scan(&actual.PID, &actual.Started, &actual.Role) != nil || actual.PID != backend.PID || actual.Role != backend.Role || !actual.Started.Equal(backend.Started) {
			return nil, ErrRecordLedgerFence
		}
		if _, found := call.fence.appSessions[backend.PID]; found {
			return nil, ErrRecordLedgerFence
		}
		call.backend = backend
		call.fence.appSessions[backend.PID] = actual
		if err := call.verify(ctx); err != nil {
			return nil, err
		}
		if err := call.observe(ctx); err != nil {
			return nil, err
		}
		ready := ledgerPredecessorReady{ledgerPredecessorEffectBinding: call.binding(), Child: call.child.process}
		if _, err := ledgerPublishExact(call.fence.control, ledgerPredecessorName("ready", call.intent.PhaseGeneration), ready); err != nil {
			return nil, err
		}
		call.ready = true
		response["op"] = "ready_ack"
	} else {
		if !call.ready || (call.written != nil && envelope.Op != "result_written") {
			return nil, ErrRecordLedgerFence
		}
		if err := call.verify(ctx); err != nil {
			return nil, err
		}
		switch envelope.Op {
		case "inventory_observe":
			if _, err := fieldsFor(); err != nil {
				return nil, err
			}
			observation, err := call.inventory(ctx)
			if err != nil && !errors.Is(err, errLedgerInventoryUnavailable) {
				return nil, err
			}
			call.events = append(call.events, event)
			return observation, nil
		case "source_check":
			fields, err := fieldsFor("source")
			if err != nil {
				return nil, err
			}
			sourceFields, err := ledgerCallbackFields(fields["source"], "deployment_id", "project_id", "kind", "id")
			if err != nil {
				return nil, err
			}
			var source ledgerPredecessorSource
			if json.Unmarshal(fields["source"], &source) != nil || sourceFields == nil || source.DeploymentID != call.config.DeploymentID || source.ProjectID != call.config.ProjectID || !records.ValidSubjectSourceID(records.SubjectKind(source.Kind), source.ID) {
				return nil, ErrRecordLedgerFence
			}
			_, items, err := call.ledger(ctx)
			if err != nil {
				return nil, err
			}
			allowed := true
			for _, item := range items {
				key := item.preview.Request.Origin.Source
				if string(key.Kind) == source.Kind && key.ID == source.ID {
					allowed = false
				}
			}
			var tombstones int
			if call.fence.app.QueryRow(ctx, `select count(*) from public.source_deletion_tombstones where project_id=$1 and source_kind=$2 and source_id=$3`, source.ProjectID, source.Kind, source.ID).Scan(&tombstones) != nil || tombstones > 1 {
				return nil, ErrRecordLedgerFence
			}
			if tombstones != 0 {
				allowed = false
			}
			call.denied = call.denied || !allowed
			response["allowed"] = allowed
			event.Source = &source
			event.Allowed = &allowed
		case "witness_head", "witness_extension":
			names := []string{}
			if envelope.Op == "witness_extension" {
				names = []string{"sequence", "hash"}
			}
			fields, err := fieldsFor(names...)
			if err != nil {
				return nil, err
			}
			witness, _, err := call.ledger(ctx)
			if err != nil {
				return nil, err
			}
			head := recorddeletion.WitnessHead{Sequence: witness.Head.Sequence, EntryHash: witness.Head.Hash}
			if head.Validate() != nil {
				return nil, ErrRecordLedgerFence
			}
			response["sequence"], response["hash"], response["healthy"] = head.Sequence, hex.EncodeToString(head.EntryHash[:]), true
			if envelope.Op == "witness_extension" {
				var sequence uint64
				hash, err := ledgerCallbackDigest(fields, "hash")
				if err != nil || ledgerCallbackDecode(fields, "sequence", &sequence) != nil || sequence == 0 {
					return nil, ErrRecordLedgerFence
				}
				accepted := false
				for _, row := range witness.Entries {
					if row.Sequence == sequence && row.EntryHash == hash {
						accepted = true
					}
				}
				response["accepted"] = accepted
				if !accepted {
					return nil, ErrRecordLedgerFence
				}
			}
			call.witness = head
		case "preview_intent", "preview_result", "reserve_intent", "reserve_result":
			event.Replay = envelope.Op == "preview_intent" && call.create != nil || envelope.Op == "preview_result" && call.preview != nil || envelope.Op == "reserve_intent" && call.reserve != nil || envelope.Op == "reserve_result" && call.committed != nil
			result, err := call.effect(ctx, envelope.Op, body)
			if err != nil {
				return nil, err
			}
			call.events = append(call.events, event)
			return result, nil
		case "result_written":
			fields, err := fieldsFor("result_digest")
			if err != nil {
				return nil, err
			}
			digest, err := ledgerCallbackDigest(fields, "result_digest")
			if err != nil {
				return nil, err
			}
			value := hex.EncodeToString(digest[:])
			if call.resultDigest != "" && call.resultDigest != value {
				return nil, ErrRecordLedgerFence
			}
			event.Replay = call.resultDigest != ""
			if call.written != nil {
				carrier, original, found, err := ledgerPredecessorReadStdout(call.fence.control, call.intent)
				if err != nil || !found || carrier.ResultDigest != value || original != call.written.Result {
					return nil, ErrRecordLedgerFence
				}
				if value != call.written.ResultDigest || ledgerReadExact(call.fence.control, ledgerPredecessorName("result-written", call.intent.PhaseGeneration), *call.written) != nil {
					return nil, ErrRecordLedgerFence
				}
				call.events = append(call.events, event)
				return map[string]any{"op": "result_written_ack", "phase_generation": call.intent.PhaseGeneration, "result_digest": value}, nil
			}
			call.resultDigest = value
			// stdout may still be queued on its separate pipe. The event loop finishes
			// the durable comparison and ACK after receiving the complete original line.
			call.events = append(call.events, event)
			return nil, nil
		default:
			return nil, ErrRecordLedgerFence
		}
	}
	call.events = append(call.events, event)
	return response, nil
}
