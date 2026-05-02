# Houfeng V1 Delivery Verification Design

## Context

Houfeng V1 has moved through implementation closure phases for runtime semantics, agent reliability, retention/aggregation, dashboard/events acceptance surfaces, and trend degradation surfaces. The remaining V1 work is not new product design. It is delivery verification: prove that the frozen V1 can be built, deployed, visually checked against the authoritative baseline, and operated through the first end-to-end smoke path.

The frozen constraints remain unchanged:

- Product identity: `候风 / Houfeng Fleet Control Plane`.
- System shape: single-user Go center, PostgreSQL, and N Go systemd agents.
- Frontend: React/Vite, dark-first, high-density operational UI.
- Visual authority: Unified / Baseline Stitch screens under `docs/design/v1-baseline/stitch/`.
- Non-goals: Docker-first deployment, extra MQ, extra TSDB, microservices, generic rule engine, or V1 visual redesign.

## Selected Approach

Use a documentation-and-evidence closure slice.

This phase should add the missing operational delivery artifacts and a reproducible verification packet without changing product behavior unless a real delivery blocker is found. The center and agent already build as binaries and the repository already has automated verification. The gap is that an operator does not yet have first-class deployment examples, a fresh-install smoke run, visual verification instructions/evidence, or a final V1 gap checklist.

Rejected alternatives:

- Add Docker Compose as the main delivery path. This conflicts with the frozen systemd-agent direction and adds an unrequested runtime layer.
- Reopen visual implementation work before evidence. Phase 6 should compare against the Unified / Baseline screens and only report concrete gaps.
- Treat manual smoke as complete without recording what was actually executed. Delivery evidence must distinguish automated verification, reproducible procedure, and any environment-dependent manual gaps.

## Delivery Artifacts

Add operational artifacts in three focused areas:

1. **Systemd examples**
   - `docs/deploy/systemd/houfeng-center.service`
   - `docs/deploy/systemd/houfeng-agent.service`
   - Unit examples should use the existing binary names and environment variables.
   - Examples are documentation fixtures, not installed files.

2. **Deployment and smoke documentation**
   - A deployment guide should cover build commands, PostgreSQL requirement, center environment, web asset build, center startup, agent token file, and systemd enablement.
   - A fresh-install smoke guide should describe the V1 path:
     1. create a Node;
     2. issue an enrollment token;
     3. enroll and run an agent;
     4. create a Target;
     5. add a ProbeItem;
     6. receive host/probe observations;
     7. trigger and recover an incident;
     8. verify event and notification records.
   - The smoke guide must mark commands that are automated, commands that require a local PostgreSQL database, and checks that require a configured Telegram destination.

3. **Final V1 gap checklist**
   - The checklist should map frozen V1 areas to current implementation evidence.
   - Each item must be classified as closed, partially closed with explicit evidence gap, or intentionally deferred outside V1.
   - It must not change the frozen design baseline.

## Visual Verification

Visual verification should use the existing Stitch PNG exports as the reference set. The first V1 evidence packet should cover primary baseline screens:

- Global App Shell Baseline
- Global Control Center (Unified)
- Fleet Nodes List
- Node Detail Center (Unified)
- Node Onboarding & Binding Conflict (Unified)

Supporting screens should be listed as secondary coverage:

- Security Audit & Events
- Global Logs Explorer
- System Configuration
- Target Detail as legacy-but-still-usable reference

The output should be a reproducible visual verification document. If live browser screenshots are not captured in this environment, the document must say so explicitly and preserve a command path for capturing them later. It is acceptable for Phase 6 to leave a visual evidence gap only if the gap is explicitly tracked in the final checklist.

## Smoke Evidence Semantics

Use three evidence levels:

- **Automated:** verified by commands such as `go test ./...`, `./scripts/verify.sh`, and frontend tests/builds.
- **Documented procedure:** the exact operator path is present, but execution depends on an external PostgreSQL or Telegram environment not guaranteed in this session.
- **Manual evidence required:** a human or later CI environment must run the documented commands and paste observed outputs into the smoke document.

This avoids overstating completion while still making V1 operable.

## Testing and Verification

Required verification before closing the phase:

- `go test ./...`
- `./scripts/verify.sh`
- `cd web && npm run build`
- Review that systemd examples reference only implemented binaries and documented environment variables.
- Review that smoke documentation uses existing API routes and UI flows.
- Review that the visual verification document references only the Unified / Baseline visual authority.

## Expected Outcome

After this phase, Houfeng V1 should have:

- deployable binary/systemd guidance for center and agent;
- a reproducible local and systemd delivery path;
- a fresh-install smoke checklist aligned with implemented API/UI behavior;
- a visual baseline verification record;
- a final V1 gap checklist with no silent unknowns.

The implementation may still require an operator-provided PostgreSQL instance and optional Telegram credentials for full live smoke evidence, but those requirements will be explicit and no longer hidden.

## Self-Review

- Placeholder scan: no `TBD`, `TODO`, or open-ended placeholder requirements remain.
- Internal consistency: this design preserves the frozen V1 product and technical baseline and scopes Phase 6 to delivery verification.
- Scope check: the slice is focused enough for one implementation plan because it creates docs/examples/evidence artifacts rather than new runtime behavior.
- Ambiguity check: evidence levels make clear which claims are automated versus environment-dependent.
