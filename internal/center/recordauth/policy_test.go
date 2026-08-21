package recordauth

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
)

const (
	testActorUserID = "usr_0123456789abcdef01234567"
	testGroupAlpha  = "rag_alpha"
	testGroupBeta   = "rag_beta"
	testVPSID       = "vps_0123456789abcdef"
)

func TestRecordAuthNormalizeActorScopeCanonicalizesGroupsAndDefensivelyCopies(t *testing.T) {
	input := ActorScope{
		UserID:    testActorUserID,
		Role:      RoleViewer,
		ProjectID: ProjectIDDefault,
		GroupIDs:  []string{testGroupBeta, testGroupAlpha, testGroupBeta},
	}

	got, err := NormalizeActorScope(input)
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	if want := []string{testGroupAlpha, testGroupBeta}; !slices.Equal(got.GroupIDs, want) {
		t.Fatalf("GroupIDs = %#v, want %#v", got.GroupIDs, want)
	}

	input.GroupIDs[0] = "rag_mutated"
	if want := []string{testGroupAlpha, testGroupBeta}; !slices.Equal(got.GroupIDs, want) {
		t.Fatalf("normalized GroupIDs changed through input mutation: %#v, want %#v", got.GroupIDs, want)
	}

	reordered, err := NormalizeActorScope(ActorScope{
		UserID:    testActorUserID,
		Role:      RoleViewer,
		ProjectID: ProjectIDDefault,
		GroupIDs:  []string{testGroupAlpha, testGroupBeta},
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope(reordered) error = %v", err)
	}
	if !bytes.Equal(got.CanonicalBytes(), reordered.CanonicalBytes()) {
		t.Fatalf("canonical bytes differ for equivalent input: %x != %x", got.CanonicalBytes(), reordered.CanonicalBytes())
	}
	if got.CanonicalHash() != reordered.CanonicalHash() {
		t.Fatalf("canonical hashes differ for equivalent input: %x != %x", got.CanonicalHash(), reordered.CanonicalHash())
	}
}

func TestRecordAuthNormalizeActorScopeRejectsUnknownOrMalformedInputs(t *testing.T) {
	tests := []struct {
		name  string
		actor ActorScope
	}{
		{
			name:  "empty user",
			actor: ActorScope{Role: RoleViewer, ProjectID: ProjectIDDefault},
		},
		{
			name:  "unknown role",
			actor: ActorScope{UserID: testActorUserID, Role: Role("viewer "), ProjectID: ProjectIDDefault},
		},
		{
			name:  "unknown project",
			actor: ActorScope{UserID: testActorUserID, Role: RoleViewer, ProjectID: ProjectID("DEFAULT")},
		},
		{
			name:  "uppercase user identifier",
			actor: ActorScope{UserID: "USR_0123456789abcdef01234567", Role: RoleViewer, ProjectID: ProjectIDDefault},
		},
		{
			name:  "malformed group identifier",
			actor: ActorScope{UserID: testActorUserID, Role: RoleViewer, ProjectID: ProjectIDDefault, GroupIDs: []string{"RAG_alpha"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NormalizeActorScope(tt.actor); err == nil {
				t.Fatal("NormalizeActorScope() error = nil, want failure")
			}
		})
	}
}

func TestRecordAuthNormalizeVisibilityScopeKeepsProjectAndEmptyRestrictedDistinct(t *testing.T) {
	project := mustVisibility(t, VisibilityKindProject, nil, nil)
	restricted := mustVisibility(t, VisibilityKindRestricted, nil, nil)

	if bytes.Equal(project.CanonicalBytes(), restricted.CanonicalBytes()) {
		t.Fatalf("project and empty restricted scope canonical bytes unexpectedly match: %x", project.CanonicalBytes())
	}
	if project.CanonicalHash != project.CanonicalHashValue() {
		t.Fatalf("project hash = %x, want recomputed %x", project.CanonicalHash, project.CanonicalHashValue())
	}
	if restricted.CanonicalHash != restricted.CanonicalHashValue() {
		t.Fatalf("restricted hash = %x, want recomputed %x", restricted.CanonicalHash, restricted.CanonicalHashValue())
	}
}

func TestRecordAuthNormalizeSourceAuthorizationRejectsTombstonedFloorWiderThanCapture(t *testing.T) {
	capture := mustVisibility(t, VisibilityKindRestricted, nil, []string{testGroupAlpha})
	floor := mustVisibility(t, VisibilityKindProject, nil, nil)

	_, err := NormalizeSourceAuthorization(SourceAuthorization{
		Version:       SourceAuthorizationVersionV1,
		Kind:          SourceKindVPS,
		SourceID:      testVPSID,
		State:         SourceStateTombstoned,
		CaptureScope:  capture,
		FinalFloor:    ptrVisibility(floor),
		LastLiveScope: ptrVisibility(capture),
	})
	if err == nil {
		t.Fatal("NormalizeSourceAuthorization() error = nil, want wider tombstone floor rejection")
	}
}

func TestRecordAuthNormalizeSourceAuthorizationRejectsTombstoneWithoutLastLiveScope(t *testing.T) {
	capture := mustVisibility(t, VisibilityKindProject, nil, nil)
	floor := mustVisibility(t, VisibilityKindProject, nil, nil)

	_, err := NormalizeSourceAuthorization(SourceAuthorization{
		Version:      SourceAuthorizationVersionV1,
		Kind:         SourceKindVPS,
		SourceID:     testVPSID,
		State:        SourceStateTombstoned,
		CaptureScope: capture,
		FinalFloor:   ptrVisibility(floor),
	})
	if err == nil {
		t.Fatal("NormalizeSourceAuthorization() error = nil, want missing last live scope rejection")
	}
}

func TestRecordAuthNormalizeSourceAuthorizationEnforcesTombstoneTransitionWitness(t *testing.T) {
	project := mustVisibility(t, VisibilityKindProject, nil, nil)
	viewerGroup := mustVisibility(t, VisibilityKindRestricted, nil, []string{testGroupAlpha})
	emptyRestricted := mustVisibility(t, VisibilityKindRestricted, nil, nil)

	tests := []struct {
		name    string
		source  SourceAuthorization
		wantErr bool
	}{
		{
			name: "final floor cannot reopen prior live restriction",
			source: SourceAuthorization{
				Version:       SourceAuthorizationVersionV1,
				Kind:          SourceKindVPS,
				SourceID:      testVPSID,
				State:         SourceStateTombstoned,
				CaptureScope:  project,
				FinalFloor:    ptrVisibility(project),
				LastLiveScope: ptrVisibility(viewerGroup),
			},
			wantErr: true,
		},
		{
			name: "last live scope cannot widen capture",
			source: SourceAuthorization{
				Version:       SourceAuthorizationVersionV1,
				Kind:          SourceKindVPS,
				SourceID:      testVPSID,
				State:         SourceStateTombstoned,
				CaptureScope:  viewerGroup,
				FinalFloor:    ptrVisibility(viewerGroup),
				LastLiveScope: ptrVisibility(project),
			},
			wantErr: true,
		},
		{
			name: "final floor may match prior live scope",
			source: SourceAuthorization{
				Version:       SourceAuthorizationVersionV1,
				Kind:          SourceKindVPS,
				SourceID:      testVPSID,
				State:         SourceStateTombstoned,
				CaptureScope:  project,
				FinalFloor:    ptrVisibility(viewerGroup),
				LastLiveScope: ptrVisibility(viewerGroup),
			},
		},
		{
			name: "final floor may narrow prior live scope",
			source: SourceAuthorization{
				Version:       SourceAuthorizationVersionV1,
				Kind:          SourceKindVPS,
				SourceID:      testVPSID,
				State:         SourceStateTombstoned,
				CaptureScope:  project,
				FinalFloor:    ptrVisibility(emptyRestricted),
				LastLiveScope: ptrVisibility(viewerGroup),
			},
		},
		{
			name: "live source must not carry last live scope",
			source: SourceAuthorization{
				Version:       SourceAuthorizationVersionV1,
				Kind:          SourceKindVPS,
				SourceID:      testVPSID,
				State:         SourceStateLive,
				CaptureScope:  project,
				CurrentScope:  ptrVisibility(viewerGroup),
				LastLiveScope: ptrVisibility(viewerGroup),
			},
			wantErr: true,
		},
		{
			name: "tombstone source must not carry current scope",
			source: SourceAuthorization{
				Version:       SourceAuthorizationVersionV1,
				Kind:          SourceKindVPS,
				SourceID:      testVPSID,
				State:         SourceStateTombstoned,
				CaptureScope:  project,
				CurrentScope:  ptrVisibility(viewerGroup),
				FinalFloor:    ptrVisibility(viewerGroup),
				LastLiveScope: ptrVisibility(viewerGroup),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeSourceAuthorization(tt.source)
			if tt.wantErr && err == nil {
				t.Fatal("NormalizeSourceAuthorization() error = nil, want failure")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("NormalizeSourceAuthorization() error = %v, want nil", err)
			}
		})
	}
}

func TestRecordAuthPolicyAuthorizeEnforcesResourceIntersection(t *testing.T) {
	viewerByRole := mustActor(t, RoleViewer, nil)
	viewerByGroup := mustActor(t, RoleViewer, []string{testGroupAlpha})
	admin := mustActor(t, RoleProjectAdmin, []string{testGroupAlpha})

	project := mustVisibility(t, VisibilityKindProject, nil, nil)
	viewerRole := mustVisibility(t, VisibilityKindRestricted, []Role{RoleViewer}, nil)
	viewerGroup := mustVisibility(t, VisibilityKindRestricted, nil, []string{testGroupAlpha})
	otherGroup := mustVisibility(t, VisibilityKindRestricted, nil, []string{testGroupBeta})
	emptyRestricted := mustVisibility(t, VisibilityKindRestricted, nil, nil)

	tests := []struct {
		name       string
		actor      ActorScope
		capability Capability
		resource   ResourceScope
		wantAllow  bool
	}{
		{
			name:       "project admin allows permanent deletion after valid evidence",
			actor:      admin,
			capability: CapabilityPermanentDelete,
			resource:   mustResource(t, project, mustLiveSource(t, project, project)),
			wantAllow:  true,
		},
		{
			name:       "viewer role allows record read",
			actor:      viewerByRole,
			capability: CapabilityRecordRead,
			resource:   mustResource(t, viewerRole, mustLiveSource(t, viewerRole, viewerRole)),
			wantAllow:  true,
		},
		{
			name:       "viewer group allows record read",
			actor:      viewerByGroup,
			capability: CapabilityRecordRead,
			resource:   mustResource(t, viewerGroup, mustLiveSource(t, viewerGroup, viewerGroup)),
			wantAllow:  true,
		},
		{
			name:       "viewer must survive role group and source intersection",
			actor:      viewerByGroup,
			capability: CapabilityRecordRead,
			resource:   mustResource(t, viewerRole, mustLiveSource(t, otherGroup, otherGroup)),
			wantAllow:  false,
		},
		{
			name:       "cross project is denied",
			actor:      viewerByRole,
			capability: CapabilityRecordRead,
			resource: ResourceScope{
				Version:    ResourceScopeVersionV1,
				ProjectID:  ProjectID("other"),
				Visibility: project,
				Sources:    []SourceAuthorization{mustLiveSource(t, project, project)},
			},
			wantAllow: false,
		},
		{
			name:       "unknown capability is denied",
			actor:      viewerByRole,
			capability: Capability("record.read "),
			resource:   mustResource(t, viewerRole, mustLiveSource(t, viewerRole, viewerRole)),
			wantAllow:  false,
		},
		{
			name:       "unknown visibility kind is denied",
			actor:      viewerByRole,
			capability: CapabilityRecordRead,
			resource: func() ResourceScope {
				resource := mustResource(t, viewerRole, mustLiveSource(t, viewerRole, viewerRole))
				resource.Visibility.Kind = VisibilityKind("unknown")
				return resource
			}(),
			wantAllow: false,
		},
		{
			name:       "unknown scope version is denied",
			actor:      viewerByRole,
			capability: CapabilityRecordRead,
			resource: func() ResourceScope {
				resource := mustResource(t, viewerRole, mustLiveSource(t, viewerRole, viewerRole))
				resource.Sources[0].CurrentScope.Version = 99
				return resource
			}(),
			wantAllow: false,
		},
		{
			name:       "unknown source kind is denied",
			actor:      viewerByRole,
			capability: CapabilityRecordRead,
			resource: func() ResourceScope {
				resource := mustResource(t, viewerRole, mustLiveSource(t, viewerRole, viewerRole))
				resource.Sources[0].Kind = SourceKind("unknown")
				return resource
			}(),
			wantAllow: false,
		},
		{
			name:       "unknown source version is denied",
			actor:      viewerByRole,
			capability: CapabilityRecordRead,
			resource: func() ResourceScope {
				resource := mustResource(t, viewerRole, mustLiveSource(t, viewerRole, viewerRole))
				resource.Sources[0].Version = 99
				return resource
			}(),
			wantAllow: false,
		},
		{
			name:       "unknown source state is denied",
			actor:      viewerByRole,
			capability: CapabilityRecordRead,
			resource: func() ResourceScope {
				resource := mustResource(t, viewerRole, mustLiveSource(t, viewerRole, viewerRole))
				resource.Sources[0].State = SourceState("unknown")
				return resource
			}(),
			wantAllow: false,
		},
		{
			name:       "empty restricted is deny all for viewer",
			actor:      viewerByRole,
			capability: CapabilityRecordRead,
			resource:   mustResource(t, emptyRestricted, mustLiveSource(t, project, project)),
			wantAllow:  false,
		},
		{
			name:       "live widening relative to capture is denied",
			actor:      viewerByGroup,
			capability: CapabilityRecordRead,
			resource: func() ResourceScope {
				resource := mustResource(t, project, mustLiveSource(t, viewerGroup, viewerGroup))
				resource.Sources[0].CurrentScope = ptrVisibility(project)
				return resource
			}(),
			wantAllow: false,
		},
		{
			name:       "tombstone without final floor is denied",
			actor:      viewerByRole,
			capability: CapabilityRecordRead,
			resource: func() ResourceScope {
				resource := mustResource(t, viewerRole, mustLiveSource(t, viewerRole, viewerRole))
				resource.Sources[0].State = SourceStateTombstoned
				resource.Sources[0].CurrentScope = nil
				return resource
			}(),
			wantAllow: false,
		},
		{
			name:       "tampered tombstone final floor is denied",
			actor:      viewerByGroup,
			capability: CapabilityRecordRead,
			resource: func() ResourceScope {
				resource := mustResource(t, viewerGroup, mustTombstonedSource(t, viewerGroup, viewerGroup, viewerGroup))
				resource.Sources[0].FinalFloor.AllowedGroupIDs = []string{testGroupBeta}
				return resource
			}(),
			wantAllow: false,
		},
		{
			name:       "source digest drift is denied without resource detail",
			actor:      viewerByRole,
			capability: CapabilityRecordRead,
			resource: func() ResourceScope {
				resource := mustResource(t, viewerRole, mustLiveSource(t, viewerRole, viewerRole))
				resource.Sources[0].Digest[0] ^= 0xff
				return resource
			}(),
			wantAllow: false,
		},
		{
			name:       "project admin cannot bypass tampered source evidence",
			actor:      admin,
			capability: CapabilityPermanentDelete,
			resource: func() ResourceScope {
				resource := mustResource(t, project, mustLiveSource(t, project, project))
				resource.Sources[0].Digest[0] ^= 0xff
				return resource
			}(),
			wantAllow: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := (Policy{}).Authorize(tt.actor, tt.capability, tt.resource)
			if tt.wantAllow {
				if err != nil {
					t.Fatalf("Authorize() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, ErrDenied) {
				t.Fatalf("Authorize() error = %v, want errors.Is(ErrDenied)", err)
			}
			if strings.Contains(err.Error(), testVPSID) {
				t.Fatalf("denial error leaks source identifier: %q", err)
			}
		})
	}
}

func TestRecordAuthPolicyProjectAdminRespectsRestrictedScopeIntersection(t *testing.T) {
	admin := mustActor(t, RoleProjectAdmin, []string{testGroupAlpha})
	project := mustVisibility(t, VisibilityKindProject, nil, nil)
	emptyRestricted := mustVisibility(t, VisibilityKindRestricted, nil, nil)
	otherGroup := mustVisibility(t, VisibilityKindRestricted, nil, []string{testGroupBeta})
	adminRole := mustVisibility(t, VisibilityKindRestricted, []Role{RoleProjectAdmin}, nil)

	tests := []struct {
		name       string
		resource   ResourceScope
		wantAllow  bool
		wantReason DenialReason
	}{
		{
			name:       "project admin is denied by empty restricted resource visibility",
			resource:   mustResource(t, emptyRestricted, mustLiveSource(t, project, project)),
			wantReason: DenialReasonVisibility,
		},
		{
			name:       "project admin is denied by restricted tombstone final floor",
			resource:   mustResource(t, project, mustTombstonedSource(t, project, otherGroup, otherGroup)),
			wantReason: DenialReasonSource,
		},
		{
			name:      "project admin is allowed by explicit restricted role grant",
			resource:  mustResource(t, adminRole, mustTombstonedSource(t, adminRole, adminRole, adminRole)),
			wantAllow: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := (Policy{}).Authorize(admin, CapabilityPermanentDelete, tt.resource)
			if tt.wantAllow {
				if err != nil {
					t.Fatalf("Authorize() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, ErrDenied) {
				t.Fatalf("Authorize() error = %v, want errors.Is(ErrDenied)", err)
			}
			if reason, ok := DenialReasonFromError(err); !ok || reason != tt.wantReason {
				t.Fatalf("DenialReasonFromError() = (%q, %t), want (%q, true)", reason, ok, tt.wantReason)
			}
		})
	}
}

func TestRecordAuthPolicyRejectsTamperedTombstoneLastLiveScopeDigest(t *testing.T) {
	project := mustVisibility(t, VisibilityKindProject, nil, nil)
	viewerGroup := mustVisibility(t, VisibilityKindRestricted, nil, []string{testGroupAlpha})
	emptyRestricted := mustVisibility(t, VisibilityKindRestricted, nil, nil)
	resource := mustResource(t, project, mustTombstonedSource(t, project, viewerGroup, emptyRestricted))
	resource.Sources[0].LastLiveScope = ptrVisibility(project)

	err := (Policy{}).Authorize(
		mustActor(t, RoleProjectAdmin, nil),
		CapabilityPermanentDelete,
		resource,
	)
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("Authorize() error = %v, want errors.Is(ErrDenied)", err)
	}
}

func mustActor(t *testing.T, role Role, groupIDs []string) ActorScope {
	t.Helper()
	actor, err := NormalizeActorScope(ActorScope{
		UserID:    testActorUserID,
		Role:      role,
		ProjectID: ProjectIDDefault,
		GroupIDs:  groupIDs,
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	return actor
}

func mustVisibility(t *testing.T, kind VisibilityKind, roles []Role, groupIDs []string) VisibilityScope {
	t.Helper()
	scope, err := NormalizeVisibilityScope(VisibilityScope{
		Version:         VisibilityScopeVersionV1,
		Kind:            kind,
		ProjectID:       ProjectIDDefault,
		AllowedRoles:    roles,
		AllowedGroupIDs: groupIDs,
		PolicyVersion:   PolicyVersionV1,
		PolicyRevision:  1,
	})
	if err != nil {
		t.Fatalf("NormalizeVisibilityScope() error = %v", err)
	}
	return scope
}

func mustLiveSource(t *testing.T, capture, current VisibilityScope) SourceAuthorization {
	t.Helper()
	source, err := NormalizeSourceAuthorization(SourceAuthorization{
		Version:      SourceAuthorizationVersionV1,
		Kind:         SourceKindVPS,
		SourceID:     testVPSID,
		State:        SourceStateLive,
		CaptureScope: capture,
		CurrentScope: ptrVisibility(current),
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	return source
}

func mustTombstonedSource(t *testing.T, capture, lastLive, floor VisibilityScope) SourceAuthorization {
	t.Helper()
	source, err := NormalizeSourceAuthorization(SourceAuthorization{
		Version:       SourceAuthorizationVersionV1,
		Kind:          SourceKindVPS,
		SourceID:      testVPSID,
		State:         SourceStateTombstoned,
		CaptureScope:  capture,
		FinalFloor:    ptrVisibility(floor),
		LastLiveScope: ptrVisibility(lastLive),
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	return source
}

func mustResource(t *testing.T, visibility VisibilityScope, source SourceAuthorization) ResourceScope {
	t.Helper()
	return ResourceScope{
		Version:    ResourceScopeVersionV1,
		ProjectID:  ProjectIDDefault,
		Visibility: visibility,
		Sources:    []SourceAuthorization{source},
	}
}

func ptrVisibility(scope VisibilityScope) *VisibilityScope {
	return &scope
}

func TestParseCanonicalVisibilityScopeRoundTripsTrustedBytesAndRejectsUnknownVersion(t *testing.T) {
	t.Parallel()

	project := mustVisibility(t, VisibilityKindProject, nil, nil)
	restricted := mustVisibility(t, VisibilityKindRestricted, []Role{RoleProjectAdmin}, []string{testGroupAlpha})
	for _, scope := range []VisibilityScope{project, restricted} {
		got, err := ParseCanonicalVisibilityScope(scope.CanonicalBytes())
		if err != nil {
			t.Fatalf("ParseCanonicalVisibilityScope() error = %v", err)
		}
		if !bytes.Equal(got.CanonicalBytes(), scope.CanonicalBytes()) || got.CanonicalHash != scope.CanonicalHash {
			t.Fatalf("parsed scope = %#v, want %#v", got, scope)
		}
	}

	if _, err := ParseCanonicalVisibilityScope(nil); !errors.Is(err, ErrInvalidVisibilityScope) {
		t.Fatalf("ParseCanonicalVisibilityScope(nil) error = %v, want ErrInvalidVisibilityScope", err)
	}
	tampered := append(append([]byte(nil), project.CanonicalBytes()...), 0x00)
	if _, err := ParseCanonicalVisibilityScope(tampered); !errors.Is(err, ErrInvalidVisibilityScope) {
		t.Fatalf("ParseCanonicalVisibilityScope(trailing byte) error = %v, want ErrInvalidVisibilityScope", err)
	}
	unknown := append([]byte(nil), project.CanonicalBytes()...)
	// Version sits after the domain string length prefix and domain bytes.
	unknown[4+len("recordauth.visibility.v1")] = 2
	if _, err := ParseCanonicalVisibilityScope(unknown); !errors.Is(err, ErrInvalidVisibilityScope) {
		t.Fatalf("ParseCanonicalVisibilityScope(unknown version) error = %v, want ErrInvalidVisibilityScope", err)
	}
}

func TestAllowsCapabilityGatesSensitiveExportWithoutInventingResource(t *testing.T) {
	admin := mustActor(t, RoleProjectAdmin, nil)
	viewer := mustActor(t, RoleViewer, nil)
	if err := AllowsCapability(admin, CapabilityExportSensitiveTopology); err != nil {
		t.Fatalf("admin sensitive export = %v", err)
	}
	if err := AllowsCapability(viewer, CapabilityExport); err != nil {
		t.Fatalf("viewer export = %v", err)
	}
	if err := AllowsCapability(viewer, CapabilityExportSensitiveTopology); !errors.Is(err, ErrDenied) {
		t.Fatalf("viewer sensitive export = %v, want ErrDenied", err)
	}
}
