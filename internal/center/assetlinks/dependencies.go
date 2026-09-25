package assetlinks

// DependencyImpact is shared by lifecycle previews and dedicated MI/Target
// management reviews. It lives with relationship contracts to avoid importing
// the lifecycle orchestration package into its own aggregate dependencies.
type DependencyImpact struct {
	ObjectType         string `json:"object_type"`
	ObjectID           string `json:"object_id"`
	VPSID              string `json:"vps_id"`
	VPSLifecycleStatus string `json:"vps_lifecycle_status"`
	RelationType       string `json:"relation_type"`
	RelationID         string `json:"relation_id"`
	RelationStatus     string `json:"relation_status"`
	Classification     string `json:"classification"`
}

type SharedObjectReference struct {
	ObjectType string `json:"object_type"`
	ObjectID   string `json:"object_id"`
}

const (
	DependencyCurrent           = "current"
	DependencyResidual          = "residual"
	DependencyPaused            = "paused"
	DependencyHistorical        = "historical"
	DependencyNeedsConfirmation = "needs_confirmation"
)

// ClassifyDependency separates retained evidence from current carrying facts.
// MI retirement does not implicitly unlink a relationship.
func ClassifyDependency(vpsLifecycle, relationType, relationStatus string) string {
	if vpsLifecycle == "archived" || relationStatus == "unlinked" || relationStatus == "retired" {
		return DependencyHistorical
	}
	if relationType != "monitoring_instance_link" {
		switch relationStatus {
		case "paused":
			return DependencyPaused
		case "active":
		default:
			return DependencyNeedsConfirmation
		}
	}
	if vpsLifecycle == "cancelled" {
		return DependencyResidual
	}
	return DependencyCurrent
}

// RequiresGlobalConfirmation covers all unarchived VPS parents, including
// cancelled residuals, but not stopped or unlinked historical relationships.
func (impact DependencyImpact) RequiresGlobalConfirmation() bool {
	return impact.Classification == DependencyCurrent || impact.Classification == DependencyResidual
}

// RequiresCancellationConfirmation adds relationships whose current state is
// unconfirmed: another VPS may still depend on the shared object.
func (impact DependencyImpact) RequiresCancellationConfirmation() bool {
	return impact.RequiresGlobalConfirmation() || impact.Classification == DependencyNeedsConfirmation
}
