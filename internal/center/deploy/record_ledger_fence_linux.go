//go:build linux

package deploy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sys/unix"

	"houfeng/internal/center/deletionledger"
	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recorddeletion"
)

// RecordLedgerFence is a non-transferable maintenance capability. Its zero
// value is invalid; every method checks its live callback scope. Do not copy it.
type RecordLedgerFence struct {
	self                    *RecordLedgerFence
	mu                      sync.Mutex
	condition               *sync.Cond
	closed                  bool
	uses                    int
	work                    sync.Mutex
	sourceWork              sync.Mutex
	sourceEffect            sync.Mutex
	phase                   ledgerMaintenancePhase
	predecessorPending      bool
	callbackDeadline        ledgerMaintenanceDeadline
	maintenanceFault        *ledgerMaintenanceFault
	maintenanceCheckpointer ledgerMaintenanceCheckpointer
	terminalCheckpointer    ledgerMaintenanceCheckpointer
	guardFD                 int
	ctx                     context.Context
	request                 RecordLedgerMaintenanceRequest
	config                  SourceMaintenanceConfig
	journal                 ledgerFenceJournal
	control                 int
	session                 int
	groups                  []ledgerCgroup
	compose                 ComposeOfflineFence
	app                     *pgxpool.Pool
	appPort                 uint16
	appPools                []*pgxpool.Pool
	appSessions             map[int32]ledgerAppSession
	appInventory            ledgerAppInventory
	appCredential           ledgerAdminCredential
	appBootstrapRole        string
	primary                 *ledgerDatabase
	witness                 *ledgerDatabase
	readPrimary             *deletionledger.Primary
	readWitness             *deletionledger.Witness
	writePrimary            *deletionledger.Primary
	writeWitness            *deletionledger.Witness
	binding                 ledgerBinding
	anchors                 []ledgerAnchor
}

var _ recorddeletion.LedgerContinuityGuard = (*RecordLedgerFence)(nil)

func (f *RecordLedgerFence) enter(ctx context.Context) error {
	if f == nil || f.self != f || f.ctx == nil || ctx == nil {
		return ErrRecordLedgerFence
	}
	if err := f.ctx.Err(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed || f.condition == nil || (f.phase.Generation != 0 && f.phase.Phase != ledgerPhaseQuiescent) {
		return ErrRecordLedgerFence
	}
	f.uses++
	return nil
}

// bindContext keeps caller values and cancellation while inheriting the
// invocation deadline and live-root cancellation for the entire operation.
func (f *RecordLedgerFence) bindContext(ctx context.Context) (context.Context, context.CancelFunc) {
	var bound context.Context
	var cancel context.CancelFunc
	if deadline, ok := f.ctx.Deadline(); ok {
		bound, cancel = context.WithDeadline(ctx, deadline)
	} else {
		bound, cancel = context.WithCancel(ctx)
	}
	stop := context.AfterFunc(f.ctx, cancel)
	if f.ctx.Err() != nil {
		cancel()
	}
	return bound, func() { stop(); cancel() }
}

func (f *RecordLedgerFence) leave() {
	f.mu.Lock()
	f.uses--
	if f.uses == 0 {
		f.condition.Broadcast()
	}
	f.mu.Unlock()
}

func (f *RecordLedgerFence) closeScope() {
	f.mu.Lock()
	f.closed = true
	for f.uses != 0 {
		f.condition.Wait()
	}
	pools := append([]*pgxpool.Pool(nil), f.appPools...)
	f.mu.Unlock()
	for _, pool := range pools {
		pool.Close()
	}
}

func ledgerInventoryMatches(project []fenceObservation, ids []string) bool {
	if len(project) != len(ids) || len(ids) == 0 {
		return false
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	for i, item := range project {
		if !fenceID.MatchString(sorted[i]) || sorted[i] != item.anchor.ID || (i > 0 && sorted[i] == sorted[i-1]) {
			return false
		}
	}
	return true
}

// WithRecordLedgerMaintenance records the exact initial participants before
// stopping them. The guard, endpoint isolation, frozen cgroups and PostgreSQL
// producer revocations remain in force for the complete callback. It never
// reopens endpoints or thaws cgroups, including on error or cancellation.
func WithRecordLedgerMaintenance(ctx context.Context, request RecordLedgerMaintenanceRequest, fn func(*RecordLedgerFence) error) error {
	return withRecordLedgerMaintenance(ctx, request, nil, fn)
}

func withRecordLedgerMaintenance(ctx context.Context, request RecordLedgerMaintenanceRequest, predecessor *ledgerPredecessorAdmission, fn func(*RecordLedgerFence) error) error {
	return withRecordLedgerMaintenanceMode(ctx, request, predecessor, nil, fn)
}

func withRecordLedgerMaintenanceMode(ctx context.Context, request RecordLedgerMaintenanceRequest, predecessor *ledgerPredecessorAdmission, migration *ledgerMigrationAdmission, fn func(*RecordLedgerFence) error) error {
	started := time.Now()
	if ctx == nil || fn == nil || os.Geteuid() != 10002 || !fenceRequestValid(request.Compose) || request.Primary.Identity.Validate() != nil || request.Witness.Identity.Validate() != nil ||
		request.Primary.Identity.Kind != "deletion_ledger" || request.Witness.Identity.Kind != "deletion_witness" ||
		request.Primary.Identity.ID == request.Witness.Identity.ID || request.Primary.Identity.SystemIdentifier == request.Witness.Identity.SystemIdentifier ||
		request.AppIdentity.ID == "" || request.AppIdentity.DatabaseOID == 0 || request.AppIdentity.Epoch == 0 ||
		request.AppIdentity.SystemIdentifier == request.Primary.Identity.SystemIdentifier || request.AppIdentity.SystemIdentifier == request.Witness.Identity.SystemIdentifier {
		return ErrRecordLedgerFence
	}
	cfg, err := LoadSourceMaintenanceConfig(request.Compose.ControlConfigPath)
	if err != nil {
		return ErrRecordLedgerFence
	}
	return withMaintenanceGuardFD(ctx, cfg.GuardDirectory, cfg.GuardIdentity, true, func(guardFD int) (result error) {
		current, err := LoadSourceMaintenanceConfig(request.Compose.ControlConfigPath)
		if err != nil || current != cfg || ctx.Err() != nil || fenceDockerBinary() != nil {
			return ErrRecordLedgerFence
		}
		swarm, err := fenceDocker(ctx, "info", "--format", "{{.Swarm.LocalNodeState}}")
		if err != nil || strings.TrimSpace(string(swarm)) != "inactive" {
			return ErrRecordLedgerFence
		}
		groups, err := ledgerOpenCgroups(request.HostCgroups)
		if err != nil {
			return err
		}
		defer ledgerCloseCgroups(groups)
		project, err := fenceInventory(ctx, request.Compose)
		if err != nil || !ledgerInventoryMatches(project, request.ComposeContainerIDs) {
			return ErrRecordLedgerFence
		}
		fenceDir, err := fenceJournalDir(request.Compose.ControlConfigPath)
		if err != nil {
			return ErrRecordLedgerFence
		}
		_, _, err = fenceReadJournal(fenceDir, request.Compose)
		_ = unix.Close(fenceDir)
		if err != nil {
			return ErrRecordLedgerFence
		}
		primary, err := ledgerOpenDomain(ctx, request.Primary)
		if err != nil {
			return err
		}
		defer primary.close()
		witness, err := ledgerOpenDomain(ctx, request.Witness)
		if err != nil {
			return err
		}
		defer witness.close()
		control, err := ledgerControlDir(request.Compose.ControlConfigPath)
		if err != nil {
			return err
		}
		defer unix.Close(control)
		// Open the live root channel only when a completed migration needs it.
		// Migration recovery already owns the invocation's one channel.
		var terminalCheckpointer ledgerMaintenanceCheckpointer
		if migration != nil {
			terminalCheckpointer = migration.checkpointer
		} else {
			candidate, err := ledgerMigrationLoadJournal(control, request.AppIdentity, true)
			if err != nil {
				return err
			}
			if len(candidate.Terminals) != 0 {
				invocation, cancelInvocation := context.WithDeadline(ctx, started.Add(ledgerMaintenanceTotalLimit))
				defer cancelInvocation()
				work, cancel, err := ledgerMaintenanceWorkContext(invocation)
				if err != nil {
					return err
				}
				defer cancel()
				ctx = work
				root, err := ledgerOpenRealm(5)
				if err != nil {
					return ErrRecordLedgerFence
				}
				defer root.close()
				channel := &ledgerMaintenanceChannel{realm: root}
				stop, err := channel.watch(ctx, cancel)
				if err != nil {
					return err
				}
				defer stop()
				terminalCheckpointer = channel
			}
		}
		if _, err := ledgerPublishExact(control, "identity.json", ledgerDomainBinding{Version: 1, Primary: request.Primary.Identity, Witness: request.Witness.Identity}); err != nil {
			return err
		}
		session, err := ledgerSessionDir(control, request.Compose.RecoveryID)
		if err != nil {
			return err
		}
		defer unix.Close(session)
		observed := ledgerJournalRequest(request, cfg, project, groups, primary, witness)
		journal, err := ledgerEnsureJournal(session, observed)
		if err != nil || !ledgerControlMatches(request.Compose.ControlConfigPath, control) || !ledgerSessionMatches(control, session, request.Compose.RecoveryID) {
			return ErrRecordLedgerFence
		}
		if err := ledgerFenceCgroups(ctx, groups); err != nil {
			return err
		}
		var compose ComposeOfflineFence
		if err := fencePerform(ctx, request.Compose, &compose); err != nil {
			return ErrRecordLedgerFence
		}
		if err := primary.fenceWriters(ctx); err != nil {
			return err
		}
		if err := witness.fenceWriters(ctx); err != nil {
			return err
		}
		app, err := ledgerOpenApp(ctx, request.AppBootstrapURL, compose.DatabaseAddress, request.AppIdentity, true, nil, 0, migration != nil)
		if err != nil {
			return err
		}
		defer app.Close()
		appConfig, err := ledgerParsePool(request.AppBootstrapURL)
		if err != nil {
			return err
		}
		if predecessor != nil {
			if err := predecessor.prepare(ctx, app, request, control, session, compose.DatabaseAddress); err != nil {
				return err
			}
		}
		if migration != nil {
			if err := migration.prepare(ctx, app, request, control, session, compose.DatabaseAddress, guardFD, cfg, primary, witness); err != nil {
				return err
			}
		}
		inventory, credential, err := ledgerPrepareApplication(ctx, app, request, appConfig.ConnConfig.User, control, session, compose.DatabaseAddress, terminalCheckpointer)
		if err != nil {
			return err
		}
		var f *RecordLedgerFence
		defer func() {
			// Unresolved predecessor and migration intents retain their exact
			// journal-bound role state for recovery.
			if migration != nil && migration.pending || predecessor != nil && predecessor.pending != nil || f != nil && f.predecessorPending {
				result = errors.Join(result, ErrRecordLedgerFence)
				return
			}
			cleanup, cancel, err := ledgerFenceCleanupContext(ctx, f)
			if err != nil {
				result = errors.Join(result, err)
				return
			}
			defer cancel()
			if err := ledgerDisableAdmin(cleanup, app, request.AppAdminRole); err != nil {
				result = errors.Join(result, err)
			}
		}()
		generation, err := ledgerRecoveryBinding(ctx, app, witness, cfg, request.Compose.RecoveryID)
		if err != nil {
			return ErrRecordLedgerFence
		}
		binding := ledgerBinding{Version: 1, RecoveryID: request.Compose.RecoveryID, Generation: generation}
		if _, err := ledgerPublishExact(session, "binding.json", binding); err != nil {
			return err
		}
		bootstrapSession, err := ledgerCurrentAppSession(ctx, app)
		if err != nil || bootstrapSession.Role != appConfig.ConnConfig.User {
			return ErrRecordLedgerFence
		}
		f = &RecordLedgerFence{ctx: ctx, request: request, config: cfg, journal: journal, control: control, session: session, groups: groups, compose: compose,
			app: app, appPort: 5432, appSessions: map[int32]ledgerAppSession{bootstrapSession.PID: bootstrapSession}, appInventory: inventory, appCredential: credential, appBootstrapRole: appConfig.ConnConfig.User, primary: primary, witness: witness, binding: binding,
			maintenanceFault: ledgerMaintenanceFaultFromContext(ctx), terminalCheckpointer: terminalCheckpointer}
		f.self = f
		f.guardFD = guardFD
		f.condition = sync.NewCond(&f.mu)
		defer f.closeScope()
		if predecessor != nil {
			if err := predecessor.resume(ctx, f); err != nil {
				return err
			}
		}
		if err := f.loadMaintenancePhase(); err != nil {
			return err
		}
		if err := f.verifyExternal(ctx); err != nil {
			return err
		}
		if err := f.loadAnchors(ctx); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return fn(f)
	})
}

func (f *RecordLedgerFence) verifyCompose(ctx context.Context) error {
	project, err := fenceInventory(ctx, f.request.Compose)
	if err != nil || !ledgerInventoryMatches(project, f.request.ComposeContainerIDs) || len(project) != len(f.journal.Containers) {
		return ErrRecordLedgerFence
	}
	for i, item := range project {
		if !fenceSameAnchors(f.journal.Containers[i], item.anchor) || item.anchor.Restart.Name != "no" ||
			!fenceAllowedNetworks(f.journal.Containers[i], item, f.compose.MaintenanceNetworkID, item.anchor.ID == f.request.Compose.DatabaseContainerID) {
			return ErrRecordLedgerFence
		}
		if item.anchor.ID == f.request.Compose.DatabaseContainerID {
			n, ok := fenceDBNetwork(item, f.compose.MaintenanceNetworkID)
			if !ok || len(item.anchor.Networks) != 1 || n.IP != f.compose.DatabaseAddress || item.state != "running" || item.published {
				return ErrRecordLedgerFence
			}
		} else if item.state != "exited" && item.state != "created" {
			return ErrRecordLedgerFence
		}
	}
	network, exists, err := fenceFindNetwork(ctx, f.request.Compose)
	if err != nil || !exists || network.ID != f.compose.MaintenanceNetworkID || len(network.Containers) != 1 || fenceValidateNetworkMembers(network, f.request.Compose.DatabaseContainerID) != nil {
		return ErrRecordLedgerFence
	}
	return nil
}

func (f *RecordLedgerFence) verifyExternal(ctx context.Context) error {
	if err := f.requireQuiescent(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg, err := LoadSourceMaintenanceConfig(f.request.Compose.ControlConfigPath)
	if err != nil || cfg != f.config || !ledgerControlMatches(f.request.Compose.ControlConfigPath, f.control) || !ledgerSessionMatches(f.control, f.session, f.binding.RecoveryID) || ledgerVerifyCgroups(f.groups) != nil || f.verifyCompose(ctx) != nil {
		return ErrRecordLedgerFence
	}
	// Preserve both the root lifetime and the caller's cancellation while
	// waiting for the terminal acknowledgement.
	checkpointCtx, cancelCheckpoint := f.bindContext(ctx)
	defer cancelCheckpoint()
	if ledgerReadExact(f.control, "identity.json", ledgerDomainBinding{Version: 1, Primary: f.request.Primary.Identity, Witness: f.request.Witness.Identity}) != nil ||
		ledgerReadExact(f.session, "session.json", f.journal) != nil || ledgerReadExact(f.session, "binding.json", f.binding) != nil ||
		ledgerMigrationVerifyInventory(checkpointCtx, f.control, f.session, f.request, f.appInventory, f.terminalCheckpointer) != nil || ledgerReadExact(f.session, "management-credential.json", f.appCredential) != nil {
		return ErrRecordLedgerFence
	}
	if f.primary.verifyFenced(ctx) != nil || f.witness.verifyFenced(ctx) != nil || ledgerAppIdentity(ctx, f.app, f.request.AppIdentity) != nil {
		return ErrRecordLedgerFence
	}
	if ledgerCheckAppFenced(ctx, f.app, f.appBootstrapRole, f.appInventory, f.appCredential) != nil {
		return ErrRecordLedgerFence
	}
	f.mu.Lock()
	sessions := make(map[int32]ledgerAppSession, len(f.appSessions))
	for pid, session := range f.appSessions {
		sessions[pid] = session
	}
	f.mu.Unlock()
	if err := ledgerCheckAppSessions(ctx, f.app, f.appInventory, sessions); err != nil {
		return err
	}
	generation, err := ledgerRecoveryBinding(ctx, f.app, f.witness, f.config, f.binding.RecoveryID)
	if err != nil || generation != f.binding.Generation {
		return ErrRecordLedgerFence
	}
	return nil
}

// WithHeldLedgerFence admits nested uses without re-acquiring flock. Scope
// accounting joins every entered use before the outer callback can release it.
func (f *RecordLedgerFence) WithHeldLedgerFence(ctx context.Context, fn func(context.Context) error) error {
	if fn == nil {
		return ErrRecordLedgerFence
	}
	if err := f.enter(ctx); err != nil {
		return err
	}
	defer f.leave()
	scope, cancel := f.bindContext(ctx)
	defer cancel()
	if err := f.verifyExternal(scope); err != nil {
		return err
	}
	if err := fn(scope); err != nil {
		return err
	}
	return f.verifyExternal(scope)
}

func (f *RecordLedgerFence) RecoveryBinding() (string, uint64, error) {
	if f == nil || f.ctx == nil {
		return "", 0, ErrRecordLedgerFence
	}
	if err := f.enter(f.ctx); err != nil {
		return "", 0, err
	}
	defer f.leave()
	if err := f.verifyExternal(f.ctx); err != nil {
		return "", 0, err
	}
	return f.binding.RecoveryID, f.binding.Generation, nil
}

// NeverEnrolled reports the independently checked offline lineage state.
func (f *RecordLedgerFence) NeverEnrolled() (bool, error) {
	if f == nil || f.ctx == nil {
		return false, ErrRecordLedgerFence
	}
	ctx := f.ctx
	if err := f.enter(ctx); err != nil {
		return false, err
	}
	defer f.leave()
	if err := f.verifyExternal(ctx); err != nil {
		return false, err
	}
	return f.binding.Generation == 0, nil
}

func (f *RecordLedgerFence) checkAnchorsLocked() error {
	durable, err := ledgerReadAnchors(f.control)
	if err != nil || len(durable) == 0 || len(durable) != len(f.anchors) {
		return ErrRecordLedgerFence
	}
	for i := range durable {
		if durable[i] != f.anchors[i] {
			return ErrRecordLedgerFence
		}
	}
	return nil
}

func (f *RecordLedgerFence) Trust() (recorddeletion.LedgerVerificationTrust, error) {
	if f == nil || f.ctx == nil {
		return recorddeletion.LedgerVerificationTrust{}, ErrRecordLedgerFence
	}
	if err := f.enter(f.ctx); err != nil {
		return recorddeletion.LedgerVerificationTrust{}, err
	}
	defer f.leave()
	if err := f.verifyExternal(f.ctx); err != nil {
		return recorddeletion.LedgerVerificationTrust{}, err
	}
	f.work.Lock()
	defer f.work.Unlock()
	if f.checkAnchorsLocked() != nil {
		return recorddeletion.LedgerVerificationTrust{}, ErrRecordLedgerFence
	}
	a := f.anchors[len(f.anchors)-1]
	return recorddeletion.LedgerVerificationTrust{Primary: f.request.Primary.Identity, Witness: f.request.Witness.Identity, PrimaryAnchor: a.Primary, WitnessAnchor: a.Witness, Continuity: f}, nil
}

func (f *RecordLedgerFence) VerifyLedgerContinuity(ctx context.Context, observation recorddeletion.LedgerContinuityObservation) error {
	if err := f.enter(ctx); err != nil {
		return err
	}
	defer f.leave()
	ctx, cancel := f.bindContext(ctx)
	defer cancel()
	if err := f.verifyExternal(ctx); err != nil {
		return err
	}
	f.work.Lock()
	defer f.work.Unlock()
	if f.checkAnchorsLocked() != nil {
		return ErrRecordLedgerFence
	}
	a := f.anchors[len(f.anchors)-1]
	if observation.Primary != f.request.Primary.Identity || observation.Witness != f.request.Witness.Identity || observation.PrimaryAnchor != a.Primary || observation.WitnessAnchor != a.Witness || observation.PrimaryHead.Sequence < a.Primary.Sequence || observation.WitnessHead.Sequence < a.Witness.Sequence {
		return ErrRecordLedgerFence
	}
	p, w, err := f.bootstrapReaders()
	if err != nil {
		return err
	}
	primary, witness, err := f.pair(ctx, p, w, a)
	if err != nil || primary.Head != observation.PrimaryHead || witness.Head != observation.WitnessHead || f.validateTail(primary, witness, a) != nil {
		return ErrRecordLedgerFence
	}
	return nil
}

// Preflight checks fresh physical chains and every unanchored intent before a
// coordinator resolves an operation or copies an entry to the witness.
func (f *RecordLedgerFence) Preflight(ctx context.Context) error {
	if err := f.enter(ctx); err != nil {
		return err
	}
	defer f.leave()
	ctx, cancel := f.bindContext(ctx)
	defer cancel()
	if err := f.verifyExternal(ctx); err != nil {
		return err
	}
	f.work.Lock()
	defer f.work.Unlock()
	if f.checkAnchorsLocked() != nil {
		return ErrRecordLedgerFence
	}
	p, w, err := f.bootstrapReaders()
	if err != nil {
		return err
	}
	a := f.anchors[len(f.anchors)-1]
	primary, witness, err := f.pair(ctx, p, w, a)
	if err != nil {
		return err
	}
	return f.validateTail(primary, witness, a)
}

// OpenAdmin creates a direct APP administrator connection pinned to the isolated
// APP endpoint. Bootstrap observes the physical system; admin gets no extra ACL.
func (f *RecordLedgerFence) OpenAdmin(ctx context.Context, url string) (*pgxpool.Pool, error) {
	if err := f.enter(ctx); err != nil {
		return nil, err
	}
	defer f.leave()
	ctx, cancel := f.bindContext(ctx)
	defer cancel()
	if err := f.verifyExternal(ctx); err != nil {
		return nil, err
	}
	if url != f.appCredential.NewURL {
		return nil, ErrRecordLedgerFence
	}
	pool, err := ledgerOpenApp(ctx, url, f.compose.DatabaseAddress, f.request.AppIdentity, false, f.app, f.appPort, false)
	if err != nil {
		return nil, err
	}
	session, err := ledgerCurrentAppSession(ctx, pool)
	if err != nil || session.Role != f.appInventory.Admin {
		pool.Close()
		return nil, ErrRecordLedgerFence
	}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		pool.Close()
		return nil, ErrRecordLedgerFence
	}
	f.appPools = append(f.appPools, pool)
	f.appSessions[session.PID] = session
	f.mu.Unlock()
	return pool, nil
}

func (f *RecordLedgerFence) bootstrapReaders() (*deletionledger.Primary, *deletionledger.Witness, error) {
	primary, err := deletionledger.NewPrimary(f.primary.admin, f.request.Primary.Identity)
	if err != nil {
		return nil, nil, ErrRecordLedgerFence
	}
	witness, err := deletionledger.NewWitness(f.witness.admin, primary, f.request.Witness.Identity)
	if err != nil {
		return nil, nil, ErrRecordLedgerFence
	}
	return primary, witness, nil
}

func (f *RecordLedgerFence) pair(ctx context.Context, p recorddeletion.LedgerSnapshotReader, w recorddeletion.LedgerSnapshotReader, anchor ledgerAnchor) (recorddeletion.LedgerSnapshot, recorddeletion.LedgerSnapshot, error) {
	witness, err := w.ReadLedgerSnapshot(ctx)
	if err != nil {
		return recorddeletion.LedgerSnapshot{}, recorddeletion.LedgerSnapshot{}, ErrRecordLedgerFence
	}
	primary, err := p.ReadLedgerSnapshot(ctx)
	if err != nil {
		return recorddeletion.LedgerSnapshot{}, recorddeletion.LedgerSnapshot{}, ErrRecordLedgerFence
	}
	trust := recorddeletion.LedgerVerificationTrust{Primary: f.request.Primary.Identity, Witness: f.request.Witness.Identity, PrimaryAnchor: anchor.Primary, WitnessAnchor: anchor.Witness, Continuity: f}
	if _, err := recorddeletion.VerifyLedgerSnapshotPair(primary, witness, trust); err != nil {
		return recorddeletion.LedgerSnapshot{}, recorddeletion.LedgerSnapshot{}, ErrRecordLedgerFence
	}
	return primary, witness, nil
}

func (f *RecordLedgerFence) loadAnchors(ctx context.Context) error {
	anchors, err := ledgerReadAnchors(f.control)
	if err != nil {
		return err
	}
	p, w, err := f.bootstrapReaders()
	if err != nil {
		return err
	}
	if len(anchors) == 0 {
		names, err := ledgerNames(f.control)
		if err != nil {
			return err
		}
		for _, name := range names {
			if strings.HasPrefix(name, "intent-") || strings.HasPrefix(name, "done-") || strings.HasPrefix(name, "resolution-") || strings.HasPrefix(name, "source-") {
				return ErrRecordLedgerFence
			}
		}
		primary, witness, err := f.pair(ctx, p, w, ledgerAnchor{})
		if err != nil || primary.Head != (recorddeletion.LedgerHead{}) || witness.Head != (recorddeletion.LedgerHead{}) || len(primary.Entries) != 0 || len(witness.Entries) != 0 {
			return ErrRecordLedgerFence
		}
		anchor, err := ledgerAppendAnchor(f.control, nil, primary.Head, witness.Head)
		if err != nil {
			return err
		}
		anchors = append(anchors, anchor)
	}
	primary, witness, err := f.pair(ctx, p, w, anchors[len(anchors)-1])
	if err != nil {
		return err
	}
	f.anchors = anchors
	if err := f.validateTail(primary, witness, anchors[len(anchors)-1]); err != nil {
		return err
	}
	return nil
}

// ProvisionRecordScopes grants the exact pre-created reader/writer roles only
// after the external fence and genesis/continuity have been established.
func (f *RecordLedgerFence) ProvisionRecordScopes(ctx context.Context) error {
	if err := f.enter(ctx); err != nil {
		return err
	}
	defer f.leave()
	ctx, cancel := f.bindContext(ctx)
	defer cancel()
	if err := f.verifyExternal(ctx); err != nil {
		return err
	}
	f.work.Lock()
	defer f.work.Unlock()
	for _, db := range []*ledgerDatabase{f.primary, f.witness} {
		reader, e1 := ledgerParsePool(db.request.ReaderURL)
		writer, e2 := ledgerParsePool(db.request.WriterURL)
		if e1 != nil || e2 != nil {
			return ErrRecordLedgerFence
		}
		roles := platformmigrate.RecordLedgerScopeRoles{Reader: platformmigrate.SourceRoleCredential{Principal: db.readerRole, Password: reader.ConnConfig.Password}, Writer: platformmigrate.SourceRoleCredential{Principal: db.writerRole, Password: writer.ConnConfig.Password}}
		kind := platformmigrate.DomainKindDeletionLedger
		if db.request.Identity.Kind == "deletion_witness" {
			kind = platformmigrate.DomainKindDeletionWitness
		}
		id, err := platformmigrate.ProvisionRecordLedgerScope(ctx, db.admin, kind, roles)
		if err != nil || id.ID != db.request.Identity.ID || id.PostgresSystemIdentifier != db.request.Identity.SystemIdentifier || id.DatabaseOID != db.request.Identity.DatabaseOID || id.DatabaseName != db.request.Identity.DatabaseName || uint64(id.IdentityEpoch) != db.request.Identity.Epoch {
			return ErrRecordLedgerFence
		}
	}
	return f.verifyExternal(ctx)
}

func (f *RecordLedgerFence) openClients(ctx context.Context) error {
	if f.readPrimary != nil {
		return nil
	}
	if f.primary.openClients(ctx) != nil || f.witness.openClients(ctx) != nil {
		return ErrRecordLedgerFence
	}
	var err error
	f.readPrimary, err = deletionledger.NewPrimary(f.primary.reader, f.request.Primary.Identity)
	if err != nil {
		return ErrRecordLedgerFence
	}
	f.readWitness, err = deletionledger.NewWitness(f.witness.reader, f.readPrimary, f.request.Witness.Identity)
	if err != nil {
		return ErrRecordLedgerFence
	}
	f.writePrimary, err = deletionledger.NewPrimary(f.primary.writer, f.request.Primary.Identity)
	if err != nil {
		return ErrRecordLedgerFence
	}
	f.writeWitness, err = deletionledger.NewWitness(f.witness.writer, f.writePrimary, f.request.Witness.Identity)
	if err != nil {
		return ErrRecordLedgerFence
	}
	return nil
}

func (f *RecordLedgerFence) Readers() (recorddeletion.LedgerSnapshotReader, recorddeletion.LedgerSnapshotReader, error) {
	if f == nil || f.ctx == nil {
		return nil, nil, ErrRecordLedgerFence
	}
	ctx := f.ctx
	if err := f.enter(ctx); err != nil {
		return nil, nil, err
	}
	defer f.leave()
	f.work.Lock()
	defer f.work.Unlock()
	if err := f.verifyExternal(ctx); err != nil {
		return nil, nil, err
	}
	if err := f.openClients(ctx); err != nil {
		return nil, nil, err
	}
	return f.readPrimary, f.readWitness, nil
}

func (f *RecordLedgerFence) Clients() (*deletionledger.Primary, *deletionledger.Witness, error) {
	if f == nil || f.ctx == nil {
		return nil, nil, ErrRecordLedgerFence
	}
	ctx := f.ctx
	if err := f.enter(ctx); err != nil {
		return nil, nil, err
	}
	defer f.leave()
	f.work.Lock()
	defer f.work.Unlock()
	if err := f.verifyExternal(ctx); err != nil {
		return nil, nil, err
	}
	if err := f.openClients(ctx); err != nil {
		return nil, nil, err
	}
	return f.writePrimary, f.writeWitness, nil
}

func ledgerFindIntentEntry(dir int, primary recorddeletion.LedgerSnapshot, intent ledgerIntent, domain [32]byte) (recorddeletion.LedgerChainEntry, bool, error) {
	for _, entry := range primary.Entries {
		if entry.Record == nil || ledgerIntentKey(entry.Record.Request) != intent.Key {
			continue
		}
		canonical, err := ledgerRequestCanonical(entry.Record.Request, domain)
		if err != nil {
			return recorddeletion.LedgerChainEntry{}, false, ErrRecordLedgerFence
		}
		if !bytes.Equal(canonical, intent.Canonical) {
			resolution, exists, err := ledgerReadResolution(dir, intent.Key)
			encoded, marshalErr := json.Marshal(intent)
			if err != nil || !exists || marshalErr != nil || resolution.IntentDigest != sha256.Sum256(encoded) || !bytes.Equal(resolution.Original, intent.Canonical) || !bytes.Equal(resolution.Outcome, canonical) || entry.Sequence != resolution.Primary.Sequence+1 || entry.PreviousHash != resolution.Primary.Hash {
				return recorddeletion.LedgerChainEntry{}, false, ErrRecordLedgerFence
			}
		}
		return entry, true, nil
	}
	return recorddeletion.LedgerChainEntry{}, false, nil
}

func (f *RecordLedgerFence) validateTail(primary, witness recorddeletion.LedgerSnapshot, anchor ledgerAnchor) error {
	intents, err := ledgerReadIntents(f.control)
	if err != nil {
		return err
	}
	domain, err := recorddeletion.LedgerDomainDigest(f.request.Primary.Identity.ID)
	if err != nil {
		return ErrRecordLedgerFence
	}
	sources, err := f.sourceJournalPair(primary, witness, anchor)
	if err != nil {
		return err
	}
	for _, row := range primary.Entries[anchor.Primary.Sequence:] {
		if row.Route == "source_permanent_delete" {
			matched := false
			for _, source := range sources {
				if source.found && source.entry.Sequence == row.Sequence {
					matched = true
					break
				}
			}
			if !matched {
				return ErrRecordLedgerFence
			}
			continue
		}
		matched := false
		for _, intent := range intents {
			entry, found, e := ledgerFindIntentEntry(f.control, primary, intent, domain)
			if e != nil {
				return e
			}
			if found && entry.Sequence == row.Sequence && intent.Primary.Sequence < row.Sequence {
				matched = true
				break
			}
		}
		if !matched {
			return ErrRecordLedgerFence
		}
	}
	return nil
}

// BeginAppend journals the exact request and pre-append anchors before W1 I/O.
// A previous intent is never reissued: its absent result remains unresolved.
func (f *RecordLedgerFence) BeginAppend(ctx context.Context, request recorddeletion.LedgerAppendRequest) (bool, error) {
	if err := f.enter(ctx); err != nil {
		return false, err
	}
	defer f.leave()
	ctx, cancel := f.bindContext(ctx)
	defer cancel()
	if request.EntryType != recorddeletion.LedgerEntryDeleteCommit {
		return false, ErrRecordLedgerFence
	}
	if err := f.verifyExternal(ctx); err != nil {
		return false, err
	}
	if err := f.sourceRequestsSettled(ctx, true); err != nil {
		return false, err
	}
	f.work.Lock()
	defer f.work.Unlock()
	if f.checkAnchorsLocked() != nil {
		return false, ErrRecordLedgerFence
	}
	if err := ledgerCheckProvisionalOperation(ctx, f.app, request); err != nil {
		return false, err
	}
	if err := f.openClients(ctx); err != nil {
		return false, err
	}
	domain, err := recorddeletion.LedgerDomainDigest(f.request.Primary.Identity.ID)
	if err != nil {
		return false, ErrRecordLedgerFence
	}
	canonical, err := ledgerRequestCanonical(request, domain)
	if err != nil {
		return false, ErrRecordLedgerFence
	}
	key := ledgerIntentKey(request)
	intents, err := ledgerReadIntents(f.control)
	if err != nil {
		return false, err
	}
	for _, intent := range intents {
		_, done, err := ledgerDone(f.control, intent)
		if err != nil {
			return false, err
		}
		if intent.Key == key {
			if !bytes.Equal(intent.Canonical, canonical) {
				return false, ErrRecordLedgerFence
			}
			return false, nil
		}
		if !done {
			return false, ErrRecordLedgerFence
		}
	}
	anchor := f.anchors[len(f.anchors)-1]
	primary, witness, err := f.pair(ctx, f.readPrimary, f.readWitness, anchor)
	if err != nil || f.validateTail(primary, witness, anchor) != nil {
		return false, ErrRecordLedgerFence
	}
	for _, row := range primary.Entries {
		if row.Record != nil && ledgerIntentKey(row.Record.Request) == key {
			return false, ErrRecordLedgerFence
		}
	}
	intent := ledgerIntent{Version: 1, Key: key, Canonical: canonical, Primary: primary.Head, Witness: witness.Head}
	fresh, err := ledgerPublishExact(f.control, "intent-"+key+".json", intent)
	if err != nil || !ledgerControlMatches(f.request.Compose.ControlConfigPath, f.control) {
		return false, ErrRecordLedgerFence
	}
	return fresh, nil
}

// Advance rereads both actual ledgers and publishes a monotonic, fsynced anchor.
// Every pending intent must be present exactly and fully witnessed first.
func (f *RecordLedgerFence) Advance(ctx context.Context) error {
	if err := f.enter(ctx); err != nil {
		return err
	}
	defer f.leave()
	ctx, cancel := f.bindContext(ctx)
	defer cancel()
	if err := f.verifyExternal(ctx); err != nil {
		return err
	}
	if err := f.verifyPendingSourceProjections(ctx); err != nil {
		return err
	}
	f.work.Lock()
	defer f.work.Unlock()
	if f.checkAnchorsLocked() != nil {
		return ErrRecordLedgerFence
	}
	if err := f.openClients(ctx); err != nil {
		return err
	}
	anchor := f.anchors[len(f.anchors)-1]
	primary, witness, err := f.pair(ctx, f.readPrimary, f.readWitness, anchor)
	if err != nil || f.validateTail(primary, witness, anchor) != nil {
		return ErrRecordLedgerFence
	}
	intents, err := ledgerReadIntents(f.control)
	if err != nil {
		return err
	}
	domain, err := recorddeletion.LedgerDomainDigest(f.request.Primary.Identity.ID)
	if err != nil {
		return ErrRecordLedgerFence
	}
	type pending struct {
		intent ledgerIntent
		entry  recorddeletion.LedgerChainEntry
	}
	var pendingIntents []pending
	for _, intent := range intents {
		done, exists, err := ledgerDone(f.control, intent)
		if err != nil {
			return err
		}
		row, found, err := ledgerFindIntentEntry(f.control, primary, intent, domain)
		if err != nil || !found || row.Sequence > witness.Head.Sequence {
			return ErrRecordLedgerFence
		}
		if exists {
			if done.Sequence != row.Sequence || done.Hash != row.EntryHash || done.AnchorIndex >= uint64(len(f.anchors)) || f.anchors[done.AnchorIndex].Witness.Sequence < row.Sequence {
				return ErrRecordLedgerFence
			}
		} else {
			pendingIntents = append(pendingIntents, pending{intent, row})
		}
	}
	sources, err := f.prepareSourceAdvance(primary, witness, anchor)
	if err != nil {
		return err
	}
	if primary.Head != anchor.Primary || witness.Head != anchor.Witness {
		next, err := ledgerAppendAnchor(f.control, f.anchors, primary.Head, witness.Head)
		if err != nil {
			return err
		}
		if err := f.sourceAnchorCutpoint(ctx, sources, next); err != nil {
			return err
		}
		f.anchors = append(f.anchors, next)
	}
	for _, item := range pendingIntents {
		data, err := json.Marshal(item.intent)
		if err != nil {
			return ErrRecordLedgerFence
		}
		done := ledgerIntentDone{Version: 1, Key: item.intent.Key, IntentDigest: sha256.Sum256(data), AnchorIndex: uint64(len(f.anchors) - 1), Sequence: item.entry.Sequence, Hash: item.entry.EntryHash}
		if _, err := ledgerPublishExact(f.control, "done-"+item.intent.Key+".json", done); err != nil {
			return err
		}
	}
	if err := f.finishSourceAdvance(sources); err != nil {
		return err
	}
	if !ledgerControlMatches(f.request.Compose.ControlConfigPath, f.control) {
		return ErrRecordLedgerFence
	}
	return f.verifyExternal(ctx)
}
