package recordreadiness

import (
	"fmt"
	"strings"
)

var requiredCapacityCorpusTests = []string{
	"TestComparisonDetailPerformanceRecordsQuantiles",
	"TestEvidenceCapacityPolicyEvaluationMatrix",
	"TestEvidenceMaintenanceWorkerRunOnceIsBoundedAndPublishesAggregateMetrics",
	"TestRestoreFailureRetryUsesFreshTargetAndBoundedWorkspace",
	"TestProjectorTruncatedPageStopsAtTheLastRowItActuallyRead",
}

var requiredCapacityPostgresTests = []string{
	"TestEvidenceComparisonCandidatePostgresQueryIsBounded",
	"TestPostgresIntegrationEvidenceCapacityExactBoundaryAndAccounting",
	"TestPostgresIntegrationRecordActivityPerformance",
	"TestPostgresIntegrationVPSOverviewPerformance",
}

var requiredBrowserSpecFiles = []string{
	"visual-contracts.spec.ts",
	"page-states.spec.ts",
	"accessibility.spec.ts",
	"comparison-workbench.spec.ts",
	"record-workspace.spec.ts",
	"record-portability.spec.ts",
}

var requiredBrowserCorpusTitles = []string{
	"VPS 概览 remains complete and reachable at 390px",
	"VPS 概览 has no serious or critical axe violations",
	"VPS 概览 keeps loading until the overview response is released",
	"record search import and export have no serious or critical accessibility violations at 390px",
	"单主体时间线 remains complete and reachable at 390px",
	"单主体时间线 has no serious or critical axe violations",
	"单主体时间线 loading / empty / local-error states",
	"record editor empty/new state stays operable at",
	"record editor new state has no serious or critical accessibility violations",
	"record material drawer stays operable at",
	"record material drawer has no serious or critical accessibility violations",
	"横向比较工作台 save command remains complete and reachable at 390px",
	"横向比较工作台 has no serious or critical axe violations",
	"横向比较工作台 390px folds conditions and scrolls only the named matrix",
	"横向比较工作台 keyboard can select, switch kinds, save, then revoke",
}

var productionBundleForbiddenTokens = []string{
	"web/e2e/",
	"../../e2e/",
	"dashboardTestFixtures",
	"@axe-core/playwright",
	"coreRouteProfile",
	"vpsOverviewProfile",
}

func RequiredCapacityCorpusTests() []string {
	return append([]string(nil), requiredCapacityCorpusTests...)
}

func RequiredCapacityPostgresTests() []string {
	return append([]string(nil), requiredCapacityPostgresTests...)
}

func RequiredBrowserSpecFiles() []string {
	return append([]string(nil), requiredBrowserSpecFiles...)
}

func RequiredBrowserCorpusTitles() []string {
	return append([]string(nil), requiredBrowserCorpusTitles...)
}

func ProductionBundleForbiddenTokens() []string {
	return append([]string(nil), productionBundleForbiddenTokens...)
}

func ScanProductionBundleSafe(payload []byte) error {
	text := string(payload)
	for _, leaked := range productionBundleForbiddenTokens {
		if strings.Contains(text, leaked) {
			return fmt.Errorf("%w: %s", ErrContentLeak, leaked)
		}
	}
	return nil
}
