package recordreadiness

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRequiredCapacityCorpusTestsAreClosedOrderedAndPresent(t *testing.T) {
	t.Parallel()

	wantUnit := []string{
		"TestComparisonDetailPerformanceRecordsQuantiles",
		"TestEvidenceCapacityPolicyEvaluationMatrix",
		"TestEvidenceMaintenanceWorkerRunOnceIsBoundedAndPublishesAggregateMetrics",
		"TestRestoreFailureRetryUsesFreshTargetAndBoundedWorkspace",
		"TestProjectorTruncatedPageStopsAtTheLastRowItActuallyRead",
	}
	wantPostgres := []string{
		"TestEvidenceComparisonCandidatePostgresQueryIsBounded",
		"TestPostgresIntegrationEvidenceCapacityExactBoundaryAndAccounting",
		"TestPostgresIntegrationRecordActivityPerformance",
		"TestPostgresIntegrationVPSOverviewPerformance",
	}
	if got := RequiredCapacityCorpusTests(); !reflect.DeepEqual(got, wantUnit) {
		t.Fatalf("RequiredCapacityCorpusTests() = %#v, want %#v", got, wantUnit)
	}
	if got := RequiredCapacityPostgresTests(); !reflect.DeepEqual(got, wantPostgres) {
		t.Fatalf("RequiredCapacityPostgresTests() = %#v, want %#v", got, wantPostgres)
	}
	got := RequiredCapacityCorpusTests()
	got[0] = "tampered"
	if fresh := RequiredCapacityCorpusTests(); !reflect.DeepEqual(fresh, wantUnit) {
		t.Fatalf("RequiredCapacityCorpusTests() after caller mutation = %#v", fresh)
	}

	requireTestsPresent(t, append(append([]string{}, wantUnit...), wantPostgres...))
}

func TestRequiredBrowserCorpusIsClosedOrderedAndPresent(t *testing.T) {
	t.Parallel()

	wantSpecs := []string{
		"visual-contracts.spec.ts",
		"page-states.spec.ts",
		"accessibility.spec.ts",
		"comparison-workbench.spec.ts",
		"record-workspace.spec.ts",
		"record-portability.spec.ts",
	}
	wantTitles := []string{
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
	if got := RequiredBrowserSpecFiles(); !reflect.DeepEqual(got, wantSpecs) {
		t.Fatalf("RequiredBrowserSpecFiles() = %#v, want %#v", got, wantSpecs)
	}
	if got := RequiredBrowserCorpusTitles(); !reflect.DeepEqual(got, wantTitles) {
		t.Fatalf("RequiredBrowserCorpusTitles() = %#v, want %#v", got, wantTitles)
	}

	root := filepath.Join("..", "..", "..", "web", "e2e")
	for _, spec := range wantSpecs {
		if _, err := os.Stat(filepath.Join(root, spec)); err != nil {
			t.Fatalf("browser spec %s missing: %v", spec, err)
		}
	}
	payloads := make([]byte, 0)
	for _, spec := range wantSpecs {
		payload, err := os.ReadFile(filepath.Join(root, spec))
		if err != nil {
			t.Fatalf("read %s: %v", spec, err)
		}
		payloads = append(payloads, payload...)
		payloads = append(payloads, '\n')
	}
	for _, title := range wantTitles {
		if !bytes.Contains(payloads, []byte(title)) {
			t.Fatalf("browser corpus title %q is missing from inventoried specs", title)
		}
	}
}

func TestProductionSourcesRejectBrowserFixturesAndHelpers(t *testing.T) {
	t.Parallel()

	want := []string{
		"web/e2e/",
		"../../e2e/",
		"dashboardTestFixtures",
		"@axe-core/playwright",
		"coreRouteProfile",
		"vpsOverviewProfile",
	}
	if got := ProductionBundleForbiddenTokens(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ProductionBundleForbiddenTokens() = %#v, want %#v", got, want)
	}

	srcRoot := filepath.Join("..", "..", "..", "web", "src")
	err := filepath.WalkDir(srcRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".test.ts") || strings.HasSuffix(name, ".test.tsx") ||
			strings.HasSuffix(name, "_test.ts") || strings.HasSuffix(name, "_test.tsx") {
			return nil
		}
		switch filepath.Ext(name) {
		case ".ts", ".tsx", ".js", ".jsx", ".css":
		default:
			return nil
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			rel = path
		}
		if err := ScanProductionBundleSafe(payload); err != nil {
			t.Fatalf("%s contains a browser fixture or helper: %v", rel, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk web/src: %v", err)
	}
}

func TestRecordsCapacityAndBrowserScriptsOwnInventoriedCorpus(t *testing.T) {
	t.Parallel()

	capacity, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "run-records-capacity.sh"))
	if err != nil {
		t.Fatalf("read run-records-capacity.sh: %v", err)
	}
	if err := ScanContentSafe(capacity); err != nil {
		t.Fatalf("run-records-capacity.sh leaked: %v", err)
	}
	capacityText := string(capacity)
	for _, want := range append(append([]string{
		"--profile",
		"local",
		"s3",
		"scripts/test-record-platform-integration.sh",
		"HOUFENG_POSTGRES_INTEGRATION=1",
		"HOUFENG_ACTIVITY_PERF_SCALE",
		"--- SKIP:",
		"go test",
	}, RequiredCapacityCorpusTests()...), RequiredCapacityPostgresTests()...) {
		if !strings.Contains(capacityText, want) {
			t.Fatalf("run-records-capacity.sh missing %q", want)
		}
	}

	browser, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "run-records-browser.sh"))
	if err != nil {
		t.Fatalf("read run-records-browser.sh: %v", err)
	}
	if err := ScanContentSafe(browser); err != nil {
		t.Fatalf("run-records-browser.sh leaked: %v", err)
	}
	browserText := string(browser)
	for _, want := range append([]string{
		"npm --prefix web",
		"test:e2e",
		"web/dist",
		"PLAYWRIGHT_BROWSERS_PATH",
		"dashboardTestFixtures",
		"coreRouteProfile",
	}, RequiredBrowserSpecFiles()...) {
		if !strings.Contains(browserText, want) {
			t.Fatalf("run-records-browser.sh missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"AdmissionGateFunc(",
		"houfeng-record-platform-admin",
		"postgres://houfeng",
		"command -v docker",
	} {
		if strings.Contains(browserText, forbidden) {
			t.Fatalf("run-records-browser.sh must not contain %q", forbidden)
		}
	}
}

func requireTestsPresent(t *testing.T, names []string) {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	present := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == "node_modules" || name == "web" || name == "bin" || name == "dist" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, testName := range names {
			if bytes.Contains(payload, []byte("func "+testName+"(")) {
				present[testName] = path
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk tests: %v", err)
	}
	for _, testName := range names {
		if present[testName] == "" {
			t.Fatalf("capacity corpus test %s is missing from the tree", testName)
		}
	}
}
