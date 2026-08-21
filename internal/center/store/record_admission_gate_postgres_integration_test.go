package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPostgresDeploymentMembershipAdmissionGateFailClosedOnEmptyStaleAndWrongDeployment(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordPlatformPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "admission-gate-membership", 1)
	gate := mustDeploymentMembershipAdmissionGate(t)
	identity := testDeploymentMembershipIdentity()

	admit := func() error {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin admission transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		return gate.Admit(ctx, tx)
	}

	if err := admit(); !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
		t.Fatalf("empty membership Admit() error = %v, want ErrRecordPlatformAdmissionUnavailable", err)
	}

	if _, err := pool.Exec(ctx, `
		insert into public.deployment_membership (
			instance_id, deployment_id, project_id, instance_kind, deployment_epoch,
			fence_contract_version, capability, load_balancer_admitted, queue_admitted, heartbeat_expires_at
		) values ($1, $2, $3, $4, 1, 2, $5, true, false, now() + interval '1 hour')
	`, identity.InstanceID, string(identity.DeploymentID), string(identity.ProjectID), identity.InstanceKind, identity.Capability); err != nil {
		t.Fatalf("insert matching membership: %v", err)
	}
	if err := admit(); !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
		t.Fatalf("unactivated contract Admit() error = %v, want ErrRecordPlatformAdmissionUnavailable", err)
	}

	if _, err := pool.Exec(ctx, `
		update public.deployment_membership
		set heartbeat_expires_at = created_at + interval '1 millisecond'
		where instance_id = $1
	`, identity.InstanceID); err != nil {
		t.Fatalf("expire membership heartbeat: %v", err)
	}
	if err := admit(); !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
		t.Fatalf("stale heartbeat Admit() error = %v, want ErrRecordPlatformAdmissionUnavailable", err)
	}

	if _, err := pool.Exec(ctx, `
		update public.deployment_membership
		set deployment_id = $2,
		    heartbeat_expires_at = now() + interval '1 hour',
		    updated_at = now()
		where instance_id = $1
	`, identity.InstanceID, "dp-"+strings.Repeat("b", 64)); err != nil {
		t.Fatalf("shift membership deployment: %v", err)
	}
	if err := admit(); !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
		t.Fatalf("wrong deployment Admit() error = %v, want ErrRecordPlatformAdmissionUnavailable", err)
	}
}

func TestPostgresDeploymentMembershipAdmissionGateUsesCallerTransaction(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordPlatformPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "admission-gate-caller-tx", 1)
	gate := mustDeploymentMembershipAdmissionGate(t)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin caller transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := gate.Admit(ctx, tx); !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
		t.Fatalf("Admit() error = %v, want fail-closed on empty membership", err)
	}

	var one int
	if err := tx.QueryRow(ctx, `select 1`).Scan(&one); err != nil {
		t.Fatalf("caller transaction after Admit() error = %v", err)
	}
	if one != 1 {
		t.Fatalf("select 1 = %d, want 1", one)
	}
}
