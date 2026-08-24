# Records authority research

## Confirmed blocker

The current Compose draft can converge the Records APP schema but cannot make Records writes available. Center constructs no production admission gate when the four instance/deployment identity values are absent, and a nil gate rejects before primitive writes. Even with those values present, admission requires both an active matching deployment contract and fresh matching `deployment_membership`. No production code currently writes the membership ledger.

The existing activation projectors deliberately accept only a canonical versioned projection command after external ledger/full-witness verification. They are migrator-owned and are not executable by runtime or platform-admin. Directly inserting contract/membership rows in db-init would therefore contradict the current authority and fail-closed design rather than complete it.

## Options considered

1. **Approved: single-host local authority.** Persist a signed, bounded, canonical authority ledger outside PostgreSQL; derive and submit the existing activation projection during initialization; run a dedicated least-privilege service that renews exact known membership through a new narrow function. This preserves admission semantics and adds no operator-managed variables.
2. **Rejected: db-init fabricates rows.** Simple, but it makes the database/bootstrap path its own proof of authority, bypasses the projector contract, and supplies no long-running heartbeat owner.
3. **Rejected: relax or bypass admission.** This would make a deployment appear healthy while Records writes lack the security contract the runtime promises.

## Selected boundaries

- This is a closed, single-host Compose profile, not a general multi-host authority or quorum system.
- Durable authority state, PostgreSQL, and attachments are restored together.
- The authority key and database credential are stack-generated private state. Center sees only the public deployment ID; processor sees no authority/admin/bootstrap/migrator secret.
- Activation remains owned by the existing projector. The verifier, not its caller, derives command digests from the complete signed ledger.
- Membership uses a new additive migration and exact current APP ACL fragment. The authority role has only the heartbeat function; it has no table DML or projector access.
- Heartbeat inventory contains actual admission-gate consumers only. The initial Compose profile requires the Center API instance; the processor is not added unless its runtime path begins using the gate.
- Missing/corrupt/mismatched authority state against an active database fails closed and requires coordinated restore. It is never regenerated over active state.

## Required evidence

- Unit tests for canonical state, signature/chain verification, hostile state, derived projection, atomic persistence, file-based deployment ID, and authority lifecycle.
- Strict PostgreSQL 16 tests for activation exact-repeat, receipt validation, membership renewal/expiry, hostile identities, and complete role privilege denial.
- Compose static and isolated Docker smoke proving a real Records write plus attachment processing/ClamAV, restart renewal, safe restore boundaries, and no manual SQL.
