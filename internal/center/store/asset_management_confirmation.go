package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/assetlinks"
)

func digestAssetManagementReview(objectType, objectID string, state []string, impacts []assetlinks.DependencyImpact) string {
	rows := make([][]string, 0, len(impacts)+1)
	object := make([]string, 0, len(state)+3)
	object = append(object, "object", objectType, objectID)
	object = append(object, state...)
	rows = append(rows, object)
	for _, impact := range impacts {
		rows = append(rows, []string{"dependency", impact.ObjectType, impact.ObjectID, impact.VPSID, impact.VPSLifecycleStatus, impact.RelationType, impact.RelationID, impact.RelationStatus, impact.Classification})
	}
	slices.SortFunc(rows, func(a, b []string) int { return slices.Compare(a, b) })
	// A string-only projection has no unsupported values or custom marshalers.
	payload, _ := json.Marshal(rows)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func requireSharedAssetConfirmation(impacts []assetlinks.DependencyImpact, expectedDigest string, input assetlinks.GlobalActionConfirmation) error {
	provided := strings.TrimSpace(input.PreviewDigest)
	if provided != "" && provided != expectedDigest {
		return fmt.Errorf("%w: management impact graph changed", assetlifecycle.ErrStaleCancellationPreview)
	}
	parent := ""
	for _, impact := range impacts {
		if !impact.RequiresGlobalConfirmation() {
			continue
		}
		if parent == "" {
			parent = impact.VPSID
			continue
		}
		if parent != impact.VPSID && (provided == "" || !input.ConfirmSharedImpact) {
			return assetlifecycle.ErrSharedImpactConfirmationRequired
		}
	}
	return nil
}
