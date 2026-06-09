# IP质量采集覆盖与展示完整性修复 - Implementation Plan

## Preconditions

- Do not implement until the user approves `prd.md`, `design.md`, and this `implement.md`.
- After approval, run `python3 ./.trellis/scripts/task.py start .trellis/tasks/06-09-ip-quality-full-coverage`.
- Before editing code, load `trellis-before-dev` for backend and web specs.
- Keep work on branch `fix/ip-quality-full-coverage`; never commit or push local `main`.
- Ensure hooks are enabled with `sh scripts/setup-git-hooks.sh`.

## Ordered Checklist

### 1. Contract Tests First

- Add JSON round-trip tests in `internal/contracts/agentapi/types_observation_test.go` for:
  - provider `status`, `source_type`, `latency_ms`, `extra_json`
  - service `source`, `probe_status`, `latency_ms`, `extra_json`
  - report `coverage`, `diagnostics_json`
- Add backwards compatibility test with old payload shape.
- Implement new fields in `internal/contracts/agentapi/types.go`.

Validation:

```bash
go test ./internal/contracts/agentapi
```

### 2. Agent Provider Registry

- Split `agent/ipquality/collector.go` only where needed:
  - create `source.go`
  - create `providers.go`
  - create `provider_parsers.go`
- Add provider parser tests in `agent/ipquality/collector_test.go` or new focused files for:
  - `ipapi.is`
  - `ipquery.io`
  - `proxycheck.io`
  - `ip2location.io`
  - `ipwho.is`
  - timeout/non-json/rate-limit source failure rows
  - optional source not configured rows
- Implement provider source registry and normalized parser mapping.
- Keep existing ipapi.is tests passing.

Validation:

```bash
go test ./agent/ipquality
```

### 3. Agent Service Probe Registry

- Add service probe tests for:
  - Netflix full/originals/blocked/unknown parser
  - ChatGPT/OpenAI available/blocked/web-only/app-only/unknown parser
  - YouTube Premium available/not available/China/unknown parser
  - Amazon Prime Video region parser
  - TikTok region/challenge unknown parser
  - Reddit 200/403/unknown parser
  - Disney+ skipped diagnostic when safe default probe is unavailable
- Implement service probes with per-probe timeout and no remote script/cookie dependency.
- Ensure every configured service emits a row, even when skipped or failed.

Validation:

```bash
go test ./agent/ipquality
go test ./agent/runtime
go test ./agent/syncqueue
```

### 4. Center Domain And Migration

- Add migration `db/migrations/0042_extend_ip_quality_source_details.sql`.
- Extend `internal/center/ipquality/types.go` with new fields and validation.
- Extend `internal/center/ipquality/raw_json.go` tests for provider/service extra JSON sanitization and truncation.
- Update `internal/center/store/ip_quality.go` insert/read paths.
- Add store tests for:
  - saving extra fields
  - reading latest full detail
  - reading historical detail
  - old reports with default new columns
  - failure/0.0.0.0 still filtered

Validation:

```bash
go test ./internal/center/ipquality ./internal/center/store ./internal/center/store/migrate
```

### 5. Center Ingest And API

- Update `internal/center/http/handlers/agent.go` request mapping.
- Update agent sync tests to assert new fields are preserved.
- Update `internal/center/http/handlers/ip_quality.go`:
  - existing latest endpoint returns new fields.
  - new historical detail endpoint returns assigned report detail only.
- Update router tests for the new endpoint.

Validation:

```bash
go test ./internal/center/http/handlers ./internal/center/http
```

### 6. Asset Decision Compatibility

- Update asset decision data loading only if new read shapes require it.
- Ensure risk evidence ignores:
  - provider `status != success`
  - service `probe_status != success`
  - skipped/not_configured rows
  - datacenter/server alone
- Add or update tests for these cases.

Validation:

```bash
go test ./internal/center/assetdecisions
```

### 7. Frontend Types And API Client

- Extend `web/src/lib/types.ts` for:
  - `IPQualityCoverage`
  - provider v2 fields
  - service v2 fields
  - historical report detail response
- Extend `web/src/lib/api.ts` with historical detail fetcher.
- Add tests for client shape if existing API tests cover this layer.

Validation:

```bash
npm --prefix web test -- --run
```

### 8. Frontend IP Quality Page

- Update `web/src/components/ip-quality/ipQualityPresentation.ts`:
  - coverage uses API `coverage`.
  - risk counts only successful provider rows.
  - service counts only successful probe rows for blocked/unlocked.
- Update `IPQualityDashboard.tsx`:
  - provider rows show source status, source type, latency, extra details.
  - service rows show source, probe status, latency, details.
  - optional/skipped sources are visible.
  - history can load detail.
- Update `VPSIPQualityPage.test.tsx` and `VPSDetailPage.test.tsx`.
- Verify responsive layout after the added columns/details.

Validation:

```bash
npm --prefix web test -- --run
npm --prefix web run build
```

### 9. End-To-End Review

- Run backend packages touched:

```bash
go test ./agent/ipquality ./agent/runtime ./agent/syncqueue ./internal/contracts/agentapi ./internal/center/ipquality ./internal/center/store ./internal/center/store/migrate ./internal/center/http ./internal/center/http/handlers ./internal/center/assetdecisions
```

- Run all reasonable project-level checks if time permits:

```bash
go test ./...
npm --prefix web test -- --run
npm --prefix web run build
```

- Manually inspect API fixture output for:
  - multiple provider rows
  - at least one service row per configured service
  - optional sources marked not_configured/skipped
  - extra_json present and sanitized
  - coverage denominator greater than current row count when optional/skipped sources exist

## Risk Points

- Some no-key providers rate-limit or change schema. Mitigation: every source has isolated failure rows and tests using fixtures, not live network.
- Service probes are inherently brittle. Mitigation: low-intrusion subset, parser tests, visible diagnostics, no false blocked status on probe failure.
- Extra/raw JSON could grow. Mitigation: sanitize and limit at agent and center.
- Service unique index change can affect old assumptions. Mitigation: empty `source` default preserves old rows; tests must cover duplicate service with different source.
- Frontend table width can regress mobile. Mitigation: use scroll-x section and detail drawers instead of forcing all extra fields into columns.

## Review Gate Before Start

Planning is ready only when:

- `prd.md` has no blocking open questions.
- `design.md` records source policy, contracts, data flow, migration, UI, and rollback.
- `implement.md` has ordered tasks and validation commands.
- User explicitly approves entering execution.

After implementation, run `trellis-check` before any commit. Do not run finish-work, push, PR, merge, release, or Docker verification until the task is committed/archived and the branch is clean.
