package ipquality

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"houfeng/internal/contracts/agentapi"
)

const detectorUpstreamCommit = "2384a67c756eb35231f5982b34731e522be3653e"
const detectorForkRevision = 1

// Embedded corresponding source and license travel with every Agent binary.
//
//go:embed detector_assets/*
var detectorAssets embed.FS

type detectorDiagnostic struct {
	Endpoint   string `json:"endpoint"`
	Outcome    string `json:"outcome"`
	ErrorCode  string `json:"error_code"`
	Region     string `json:"region"`
	HTTPStatus int    `json:"http_status"`
}
type detectorGeoSample struct {
	Provider              string `json:"provider"`
	CountryCode           string `json:"country_code"`
	RegisteredCountryCode string `json:"registered_country_code"`
	AbuseContactCountry   string `json:"abuse_contact_country"`
	City                  string `json:"city"`
	Region                string `json:"region"`
	PostalCode            string `json:"postal_code"`
	Timezone              string `json:"timezone"`
	ASN                   string `json:"asn"`
	ASName                string `json:"as_name"`
	UsageType             string `json:"usage_type"`
	CompanyType           string `json:"company_type"`
}
type detectorResult struct {
	Status     string              `json:"status"`
	Region     string              `json:"region"`
	UnlockType string              `json:"unlock_type"`
	Rule       string              `json:"rule"`
	ErrorCode  string              `json:"error_code"`
	Samples    []detectorGeoSample `json:"samples"`
}
type detectorReport struct {
	SchemaRevision int                  `json:"schema_revision"`
	Module         string               `json:"module"`
	TargetIP       string               `json:"target_ip"`
	IPVersion      int                  `json:"ip_version"`
	UpstreamCommit string               `json:"upstream_commit"`
	ForkRevision   int                  `json:"fork_revision"`
	Result         detectorResult       `json:"result"`
	Diagnostics    []detectorDiagnostic `json:"diagnostics"`
}

type detectorFailure string

func (e detectorFailure) Error() string { return string(e) }

// Dependencies are resolved only in trusted standard locations, never the
// Agent's inherited PATH. Bash >=4 is checked again by the fixed module itself.
func detectorDependency(name string) (string, error) {
	for _, dir := range []string{"/usr/bin", "/bin"} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return path, nil
		}
	}
	return "", detectorFailure("detector_dependency_missing")
}

// Do not embed bytes.Buffer: its promoted io.ReaderFrom would let io.Copy in
// os/exec bypass Write and therefore bypass both output limits.
type detectorOutput struct {
	buffer   bytes.Buffer
	limit    int
	cancel   context.CancelFunc
	mu       sync.Mutex
	exceeded bool
}

func (w *detectorOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(p) > w.limit-w.buffer.Len() {
		w.exceeded = true
		w.cancel()
		return 0, errors.New("detector output limit")
	}
	return w.buffer.Write(p)
}
func (w *detectorOutput) Len() int      { w.mu.Lock(); defer w.mu.Unlock(); return w.buffer.Len() }
func (w *detectorOutput) Bytes() []byte { w.mu.Lock(); defer w.mu.Unlock(); return w.buffer.Bytes() }

func runDetector(ctx context.Context, module, target string, family int) (detectorReport, error) {
	helper, err := os.Executable()
	if err != nil {
		return detectorReport{}, detectorFailure("detector_fetch_failed")
	}
	return runDetectorWithHelper(ctx, module, target, family, helper)
}

// The helper argument is an internal dependency boundary used by tests to run
// the actual embedded Bash against static, extracted HTTP fixtures. Production
// always supplies os.Executable(); no environment/CLI fixture switch exists.
func runDetectorWithHelper(ctx context.Context, module, target string, family int, helper string) (detectorReport, error) {
	script, err := detectorAssets.ReadFile("detector_assets/detector.sh")
	if err != nil {
		return detectorReport{}, err
	}
	return runDetectorProgram(ctx, module, target, family, helper, script)
}

func runDetectorProgram(ctx context.Context, module, target string, family int, helper string, script []byte) (detectorReport, error) {
	var report detectorReport
	if ctx.Err() != nil {
		return report, ctx.Err()
	}
	if !validDetectorTarget(target, family) {
		return report, detectorFailure("missing_target_ip")
	}
	switch module {
	case "geo", "netflix", "youtube-premium", "amazon-prime-video", "chatgpt":
	default:
		return report, detectorFailure("unsupported_service")
	}
	bash, err := detectorDependency("bash")
	if err != nil {
		return report, err
	}
	if _, err = detectorDependency("jq"); err != nil {
		return report, err
	}
	dir, err := os.MkdirTemp("", "houfeng-ipquality-")
	if err != nil {
		return report, detectorFailure("detector_fetch_failed")
	}
	defer os.RemoveAll(dir)
	if err = os.WriteFile(filepath.Join(dir, "detector.sh"), script, 0600); err != nil {
		return report, err
	}
	// The local country asset is retained for corresponding-source provenance;
	// no runtime floating download or system package installation is permitted.
	iso, err := detectorAssets.ReadFile("detector_assets/iso3166.json")
	if err != nil {
		return report, err
	}
	if err = os.WriteFile(filepath.Join(dir, "iso3166.json"), iso, 0600); err != nil {
		return report, err
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	stdout := &detectorOutput{limit: 128 * 1024, cancel: cancel}
	stderr := &detectorOutput{limit: 64 * 1024, cancel: cancel}
	cmd := exec.CommandContext(child, bash, "--noprofile", "--norc", "detector.sh", module, target, itoa(family), helper)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "HOME=" + dir, "TMPDIR=" + dir}
	cmd.Stdin = nil
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = time.Second
	if err = cmd.Start(); err != nil {
		return report, detectorFailure("detector_dependency_missing")
	}
	err = cmd.Wait()
	// Also reap descendants after a nominal shell exit, before removing its cwd.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if stdout.exceeded || stderr.exceeded {
		return report, detectorFailure("detector_output_limit")
	}
	if ctx.Err() != nil {
		return report, ctx.Err()
	}
	if err != nil {
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 70 {
			return report, detectorFailure("detector_dependency_missing")
		}
		return report, detectorFailure("detector_invalid_output")
	}
	if err = decodeDetectorReport(stdout.Bytes(), module, target, family, &report); err != nil {
		return detectorReport{}, err
	}
	return report, nil
}
func itoa(family int) string {
	if family == 6 {
		return "6"
	}
	return "4"
}
func safeDetectorToken(value string) bool {
	if len(value) > 96 || value == "null" {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
func safeDetectorCountry(value string) bool { return value == "" || serviceCountry(value) == value }
func cleanDetectorText(value string) string {
	value = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value))
	if value == "null" {
		return ""
	}
	return value
}
func decodeDetectorReport(body []byte, module, target string, family int, report *detectorReport) error {
	invalid := detectorFailure("detector_invalid_output")
	if len(body) > 128*1024 {
		return invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(report) != nil || decoder.Decode(new(any)) != io.EOF {
		return invalid
	}
	if report.SchemaRevision != 1 || report.ForkRevision != detectorForkRevision || report.UpstreamCommit != detectorUpstreamCommit || report.Module != module || report.TargetIP != target || report.IPVersion != family || len(report.Diagnostics) > 3 || len(report.Result.Samples) > 2 {
		return invalid
	}
	r := &report.Result
	switch r.Status {
	case "unlocked", "partial", "blocked", "unknown":
	default:
		return invalid
	}
	if !safeDetectorCountry(r.Region) || !safeDetectorToken(r.Rule) || !safeDetectorToken(r.UnlockType) || !safeDetectorToken(r.ErrorCode) {
		return invalid
	}
	if module != "geo" && (len(r.Samples) > 0 || r.Status == "unknown" && r.ErrorCode == "" || r.Status != "unknown" && (r.ErrorCode != "" || r.Rule == "")) {
		return invalid
	}
	seen := map[string]bool{}
	if report.Diagnostics == nil || r.Samples == nil {
		return invalid
	}
	switch module {
	case "netflix":
		if len(report.Diagnostics) < 2 || !hasDetectorEndpoint(report.Diagnostics, "netflix-81280792") || !hasDetectorEndpoint(report.Diagnostics, "netflix-70143836") {
			return invalid
		}
	case "chatgpt":
		if len(report.Diagnostics) != 2 || !hasDetectorEndpoint(report.Diagnostics, "openai-cookie") || !hasDetectorEndpoint(report.Diagnostics, "openai-ios") {
			return invalid
		}
	case "geo":
		if len(report.Diagnostics) < 1 || len(report.Diagnostics) > 2 || !hasDetectorEndpoint(report.Diagnostics, "geo-maxmind") {
			return invalid
		}
	default:
		if len(report.Diagnostics) != 1 || report.Diagnostics[0].Endpoint != module {
			return invalid
		}
	}
	for i := range report.Diagnostics {
		d := &report.Diagnostics[i]
		if _, ok := detectorEndpointFor(d.Endpoint, target); !ok || seen[d.Endpoint] || !safeDetectorCountry(d.Region) || !safeDetectorToken(d.ErrorCode) || d.HTTPStatus < 0 || d.HTTPStatus > 599 {
			return invalid
		}
		seen[d.Endpoint] = true
		switch d.Outcome {
		case "positive", "restricted", "unknown":
		default:
			return invalid
		}
	}
	for _, d := range report.Diagnostics {
		valid := d.Endpoint == module
		switch module {
		case "netflix":
			valid = strings.HasPrefix(d.Endpoint, "netflix-")
		case "chatgpt":
			valid = d.Endpoint == "openai-cookie" || d.Endpoint == "openai-ios"
		case "geo":
			valid = d.Endpoint == "geo-maxmind" || d.Endpoint == "geo-ipinfo"
		}
		if !valid {
			return invalid
		}
	}
	providers := map[string]bool{}
	for i := range r.Samples {
		s := &r.Samples[i]
		if module != "geo" || s.Provider != "maxmind" && s.Provider != "ipinfo" || providers[s.Provider] || !safeDetectorCountry(s.CountryCode) || !safeDetectorCountry(s.RegisteredCountryCode) || !safeDetectorCountry(s.AbuseContactCountry) || s.Provider == "ipinfo" && s.RegisteredCountryCode != "" {
			return invalid
		}
		providers[s.Provider] = true
		fields := []*string{&s.City, &s.Region, &s.PostalCode, &s.Timezone, &s.ASN, &s.ASName, &s.UsageType, &s.CompanyType}
		for _, field := range fields {
			if len(*field) > 512 {
				return invalid
			}
			*field = cleanDetectorText(*field)
		}
	}
	return nil
}
func hasDetectorEndpoint(ds []detectorDiagnostic, id string) bool {
	for _, d := range ds {
		if d.Endpoint == id {
			return true
		}
	}
	return false
}

func detectorErrorCode(err error) string {
	var failure detectorFailure
	if errors.As(err, &failure) {
		return string(failure)
	}
	return errorCodeForHTTPError(err)
}
func scriptService(service string) bool {
	switch service {
	case "netflix", "youtube-premium", "amazon-prime-video", "chatgpt":
		return true
	}
	return false
}
func scriptServiceOutcome(ctx context.Context, service, target string, family int) serviceProbeOutcome {
	start := time.Now()
	source, _ := defaultServiceSource(service)
	report, err := runDetector(ctx, service, target, family)
	evidence := map[string]any{"evidence_kind": "ipquality_script", "probe_revision": 5, "ip_version": family, "schema_revision": 1, "upstream_commit": detectorUpstreamCommit, "fork_revision": detectorForkRevision}
	var outcome serviceProbeOutcome
	if err != nil {
		code := detectorErrorCode(err)
		outcome = serviceFailure(service, source, code, serviceErrorSummary(code), elapsedMillis(start), nil)
	} else {
		r := report.Result
		evidence["rule"] = r.Rule
		evidence["diagnostics"] = report.Diagnostics
		if r.Status == "unknown" {
			outcome = serviceFailure(service, source, r.ErrorCode, serviceErrorSummary(r.ErrorCode), elapsedMillis(start), nil)
		} else {
			outcome.Result = agentapi.IPQualityServiceUnlockPayload{Service: service, Source: source, Status: r.Status, ProbeStatus: sourceStatusSuccess, Region: r.Region, UnlockType: r.UnlockType, LatencyMS: elapsedMillis(start)}
		}
	}
	outcome.Raw = extraJSON(evidence)
	outcome.Result.ExtraJSON = outcome.Raw
	return outcome
}

func collectScriptGeo(ctx context.Context, targetIP string, family int, observedAt time.Time, providerSlots chan struct{}) []providerSourceOutcome {
	select {
	case providerSlots <- struct{}{}:
		defer func() { <-providerSlots }()
	case <-ctx.Done():
		return nil
	}
	start := time.Now()
	report, err := runDetector(ctx, "geo", targetIP, family)
	if err != nil {
		return []providerSourceOutcome{sourceFailure("maxmind", sourceTypeDefault, detectorErrorCode(err), "", elapsedMillis(start), nil)}
	}
	return scriptGeoOutcomes(report, targetIP, observedAt, start)
}

func scriptGeoOutcomes(report detectorReport, targetIP string, observedAt, start time.Time) []providerSourceOutcome {
	outcomes := make([]providerSourceOutcome, 0, 2)
	for _, sample := range report.Result.Samples {
		details := &agentapi.IPQualityProviderDetails{QueriedIP: targetIP, ObservedAt: observedAt.UTC(), Method: "agent_public", APITier: "anonymous", CountryCode: sample.CountryCode, RegisteredCountryCode: sample.RegisteredCountryCode, City: sample.City, Region: sample.Region, PostalCode: sample.PostalCode, Timezone: sample.Timezone, ASN: sample.ASN, ASName: sample.ASName, FieldStates: map[string]string{}}
		if number, err := strconv.ParseUint(strings.TrimPrefix(strings.ToUpper(sample.ASN), "AS"), 10, 32); err == nil && number > 0 {
			details.ASN = "AS" + strconv.FormatUint(number, 10)
		} else if sample.ASN != "" {
			details.ASN = ""
			details.FieldStates["asn"] = "error"
		}
		if err := agentapi.ValidateIPQualityProviderDetails(sample.Provider, details, agentapi.IPQualityProductRevision, targetIP); err != nil {
			outcomes = append(outcomes, sourceFailure(sample.Provider, sourceTypeDefault, "invalid_response", "", elapsedMillis(start), nil))
			continue
		}
		evidence := map[string]any{"evidence_kind": "ipquality_script", "schema_revision": 1, "upstream_commit": detectorUpstreamCommit, "fork_revision": detectorForkRevision, "capability": "geo_only", "diagnostics": report.Diagnostics}
		if sample.Provider == "ipinfo" {
			evidence["geo_classification"] = map[string]any{"basis": "abuse_contact_country", "country_code": sample.CountryCode, "comparison_country_code": sample.AbuseContactCountry, "upstream_commit": detectorUpstreamCommit, "fork_revision": detectorForkRevision}
		}
		raw := extraJSON(evidence)
		outcomes = append(outcomes, providerSourceOutcome{IPAddress: targetIP, Raw: raw, Result: agentapi.IPQualityProviderResultPayload{Provider: sample.Provider, SourceType: sourceTypeDefault, Status: sourceStatusSuccess, RegionCode: sample.CountryCode, UsageType: sample.UsageType, CompanyType: sample.CompanyType, LatencyMS: elapsedMillis(start), Details: details, ExtraJSON: raw}})
	}
	// Failed attempts remain diagnostics and never erase a successful fallback.
	successful := map[string]bool{}
	for _, outcome := range outcomes {
		successful[outcome.Result.Provider] = true
	}
	for _, d := range report.Diagnostics {
		provider := "maxmind"
		if d.Endpoint == "geo-ipinfo" {
			provider = "ipinfo"
		}
		if !successful[provider] {
			raw := extraJSON(map[string]any{"evidence_kind": "ipquality_script", "diagnostics": []detectorDiagnostic{d}})
			outcomes = append(outcomes, sourceFailure(provider, sourceTypeDefault, firstNonEmpty(d.ErrorCode, "invalid_response"), "", elapsedMillis(start), raw))
		}
	}
	return outcomes
}
