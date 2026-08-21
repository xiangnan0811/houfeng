# Capacity and browser run 2026-08-21

Child 11 assembled existing owning-child suites. It did not invent domain
performance logic or a 4 GiB cgroup harness.

## Capacity

`./scripts/run-records-capacity.sh` (unit only) and
`./scripts/run-records-capacity.sh --profile local` both passed.
`TMPDIR=/tmp`. Any `--- SKIP:` is a failure.

Unit (always):

- `TestComparisonDetailPerformanceRecordsQuantiles`
- `TestEvidenceCapacityPolicyEvaluationMatrix`
- `TestEvidenceMaintenanceWorkerRunOnceIsBoundedAndPublishesAggregateMetrics`
- `TestRestoreFailureRetryUsesFreshTargetAndBoundedWorkspace`
- `TestProjectorTruncatedPageStopsAtTheLastRowItActuallyRead`

Local PostgreSQL profile used `HOUFENG_ACTIVITY_PERF_SCALE=0.001` (minimum
1000 rows) through `scripts/test-record-platform-integration.sh postgres`:

- `TestEvidenceComparisonCandidatePostgresQueryIsBounded`
- `TestPostgresIntegrationEvidenceCapacityExactBoundaryAndAccounting`
- `TestPostgresIntegrationRecordActivityPerformance`
- `TestPostgresIntegrationVPSOverviewPerformance` (store + HTTP)

`--profile s3` is the same representative PostgreSQL suite; it was not rerun
after local passed. The default 1M-row activity seed remains owning-child.

## Browser

`./scripts/run-records-browser.sh` used Node 22.23.1, production preview, and
Playwright Chromium from `$HOME/.cache/ms-playwright`. 64/64 passed.

Inventoried specs: `visual-contracts`, `page-states`, `accessibility`,
`comparison-workbench`, `record-workspace`, `record-portability`.

Production `web/src` (non-test) and `web/dist` were scanned for e2e fixture
tokens (`dashboardTestFixtures`, `coreRouteProfile`, `vpsOverviewProfile`,
`@axe-core/playwright`, `web/e2e/` imports). No leak.
