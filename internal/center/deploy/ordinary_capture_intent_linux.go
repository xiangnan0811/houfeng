//go:build linux

package deploy

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"net"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
)

func ordinaryOriginalObserver(ctx context.Context, request ordinaryUpgradeRequest, journal fenceJournal, password string) (*pgx.Conn, ordinaryPhysicalDatabase, error) {
	var database fenceContainer
	for _, container := range journal.Containers {
		if container.ID == request.DatabaseContainerID && container.Service == "db" {
			database = container
			break
		}
	}
	observed, err := fenceInspect(ctx, request.DatabaseContainerID)
	if err != nil || database.ID == "" || observed.state != "running" || !fenceSameAnchors(database, observed.anchor) || !fenceAllowedNetworks(database, observed, "", false) {
		return nil, ordinaryPhysicalDatabase{}, errOrdinaryUpgrade
	}
	for _, network := range database.Networks {
		endpoint, ok := fenceDBNetwork(observed, network.ID)
		if !ok || net.ParseIP(endpoint.IP) == nil {
			continue
		}
		conn, physical, err := ordinaryOpenControlObserver(ctx, endpoint.IP, password, request.OperationID)
		if err == nil {
			return conn, physical, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, ordinaryPhysicalDatabase{}, errOrdinaryUpgrade
}

// Capture a complete predecessor and target while holding the exclusive guard.
// No raw credential or private authority material is put into the intent.
func ordinaryCaptureUpgradeIntent(ctx context.Context, request ordinaryUpgradeRequest, dirs *ordinaryOperationDirectories, image ordinaryImageTarget, identities []ordinarySecretIdentity, passwords ComposeInitPasswords, commitment string) (ordinaryUpgradeIntent, error) {
	var intent ordinaryUpgradeIntent
	requestBody, err := json.Marshal(request)
	if err != nil || dirs == nil || len(identities) != len(ordinarySecretNames) {
		return intent, ordinaryUpgradePhase("predecessor-capture", errOrdinaryUpgrade)
	}
	fenceRequest := ComposeOfflineFenceRequest{RecoveryID: request.OperationID, Project: request.Project, WorkingDirectory: request.WorkingDirectory, DatabaseContainerID: request.DatabaseContainerID, ControlConfigPath: dirs.fenceConfigPath}
	journal, err := ordinaryCaptureFenceIntent(ctx, fenceRequest)
	if err != nil {
		return intent, ordinaryUpgradePhase("fence-inventory", err)
	}
	centerID, _, err := ordinaryCaptureSessionHMAC(ctx, request, journal)
	if err != nil {
		return intent, ordinaryUpgradePhase("session-witness", err)
	}
	observer, physical, err := ordinaryOriginalObserver(ctx, request, journal, passwords.Bootstrap)
	if err != nil {
		return intent, ordinaryUpgradePhase("predecessor-observer", err)
	}
	defer ordinaryCloseControlObserver(observer)
	class, err := ordinaryObservePredecessorClass(ctx, observer)
	if err != nil {
		return intent, ordinaryUpgradePhase("predecessor-catalog", err)
	}
	state, predecessor, err := ordinaryCapturePredecessorAuthority(filepath.Join(request.WorkingDirectory, "data", "records-authority"))
	if err != nil {
		return intent, ordinaryUpgradePhase("predecessor-authority", err)
	}
	predecessor.CatalogClass = class
	if err := ordinaryCaptureLaunchPredecessor(ctx, observer, request.WorkingDirectory, &predecessor); err != nil {
		return intent, ordinaryUpgradePhase("launch-predecessor", err)
	}
	if err := observer.Close(ctx); err != nil {
		return intent, ordinaryUpgradePhase("predecessor-observer-close", errOrdinaryUpgrade)
	}
	pair, err := ordinaryBuildTargetLaunch(state, image)
	if err != nil || state.MembershipRotation == nil {
		return intent, ordinaryUpgradePhase("target-launch", errOrdinaryUpgrade)
	}
	activation, err1 := state.ActivationCommand.MarshalBinary()
	rotation, err2 := state.MembershipRotation.MarshalBinary()
	contract, err3 := json.Marshal(image.Contract)
	recipe, err4 := ordinaryCaptureComposeRecipe(request.WorkingDirectory)
	override, err5 := ordinaryComposeOverride(request)
	if err1 != nil || err2 != nil || err3 != nil {
		return intent, ordinaryUpgradePhase("target-manifest", errOrdinaryUpgrade)
	}
	if err4 != nil || err5 != nil {
		return intent, ordinaryUpgradePhase("compose-recipe", errOrdinaryUpgrade)
	}
	contract = append(contract, '\n')
	recipe.OverrideSHA256 = ordinaryDigestHex(override)
	intent = ordinaryUpgradeIntent{
		Version: 3, Request: request, RequestSHA256: ordinaryDigestHex(append(requestBody, '\n')), Secrets: identities,
		OldCenterContainerID: centerID, SecretCommitmentSHA256: commitment, Recipe: recipe, Physical: physical,
		Predecessor: predecessor, Fence: journal,
		Target: ordinaryTargetIntent{AdminSHA256: hex.EncodeToString(image.AdminSHA256[:]), CenterSHA256: hex.EncodeToString(image.CenterSHA256[:]), ProcessorSHA256: hex.EncodeToString(image.ProcessorSHA256[:]), ContractSHA256: ordinaryDigestHex(contract), ContractWire: contract, C81Identity: image.Contract.SourceIdentitySHA256, ActivationCommand: activation, RotationCommand: rotation, CenterLaunch: pair.centerBytes, ProcessorLaunch: pair.processorBytes},
	}
	if err := ordinaryStageComposeOverride(dirs.dirs[len(dirs.dirs)-1].fd, request, recipe.OverrideSHA256); err != nil {
		return ordinaryUpgradeIntent{}, ordinaryUpgradePhase("compose-recipe", err)
	}
	policyCtx, cancelPolicy := context.WithTimeout(ctx, 30*time.Second)
	policy, namespaceRecipe, err := ordinaryCaptureDatabaseRestart(policyCtx, request, recipe, commitment)
	cancelPolicy()
	database, found := ordinaryIntentDatabase(intent)
	if err != nil || !found || database.Restart != policy {
		return ordinaryUpgradeIntent{}, ordinaryUpgradePhase("database-restart-recipe", errOrdinaryUpgrade)
	}
	baseline, err := ordinaryCaptureNamespaceBaseline(namespaceRecipe)
	if err != nil {
		return ordinaryUpgradeIntent{}, ordinaryUpgradePhase("namespace-preflight", err)
	}
	namespaceCtx, cancelNamespace := context.WithTimeout(ctx, 30*time.Second)
	namespaceObserver, namespacePhysical, err := ordinaryOriginalObserver(namespaceCtx, request, journal, passwords.Bootstrap)
	if err == nil && ordinarySamePhysical(namespacePhysical, physical) {
		err = ordinaryNamespacePreflight(namespaceCtx, namespaceRecipe, namespaceObserver, physical, class)
	} else {
		err = errOrdinaryUpgrade
	}
	if namespaceObserver != nil {
		closeCtx, cancelClose := context.WithTimeout(context.Background(), 2*time.Second)
		closeErr := namespaceObserver.Close(closeCtx)
		cancelClose()
		if err == nil && closeErr != nil {
			err = closeErr
		}
	}
	cancelNamespace()
	if err != nil {
		return ordinaryUpgradeIntent{}, ordinaryUpgradePhase("namespace-preflight", err)
	}
	intent.Namespace, err = ordinaryCaptureNamespaceBaseline(namespaceRecipe)
	if err != nil || intent.Namespace != baseline {
		return ordinaryUpgradeIntent{}, ordinaryUpgradePhase("namespace-preflight", errOrdinaryUpgrade)
	}
	if ordinaryValidateIntent(intent) != nil {
		return ordinaryUpgradeIntent{}, ordinaryUpgradePhase("intent", errOrdinaryUpgrade)
	}
	return intent, nil
}

func ordinaryImageMatchesIntent(image ordinaryImageTarget, intent ordinaryUpgradeIntent) bool {
	contract, err := json.Marshal(image.Contract)
	if err != nil {
		return false
	}
	contract = append(contract, '\n')
	return image.ImageID == intent.Request.TargetImageID && hex.EncodeToString(image.AdminSHA256[:]) == intent.Target.AdminSHA256 && hex.EncodeToString(image.CenterSHA256[:]) == intent.Target.CenterSHA256 && hex.EncodeToString(image.ProcessorSHA256[:]) == intent.Target.ProcessorSHA256 && bytes.Equal(contract, intent.Target.ContractWire)
}

func ordinarySamePhysical(a, b ordinaryPhysicalDatabase) bool {
	return a.SystemIdentifier == b.SystemIdentifier && a.DatabaseOID == b.DatabaseOID && a.PostmasterStarted.Equal(b.PostmasterStarted)
}
