//go:build linux

package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"time"

	"golang.org/x/sys/unix"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

// RunLegacyC80Service executes only the pinned, one-shot maintenance caller.
// A live quiescent fence and root-supervised no-copy realm are mandatory.
func (f *RecordLedgerFence) RunLegacyC80Service(ctx context.Context, request LegacyC80ServiceRequest) (LegacyC80ServiceResult, error) {
	ctx, cancel, err := ledgerMaintenanceWorkContext(ctx)
	if err != nil {
		return LegacyC80ServiceResult{}, err
	}
	defer cancel()
	ctx, boundCancel, err := f.bindMaintenanceWork(ctx)
	if err != nil {
		return LegacyC80ServiceResult{}, err
	}
	defer boundCancel()
	realm, err := ledgerOpenRealm(request.RealmFD)
	if err != nil {
		return LegacyC80ServiceResult{}, err
	}
	defer realm.close()
	return f.runLegacyC80Service(ctx, request, realm, nil)
}

func (f *RecordLedgerFence) runLegacyC80Service(ctx context.Context, request LegacyC80ServiceRequest, realm *ledgerRealmChannel, fault *ledgerPredecessorFault) (result LegacyC80ServiceResult, resultErr error) {
	if ctx == nil || recordauth.ValidateActorUserID(request.ActorUserID) != nil || !ledgerC80ID(request.RecordID, "rec_") || os.Geteuid() != 10002 || os.Getegid() != 10002 {
		return result, ErrRecordLedgerFence
	}
	ctx, cancel, err := ledgerMaintenanceWorkContext(ctx)
	if err != nil {
		return result, err
	}
	defer cancel()
	ctx, boundCancel, err := f.bindMaintenanceWork(ctx)
	if err != nil {
		return result, err
	}
	defer boundCancel()
	if err := f.enter(ctx); err != nil {
		return result, err
	}
	defer f.leave()
	f.sourceWork.Lock()
	defer f.sourceWork.Unlock()
	if f.predecessorPending {
		return result, ErrRecordLedgerFence
	}
	if err := f.verifyExternal(ctx); err != nil {
		return result, err
	}
	if !f.appInventory.LegacyC80 || len(f.appSessions) != 1 {
		return result, ErrRecordLedgerFence
	}
	if err := f.sourceRequestsSettled(ctx, false); err != nil {
		return result, err
	}
	observation, err := realm.observe(ctx, f.phase.Generation)
	if err != nil {
		return result, err
	}
	coordinator, err := ledgerPredecessorSelf()
	if err != nil {
		return result, err
	}
	launcherHash, err := ledgerPredecessorProgram(ledgerPredecessorLauncher, 04750)
	if err != nil {
		return result, err
	}
	predecessorHash, err := ledgerPredecessorProgram(ledgerPredecessorPath, 0550)
	if err != nil {
		return result, err
	}
	group, err := ledgerPredecessorGroupIdentity()
	if err != nil || ledgerPredecessorGroupEmpty(group) != nil {
		return result, ErrRecordLedgerFence
	}
	var guard unix.Stat_t
	if unix.Fstat(f.guardFD, &guard) != nil || guard.Mode&unix.S_IFMT != unix.S_IFREG {
		return result, ErrRecordLedgerFence
	}
	var deployment, project string
	if f.app.QueryRow(ctx, `select deployment_id,project_id from public.deployment_contract_state where project_id='default' and activation_sequence=1 and active_profile in ('postgres_sync','s3_worm') and minimum_fence_contract_version>0`).Scan(&deployment, &project) != nil || recordplatform.ValidateDeploymentID(recordplatform.DeploymentID(deployment)) != nil {
		return result, ErrRecordLedgerFence
	}
	call := &ledgerPredecessorCall{fence: f, realm: realm, observation: observation, fault: fault, events: []ledgerPredecessorEvent{}, config: ledgerPredecessorConfig{Version: 1, PhaseGeneration: f.phase.Generation + 1, DeploymentID: deployment, ProjectID: project, ActorUserID: request.ActorUserID, RecordID: request.RecordID}}
	call.actorDigest, err = ledgerPredecessorActor(ctx, f, project, request.ActorUserID)
	if err != nil {
		return result, err
	}
	call.initialEffects, err = call.effectCounts(ctx)
	if err != nil || !call.initialEffects.Record {
		return result, ErrRecordLedgerFence
	}
	// A fresh service callback never takes over an existing target operation.
	if call.initialEffects.Operations != 0 {
		return result, ErrRecordLedgerFence
	}
	call.roles, call.config.RuntimeURL, err = f.newPredecessorRoles(call.config.PhaseGeneration, hexDigest(observation.Realm))
	if err != nil {
		return result, err
	}
	configBody, err := json.Marshal(call.config)
	if err != nil {
		return result, ErrRecordLedgerFence
	}
	configFile, err := ledgerPredecessorMemfd(configBody)
	clear(configBody)
	call.config.RuntimeURL = ""
	if err != nil {
		return result, err
	}
	defer configFile.Close()
	configHash, err := ledgerPredecessorConfigDigest(configFile)
	if err != nil {
		return result, err
	}
	call.intent = ledgerPredecessorIntent{Version: 1, PhaseGeneration: call.config.PhaseGeneration, RecoveryID: f.request.Compose.RecoveryID, Application: f.request.AppIdentity, RealmDigest: hexDigest(observation.Realm), Coordinator: coordinator, Guard: ledgerPredecessorGuard{Dev: guard.Dev, Ino: guard.Ino}, ConfigFD: int(configFile.Fd()), ConfigDigest: configHash, ActorUserID: request.ActorUserID, RecordID: request.RecordID, ChildCgroup: group, LauncherSHA256: launcherHash, PredecessorSHA256: predecessorHash}
	if ledgerPredecessorValidateRoles(call.roles, f.appInventory, call.intent) != nil {
		return result, ErrRecordLedgerFence
	}
	if _, err := ledgerPublishExact(f.control, ledgerPredecessorName("callback-intent", call.intent.PhaseGeneration), call.intent); err != nil {
		return result, err
	}
	f.predecessorPending = true
	transitioned := false
	defer func() {
		if !transitioned {
			return
		}
		cleanupCtx, cleanupCancel, err := ledgerMaintenanceCallbackCleanupContext(ctx)
		if err != nil {
			result = LegacyC80ServiceResult{}
			resultErr = errors.Join(resultErr, err)
			if call.child != nil {
				call.child.close()
			}
			return
		}
		defer cleanupCancel()
		cleanupErr := call.refence(cleanupCtx)
		if call.child != nil {
			call.child.close()
		}
		if cleanupErr != nil {
			result = LegacyC80ServiceResult{}
			resultErr = errors.Join(resultErr, cleanupErr)
			return
		}
		if resultErr == nil {
			if cleanupCtx.Err() != nil {
				result = LegacyC80ServiceResult{}
				resultErr = ErrRecordLedgerFence
				return
			}
			if err := call.publishCompletion(); err != nil {
				result = LegacyC80ServiceResult{}
				resultErr = err
				return
			}
			f.predecessorPending = false
		}
	}()
	// The callback intent precedes both the role transaction and phase switch.
	// Recovery accepts only these two exact catalog states under this intent.
	transitioned = true
	if err := ledgerPredecessorApplyRoles(ctx, f.app, f.appInventory, ledgerAppExpectedRoles(f.appInventory, ledgerAppTarget), call.roles.Normal); err != nil {
		return result, err
	}
	if err := f.transitionMaintenancePhase(ledgerPhasePredecessor); err != nil {
		return result, err
	}
	if err := call.observe(ctx); err != nil {
		return result, err
	}
	var membership bool
	if f.app.QueryRow(ctx, `select exists(select 1 from public.deployment_membership m join public.deployment_contract_state c on c.project_id=m.project_id and c.deployment_id=m.deployment_id where m.instance_id='compose-center' and m.deployment_id=$1 and m.project_id=$2 and m.instance_kind='api' and m.capability='records.runtime' and m.load_balancer_admitted and m.deployment_epoch>0 and m.fence_contract_version>=c.minimum_fence_contract_version and c.minimum_fence_contract_version>0 and m.heartbeat_expires_at>=clock_timestamp()+interval '60 seconds')`, deployment, project).Scan(&membership) != nil || !membership {
		return result, ErrRecordLedgerFence
	}
	call.child, err = ledgerLaunchPredecessor(ctx, f.guardFD, configFile)
	if err != nil {
		return result, err
	}
	if err := call.run(ctx); err != nil {
		return result, err
	}
	return LegacyC80ServiceResult{Status: call.written.Result.Status, OperationID: call.written.Result.OperationID, Reason: call.written.Result.Reason}, nil
}

func ledgerPredecessorConfigDigest(file *os.File) (string, error) {
	body, err := io.ReadAll(io.LimitReader(file, ledgerCallbackFrameLimit+1))
	if err != nil || len(body) == 0 || len(body) > ledgerCallbackFrameLimit {
		return "", ErrRecordLedgerFence
	}
	defer clear(body)
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", ErrRecordLedgerFence
	}
	return ledgerPredecessorHash(body), nil
}

func (call *ledgerPredecessorCall) run(ctx context.Context) error {
	stdout := make([]byte, 0, 1024)
	eofs := map[string]bool{}
	acknowledged := false
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	wait := call.child.waited
	for {
		if acknowledged && len(eofs) == 3 && call.child.waitSeen {
			if call.child.waitError != nil || ledgerHeldProcessLive(call.child.pidfd) || call.written == nil {
				return ErrRecordLedgerFence
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ErrRecordLedgerFence
		case err := <-wait:
			call.child.waitSeen = true
			call.child.waitError = err
			wait = nil
			if err != nil {
				return ErrRecordLedgerFence
			}
		case <-ticker.C:
			if call.ready && !acknowledged {
				if err := call.verify(ctx); err != nil {
					return err
				}
			}
		case message := <-call.child.output:
			if eofs[message.Stream] {
				return ErrRecordLedgerFence
			}
			if message.Err != nil {
				if !errors.Is(message.Err, io.EOF) {
					return ErrRecordLedgerFence
				}
				if message.Stream == "ipc" && !acknowledged {
					return ErrRecordLedgerFence
				}
				eofs[message.Stream] = true
			}
			if len(message.Body) > 0 {
				switch message.Stream {
				case "stderr":
					return ErrRecordLedgerFence
				case "stdout":
					if acknowledged || len(stdout)+len(message.Body) > ledgerCallbackFrameLimit {
						return ErrRecordLedgerFence
					}
					stdout = append(stdout, message.Body...)
					if bytes.Count(stdout, []byte{'\n'}) > 1 || (bytes.Contains(stdout, []byte{'\n'}) && stdout[len(stdout)-1] != '\n') {
						return ErrRecordLedgerFence
					}
				case "ipc":
					response, err := call.dispatch(ctx, message.Body)
					if err != nil {
						return err
					}
					if response != nil {
						if err := call.child.channel.send(ctx, response); err != nil {
							return err
						}
					}
				default:
					return ErrRecordLedgerFence
				}
			}
			if !acknowledged && call.resultDigest != "" {
				complete, err := call.finishOutput(ctx, stdout)
				if err != nil {
					return err
				}
				acknowledged = complete
			}
		}
	}
}

func (call *ledgerPredecessorCall) refence(ctx context.Context) error {
	if ctx == nil || ctx.Err() != nil {
		return ErrRecordLedgerFence
	}
	f := call.fence
	if f.phase.Phase == ledgerPhaseQuiescent {
		if f.phase.Generation+1 != call.intent.PhaseGeneration {
			return ErrRecordLedgerFence
		}
		if err := f.transitionMaintenancePhase(ledgerPhasePredecessor); err != nil {
			return err
		}
	}
	if f.phase.Phase == ledgerPhasePredecessor {
		if err := f.transitionMaintenancePhase(ledgerPhaseRefencing); err != nil {
			return err
		}
	} else if f.phase.Phase != ledgerPhaseRefencing {
		return ErrRecordLedgerFence
	}
	actual, err := ledgerReadAppRoleSnapshot(ctx, f.app, ledgerAppRoleNames(f.appInventory))
	if err != nil {
		return err
	}
	target := ledgerAppExpectedRoles(f.appInventory, ledgerAppTarget)
	if !reflect.DeepEqual(actual, target) {
		if !reflect.DeepEqual(actual, call.roles.Normal) {
			return ErrRecordLedgerFence
		}
		if err := ledgerPredecessorApplyRoles(ctx, f.app, f.appInventory, call.roles.Normal, target); err != nil {
			return err
		}
	}
	if err := ledgerPredecessorJoin(ctx, call.child); err != nil {
		return err
	}
	if call.backend.PID != 0 {
		delete(f.appSessions, call.backend.PID)
	}
	if ledgerPredecessorGroupEmpty(call.intent.ChildCgroup) != nil {
		return ErrRecordLedgerFence
	}
	if err := ledgerCheckAppSessions(ctx, f.app, f.appInventory, f.appSessions); err != nil {
		return err
	}
	if err := ledgerCheckAppFenced(ctx, f.app, f.appBootstrapRole, f.appInventory, f.appCredential); err != nil {
		return err
	}
	// Root's fresh refencing observation joins its original held child and checks
	// every effect it previously witnessed. A new process cannot replace it.
	observation, err := call.realm.observe(ctx, f.phase.Generation)
	if err != nil || observation != call.observation {
		return ErrRecordLedgerFence
	}
	if ctx.Err() != nil {
		return ErrRecordLedgerFence
	}
	if err := f.transitionMaintenancePhase(ledgerPhaseQuiescent); err != nil {
		return err
	}
	return f.verifyExternal(ctx)
}

func (call *ledgerPredecessorCall) publishCompletion() error {
	if call.child == nil || !call.child.waitSeen || call.child.waitError != nil || call.written == nil || call.fence.phase.Phase != ledgerPhaseQuiescent {
		return ErrRecordLedgerFence
	}
	receipt := ledgerPredecessorCompletion{Version: 1, PhaseGeneration: call.intent.PhaseGeneration, FinalPhaseGeneration: call.fence.phase.Generation, Application: call.intent.Application, RecoveryID: call.intent.RecoveryID, ActorUserID: call.intent.ActorUserID, RecordID: call.intent.RecordID, Coordinator: call.intent.Coordinator, Child: call.child.process, Backend: call.backend, Events: call.events, ChildResult: call.written.Result}
	receipt.Exit.PidfdExited = true
	_, err := ledgerPublishExact(call.fence.control, ledgerPredecessorName("callback-result", call.intent.PhaseGeneration), receipt)
	return err
}
