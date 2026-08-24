package recordbackup

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

type recordsRunnerLifecycleSpec struct {
	name            string
	relativePath    string
	args            []string
	runnerKind      string
	outer           bool
	workspacePrefix string
	wantContainers  int
	wantVolumes     int
	failingName     string
}

type recordsRunnerFakeToolchain struct {
	bin       string
	tmpParent string
	dockerLog string
	rmLog     string
}

type recordsRunnerSignalProcess struct {
	command *exec.Cmd
	stdout  bytes.Buffer
	stderr  bytes.Buffer
}

type recordsRunnerRunOptions struct {
	bodyStatus             int
	bodyStdout             string
	bodyStderr             string
	failContainer          string
	failVolume             bool
	failVolumeCreate       bool
	foreignVolumeCollision bool
	failWorkspace          string
	failSetup              string
	failStdoutTee          bool
	failStderrTee          bool
	failSkipScan           string
	foreignCollision       string
	replacedContainer      string
	ignoreSignalBody       bool
	cleanupDelay           string
	cleanupReady           string
	signalBodyDuration     string
	keep                   bool
	runID                  string
	omitRunID              bool
	replacedVolume         bool
}

func TestRecordsRunnerLifecycleCleanupFailuresAreVisibleAndDoNotShortCircuit(t *testing.T) {
	t.Parallel()

	integration := recordsRunnerLifecycleSpecByName(t, "integration s3")
	integrationLocal := recordsRunnerLifecycleSpecByName(t, "integration local")
	recovery := recordsRunnerLifecycleSpecByName(t, "recovery s3")
	recoveryLocal := recordsRunnerLifecycleSpecByName(t, "recovery local")
	platform := recordsRunnerLifecycleSpecByName(t, "record platform child")
	tests := []struct {
		name             string
		spec             recordsRunnerLifecycleSpec
		options          recordsRunnerRunOptions
		wantDiagnostic   string
		wantContainerRMs int
		wantVolumeRMs    int
		wantWorkspaceRMs int
	}{
		{
			name:             "outer container cleanup",
			spec:             integration,
			options:          recordsRunnerRunOptions{failContainer: "minio"},
			wantDiagnostic:   "records runner cleanup failed: container ",
			wantContainerRMs: 5,
			wantVolumeRMs:    1,
			wantWorkspaceRMs: 2,
		},
		{
			name:             "recovery outer container cleanup",
			spec:             recovery,
			options:          recordsRunnerRunOptions{failContainer: "minio"},
			wantDiagnostic:   "records runner cleanup failed: container ",
			wantContainerRMs: 5,
			wantVolumeRMs:    1,
			wantWorkspaceRMs: 2,
		},
		{
			name:             "outer volume cleanup",
			spec:             integration,
			options:          recordsRunnerRunOptions{failVolume: true},
			wantDiagnostic:   "records runner cleanup failed: volume ",
			wantContainerRMs: 5,
			wantVolumeRMs:    1,
			wantWorkspaceRMs: 2,
		},
		{
			name:             "recovery outer volume cleanup",
			spec:             recovery,
			options:          recordsRunnerRunOptions{failVolume: true},
			wantDiagnostic:   "records runner cleanup failed: volume ",
			wantContainerRMs: 5,
			wantVolumeRMs:    1,
			wantWorkspaceRMs: 2,
		},
		{
			name:             "outer workspace cleanup",
			spec:             integration,
			options:          recordsRunnerRunOptions{failWorkspace: "houfeng-records-integration."},
			wantDiagnostic:   "records runner cleanup failed: workspace ",
			wantContainerRMs: 5,
			wantVolumeRMs:    1,
			wantWorkspaceRMs: 2,
		},
		{
			name:             "direct child container cleanup",
			spec:             platform,
			options:          recordsRunnerRunOptions{failContainer: "houfeng-rp-app-"},
			wantDiagnostic:   "records runner cleanup failed: container ",
			wantContainerRMs: 4,
			wantWorkspaceRMs: 1,
		},
		{
			name:             "direct child workspace cleanup",
			spec:             platform,
			options:          recordsRunnerRunOptions{failWorkspace: "houfeng-record-platform."},
			wantDiagnostic:   "records runner cleanup failed: workspace ",
			wantContainerRMs: 4,
			wantWorkspaceRMs: 1,
		},
		{
			name:             "integration local nested container cleanup",
			spec:             integrationLocal,
			options:          recordsRunnerRunOptions{failContainer: "houfeng-rp-app-"},
			wantDiagnostic:   "records runner cleanup failed: container ",
			wantContainerRMs: 4,
			wantWorkspaceRMs: 2,
		},
		{
			name:             "recovery local nested container cleanup",
			spec:             recoveryLocal,
			options:          recordsRunnerRunOptions{failContainer: "houfeng-rp-app-"},
			wantDiagnostic:   "records runner cleanup failed: container ",
			wantContainerRMs: 4,
			wantWorkspaceRMs: 2,
		},
		{
			name:             "integration local outer workspace cleanup",
			spec:             integrationLocal,
			options:          recordsRunnerRunOptions{failWorkspace: "houfeng-records-integration."},
			wantDiagnostic:   "records runner cleanup failed: workspace ",
			wantContainerRMs: 4,
			wantWorkspaceRMs: 2,
		},
		{
			name:             "recovery local outer workspace cleanup",
			spec:             recoveryLocal,
			options:          recordsRunnerRunOptions{failWorkspace: "houfeng-records-recovery."},
			wantDiagnostic:   "records runner cleanup failed: workspace ",
			wantContainerRMs: 4,
			wantWorkspaceRMs: 2,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fake := newRecordsRunnerFakeToolchain(t)
			code, _, stderr := runRecordsRunner(t, test.spec, fake, test.options)
			if code != 1 {
				t.Fatalf("cleanup failure exit code = %d, stderr %q, want 1", code, stderr)
			}
			if !strings.Contains(stderr, test.wantDiagnostic) {
				t.Fatalf("cleanup failure stderr = %q, want %q", stderr, test.wantDiagnostic)
			}
			log := readRecordsRunnerLog(t, fake.dockerLog)
			if got := countRecordsRunnerLogLines(log, "rm\t"); got != test.wantContainerRMs {
				t.Fatalf("container cleanup attempts = %d, log %q, want %d", got, log, test.wantContainerRMs)
			}
			if got := countRecordsRunnerLogLines(log, "volume-rm\t"); got != test.wantVolumeRMs {
				t.Fatalf("volume cleanup attempts = %d, log %q, want %d", got, log, test.wantVolumeRMs)
			}
			rmLog := readRecordsRunnerLog(t, fake.rmLog)
			if got := countRecordsRunnerLogLines(rmLog, "workspace-rm\t"); got != test.wantWorkspaceRMs {
				t.Fatalf("workspace cleanup attempts = %d, log %q, want %d", got, rmLog, test.wantWorkspaceRMs)
			}
		})
	}
}

func TestRecordsRunnerLifecycleBodyFailureKeepsPriorityOverCleanupFailure(t *testing.T) {
	t.Parallel()

	for _, spec := range recordsRunnerLifecycleSpecs(t) {
		spec := spec
		t.Run(spec.name, func(t *testing.T) {
			t.Parallel()
			fake := newRecordsRunnerFakeToolchain(t)
			code, _, stderr := runRecordsRunner(t, spec, fake, recordsRunnerRunOptions{
				bodyStatus:    23,
				failContainer: spec.failingName,
			})
			if code != 23 {
				t.Fatalf("body plus cleanup failure exit code = %d, stderr %q, want 23", code, stderr)
			}
			if !strings.Contains(stderr, "records runner cleanup failed: container ") {
				t.Fatalf("body plus cleanup failure stderr = %q, want cleanup diagnostic", stderr)
			}
			assertRecordsRunnerCleanupAttempted(t, spec, fake)
		})
	}
}

func TestRecordsRunnerLifecycleSuccessUsesExactLabeledResourcesAndLeavesNoTempState(t *testing.T) {
	t.Parallel()

	for _, spec := range recordsRunnerLifecycleSpecs(t) {
		spec := spec
		t.Run(spec.name, func(t *testing.T) {
			t.Parallel()
			fake := newRecordsRunnerFakeToolchain(t)
			code, _, stderr := runRecordsRunner(t, spec, fake, recordsRunnerRunOptions{})
			if code != 0 {
				t.Fatalf("successful lifecycle exit code = %d, stderr %q, want 0", code, stderr)
			}
			assertRecordsRunnerResourceLifecycle(t, spec, fake, "records-test-run")
			assertRecordsRunnerTMPDirEmpty(t, fake)
		})
	}
}

func TestRecordsRunnerLifecycleRejectsSkipAndCleansResources(t *testing.T) {
	t.Parallel()

	for _, spec := range recordsRunnerLifecycleSpecs(t) {
		spec := spec
		for _, stream := range []string{"stdout", "stderr"} {
			stream := stream
			t.Run(spec.name+" "+stream, func(t *testing.T) {
				t.Parallel()
				options := recordsRunnerRunOptions{}
				if stream == "stdout" {
					options.bodyStdout = "--- SKIP: fake records lifecycle fixture\n"
				} else {
					options.bodyStderr = "--- SKIP: fake records lifecycle fixture\n"
				}
				fake := newRecordsRunnerFakeToolchain(t)
				code, stdout, stderr := runRecordsRunner(t, spec, fake, options)
				if code != 1 {
					t.Fatalf("skip lifecycle exit code = %d, stdout %q, stderr %q, want 1", code, stdout, stderr)
				}
				wantOutput := "skipped a test"
				if spec.wantVolumes == 0 {
					wantOutput = "--- SKIP: fake records lifecycle fixture"
				}
				if !strings.Contains(stdout+stderr, wantOutput) {
					t.Fatalf("skip lifecycle output = %q, want %q", stdout+stderr, wantOutput)
				}
				assertRecordsRunnerCleanupAttempted(t, spec, fake)
				assertRecordsRunnerTMPDirEmpty(t, fake)
			})
		}
	}
}

func TestRecordsRunnerLifecycleEvidenceTeeFailuresAreVisibleAndBodyKeepsPriority(t *testing.T) {
	t.Parallel()

	platform := recordsRunnerLifecycleSpecByName(t, "record platform child")
	tests := []struct {
		name           string
		options        recordsRunnerRunOptions
		wantStatus     int
		wantDiagnostic []string
		forbidSkip     bool
	}{
		{
			name:           "stdout tee",
			options:        recordsRunnerRunOptions{failStdoutTee: true},
			wantStatus:     1,
			wantDiagnostic: []string{"evidence sink failed: stdout tee status 41"},
		},
		{
			name:           "stderr tee",
			options:        recordsRunnerRunOptions{failStderrTee: true},
			wantStatus:     1,
			wantDiagnostic: []string{"evidence sink failed: stderr tee status 42"},
		},
		{
			name: "both tees are waited",
			options: recordsRunnerRunOptions{
				failStdoutTee: true,
				failStderrTee: true,
			},
			wantStatus: 1,
			wantDiagnostic: []string{
				"evidence sink failed: stdout tee status 41",
				"evidence sink failed: stderr tee status 42",
			},
		},
		{
			name: "body failure keeps priority over both tees",
			options: recordsRunnerRunOptions{
				bodyStatus:    23,
				failStdoutTee: true,
				failStderrTee: true,
			},
			wantStatus: 23,
			wantDiagnostic: []string{
				"evidence sink failed: stdout tee status 41",
				"evidence sink failed: stderr tee status 42",
			},
		},
		{
			name: "failed evidence is not scanned for skip",
			options: recordsRunnerRunOptions{
				bodyStdout:    "--- SKIP: untrusted partial evidence\n",
				failStdoutTee: true,
			},
			wantStatus:     1,
			wantDiagnostic: []string{"evidence sink failed: stdout tee status 41"},
			forbidSkip:     true,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fake := newRecordsRunnerFakeToolchain(t)
			code, stdout, stderr := runRecordsRunner(t, platform, fake, test.options)
			if code != test.wantStatus {
				t.Fatalf("tee failure exit code = %d, stdout %q, stderr %q, want %d", code, stdout, stderr, test.wantStatus)
			}
			for _, want := range test.wantDiagnostic {
				if !strings.Contains(stderr, want) {
					t.Fatalf("tee failure stderr = %q, want %q", stderr, want)
				}
			}
			if test.forbidSkip && strings.Contains(stderr, "skipped a test") {
				t.Fatalf("tee failure stderr = %q, must not trust incomplete evidence for skip scan", stderr)
			}
			assertRecordsRunnerCleanupAttempted(t, platform, fake)
			assertRecordsRunnerTMPDirEmpty(t, fake)
		})
	}
}

func TestRecordsRunnerLifecycleEvidenceScanFailureIsVisibleAndBodyKeepsPriority(t *testing.T) {
	t.Parallel()

	for _, spec := range recordsRunnerLifecycleSpecs(t) {
		spec := spec
		for _, bodyStatus := range []int{0, 23} {
			bodyStatus := bodyStatus
			t.Run(fmt.Sprintf("%s body %d", spec.name, bodyStatus), func(t *testing.T) {
				t.Parallel()
				fake := newRecordsRunnerFakeToolchain(t)
				code, stdout, stderr := runRecordsRunner(t, spec, fake, recordsRunnerRunOptions{
					bodyStatus:   bodyStatus,
					failSkipScan: spec.workspacePrefix,
				})
				wantStatus := 1
				if bodyStatus != 0 {
					wantStatus = bodyStatus
				}
				if code != wantStatus {
					t.Fatalf("evidence scan failure exit code = %d, stdout %q, stderr %q, want %d", code, stdout, stderr, wantStatus)
				}
				if !strings.Contains(stderr, "records runner evidence scan failed: status 47") {
					t.Fatalf("evidence scan failure stderr = %q, want explicit status 47 diagnostic", stderr)
				}
				assertRecordsRunnerCleanupAttempted(t, spec, fake)
				assertRecordsRunnerTMPDirEmpty(t, fake)
			})
		}
	}
}

func TestRecordsRunnerLifecycleSetupFailureCleansAlreadyCreatedResources(t *testing.T) {
	t.Parallel()

	for _, spec := range recordsRunnerLifecycleSpecs(t) {
		spec := spec
		t.Run(spec.name, func(t *testing.T) {
			t.Parallel()
			fake := newRecordsRunnerFakeToolchain(t)
			code, _, stderr := runRecordsRunner(t, spec, fake, recordsRunnerRunOptions{failSetup: spec.failingName})
			if code != 37 {
				t.Fatalf("setup failure exit code = %d, stderr %q, want 37", code, stderr)
			}
			log := readRecordsRunnerLog(t, fake.dockerLog)
			if got := countRecordsRunnerLogLines(log, "rm\t"); got != 1 {
				t.Fatalf("setup failure container cleanup attempts = %d, log %q, want 1", got, log)
			}
			if !strings.Contains(log, "container-inspect\t") || !strings.Contains(log, "rm\tfake-id-") {
				t.Fatalf("setup failure log = %q, want owner inspection and immutable-id cleanup", log)
			}
			if spec.wantVolumes == 1 && countRecordsRunnerLogLines(log, "volume-rm\t") != 1 {
				t.Fatalf("setup failure log = %q, want created volume cleanup", log)
			}
			wantWorkspaceRMs := 1
			if spec.outer && spec.wantVolumes == 0 {
				wantWorkspaceRMs = 2
			}
			if countRecordsRunnerLogLines(readRecordsRunnerLog(t, fake.rmLog), "workspace-rm\t") != wantWorkspaceRMs {
				t.Fatalf("setup failure workspace cleanup log = %q, want %d attempts", readRecordsRunnerLog(t, fake.rmLog), wantWorkspaceRMs)
			}
			assertRecordsRunnerTMPDirEmpty(t, fake)
		})
	}
}

func TestRecordsRunnerLifecycleForeignContainerCollisionIsNeverRemoved(t *testing.T) {
	t.Parallel()

	for _, spec := range recordsRunnerLifecycleSpecs(t) {
		spec := spec
		t.Run(spec.name, func(t *testing.T) {
			t.Parallel()
			fake := newRecordsRunnerFakeToolchain(t)
			code, _, stderr := runRecordsRunner(t, spec, fake, recordsRunnerRunOptions{
				foreignCollision: spec.failingName,
			})
			if code != 125 {
				t.Fatalf("foreign collision exit code = %d, stderr %q, want 125", code, stderr)
			}
			log := readRecordsRunnerLog(t, fake.dockerLog)
			if got := countRecordsRunnerLogLines(log, "rm\t"); got != 0 {
				t.Fatalf("foreign collision removed %d container(s), log %q, want none", got, log)
			}
			if got := countRecordsRunnerLogLines(log, "volume-rm\t"); got != spec.wantVolumes {
				t.Fatalf("foreign collision volume cleanup attempts = %d, log %q, want %d", got, log, spec.wantVolumes)
			}
			workspaceRMs := countRecordsRunnerLogLines(readRecordsRunnerLog(t, fake.rmLog), "workspace-rm\t")
			wantWorkspaceRMs := 1
			if spec.outer && spec.wantVolumes == 0 {
				wantWorkspaceRMs = 2
			}
			if workspaceRMs != wantWorkspaceRMs {
				t.Fatalf("foreign collision workspace cleanup attempts = %d, want %d", workspaceRMs, wantWorkspaceRMs)
			}
			assertRecordsRunnerTMPDirEmpty(t, fake)
		})
	}
}

func TestRecordsRunnerLifecycleReplacedContainerFailsClosedAndContinuesCleanup(t *testing.T) {
	t.Parallel()

	for _, spec := range recordsRunnerLifecycleSpecs(t) {
		spec := spec
		t.Run(spec.name, func(t *testing.T) {
			t.Parallel()
			fake := newRecordsRunnerFakeToolchain(t)
			code, _, stderr := runRecordsRunner(t, spec, fake, recordsRunnerRunOptions{
				replacedContainer: spec.failingName,
			})
			if code != 1 {
				t.Fatalf("replaced container cleanup exit code = %d, stderr %q, want 1", code, stderr)
			}
			if !strings.Contains(stderr, "skipped unowned container candidate") {
				t.Fatalf("replaced container cleanup stderr = %q, want ownership diagnostic", stderr)
			}
			log := readRecordsRunnerLog(t, fake.dockerLog)
			if strings.Contains(log, "rm\tfake-foreign-id") {
				t.Fatalf("replaced container cleanup log = %q, must not remove foreign replacement", log)
			}
			if got := countRecordsRunnerLogLines(log, "rm\t"); got != spec.wantContainers-1 {
				t.Fatalf("replaced container cleanup attempts = %d, log %q, want %d remaining owned containers", got, log, spec.wantContainers-1)
			}
			if got := countRecordsRunnerLogLines(log, "volume-rm\t"); got != spec.wantVolumes {
				t.Fatalf("replaced container volume cleanup attempts = %d, log %q, want %d", got, log, spec.wantVolumes)
			}
			wantWorkspaceRMs := 1
			if spec.outer {
				wantWorkspaceRMs = 2
			}
			if got := countRecordsRunnerLogLines(readRecordsRunnerLog(t, fake.rmLog), "workspace-rm\t"); got != wantWorkspaceRMs {
				t.Fatalf("replaced container workspace cleanup attempts = %d, want %d", got, wantWorkspaceRMs)
			}
			assertRecordsRunnerTMPDirEmpty(t, fake)
		})
	}
}

func TestRecordsRunnerLifecyclePartialVolumeCreateFailureCleansOwnedVolume(t *testing.T) {
	t.Parallel()

	for _, spec := range recordsRunnerLifecycleSpecs(t) {
		if spec.wantVolumes == 0 {
			continue
		}
		spec := spec
		t.Run(spec.name, func(t *testing.T) {
			t.Parallel()
			fake := newRecordsRunnerFakeToolchain(t)
			code, _, stderr := runRecordsRunner(t, spec, fake, recordsRunnerRunOptions{failVolumeCreate: true})
			if code != 38 {
				t.Fatalf("partial volume create exit code = %d, stderr %q, want 38", code, stderr)
			}
			log := readRecordsRunnerLog(t, fake.dockerLog)
			if got := countRecordsRunnerLogLines(log, "volume-inspect\t"); got != 1 {
				t.Fatalf("partial volume create inspect attempts = %d, log %q, want 1", got, log)
			}
			if got := countRecordsRunnerLogLines(log, "volume-rm\t"); got != 1 {
				t.Fatalf("partial volume create cleanup attempts = %d, log %q, want 1", got, log)
			}
			if got := countRecordsRunnerLogLines(log, "run\t"); got != 0 {
				t.Fatalf("partial volume create started %d containers, log %q, want 0", got, log)
			}
			if got := countRecordsRunnerLogLines(readRecordsRunnerLog(t, fake.rmLog), "workspace-rm\t"); got != 1 {
				t.Fatalf("partial volume create workspace cleanup attempts = %d, want 1", got)
			}
			assertRecordsRunnerTMPDirEmpty(t, fake)
		})
	}
}

func TestRecordsRunnerLifecycleForeignVolumeCollisionIsNeverRemoved(t *testing.T) {
	t.Parallel()

	for _, spec := range recordsRunnerLifecycleSpecs(t) {
		if spec.wantVolumes == 0 {
			continue
		}
		spec := spec
		t.Run(spec.name, func(t *testing.T) {
			t.Parallel()
			fake := newRecordsRunnerFakeToolchain(t)
			code, _, stderr := runRecordsRunner(t, spec, fake, recordsRunnerRunOptions{foreignVolumeCollision: true})
			if code != 1 {
				t.Fatalf("foreign volume collision exit code = %d, stderr %q, want 1", code, stderr)
			}
			log := readRecordsRunnerLog(t, fake.dockerLog)
			if got := countRecordsRunnerLogLines(log, "volume-rm\t"); got != 0 {
				t.Fatalf("foreign volume collision removed %d volume(s), log %q, want none", got, log)
			}
			if got := countRecordsRunnerLogLines(log, "run\t"); got != 0 {
				t.Fatalf("foreign volume collision started %d containers, log %q, want 0", got, log)
			}
			assertRecordsRunnerTMPDirEmpty(t, fake)
		})
	}
}

func TestRecordsRunnerLifecycleReplacedVolumeFailsClosedAfterContainerCleanup(t *testing.T) {
	t.Parallel()

	for _, spec := range recordsRunnerLifecycleSpecs(t) {
		if spec.wantVolumes == 0 {
			continue
		}
		spec := spec
		t.Run(spec.name, func(t *testing.T) {
			t.Parallel()
			fake := newRecordsRunnerFakeToolchain(t)
			code, _, stderr := runRecordsRunner(t, spec, fake, recordsRunnerRunOptions{replacedVolume: true})
			if code != 1 {
				t.Fatalf("replaced volume cleanup exit code = %d, stderr %q, want 1", code, stderr)
			}
			if !strings.Contains(stderr, "volume ownership verification failed: unowned candidate") {
				t.Fatalf("replaced volume cleanup stderr = %q, want ownership diagnostic", stderr)
			}
			log := readRecordsRunnerLog(t, fake.dockerLog)
			if got := countRecordsRunnerLogLines(log, "rm\t"); got != spec.wantContainers {
				t.Fatalf("replaced volume container cleanup attempts = %d, log %q, want %d", got, log, spec.wantContainers)
			}
			if got := countRecordsRunnerLogLines(log, "volume-rm\t"); got != 0 {
				t.Fatalf("replaced volume cleanup attempts = %d, log %q, want none", got, log)
			}
			if got := countRecordsRunnerLogLines(readRecordsRunnerLog(t, fake.rmLog), "workspace-rm\t"); got != 2 {
				t.Fatalf("replaced volume workspace cleanup attempts = %d, want 2", got)
			}
			assertRecordsRunnerTMPDirEmpty(t, fake)
		})
	}
}

func TestRecordsRunnerLifecycleSignalsUseTheSameCompleteCleanup(t *testing.T) {
	t.Parallel()

	signalCases := []struct {
		name         string
		signal       syscall.Signal
		wantStatus   int
		processGroup bool
		ignoreBody   bool
	}{
		{name: "parent only INT", signal: syscall.SIGINT, wantStatus: 130},
		{name: "parent only TERM", signal: syscall.SIGTERM, wantStatus: 143},
		{name: "process group INT", signal: syscall.SIGINT, wantStatus: 130, processGroup: true},
		{name: "process group TERM", signal: syscall.SIGTERM, wantStatus: 143, processGroup: true},
		{name: "parent only TERM ignored body", signal: syscall.SIGTERM, wantStatus: 143, ignoreBody: true},
	}
	for _, spec := range recordsRunnerLifecycleSpecs(t) {
		spec := spec
		t.Run(spec.name, func(t *testing.T) {
			for _, signalCase := range signalCases {
				signalCase := signalCase
				t.Run(signalCase.name, func(t *testing.T) {
					fake := newRecordsRunnerFakeToolchain(t)
					process := startRecordsRunnerSignalProcess(t, spec, fake, recordsRunnerRunOptions{ignoreSignalBody: signalCase.ignoreBody})
					signalRecordsRunnerProcess(t, process, signalCase.signal, signalCase.processGroup)
					waitRecordsRunnerSignalProcess(t, process, signalCase.wantStatus, time.Second)
					assertRecordsRunnerCleanupAttempted(t, spec, fake)
					assertRecordsRunnerTMPDirEmpty(t, fake)
				})
			}
		})
	}
}

func TestRecordsRunnerLifecycleOuterSignalWaitsForDelayedChildCleanup(t *testing.T) {
	t.Parallel()

	spec := recordsRunnerLifecycleSpecByName(t, "integration s3")
	fake := newRecordsRunnerFakeToolchain(t)
	process := startRecordsRunnerSignalProcess(t, spec, fake, recordsRunnerRunOptions{
		ignoreSignalBody: true,
		cleanupDelay:     "0.2",
	})
	signalRecordsRunnerProcess(t, process, syscall.SIGTERM, false)
	waitRecordsRunnerSignalProcess(t, process, 143, 3*time.Second)
	assertRecordsRunnerCleanupAttempted(t, spec, fake)
	assertRecordsRunnerContainerCleanupCompleted(t, spec, fake)
	assertRecordsRunnerTMPDirEmpty(t, fake)
}

func TestRecordsRunnerLifecycleSecondSignalCannotInterruptCleanup(t *testing.T) {
	t.Parallel()

	spec := recordsRunnerLifecycleSpecByName(t, "record platform child")
	fake := newRecordsRunnerFakeToolchain(t)
	cleanupReady := filepath.Join(filepath.Dir(fake.bin), "cleanup-ready")
	process := startRecordsRunnerSignalProcess(t, spec, fake, recordsRunnerRunOptions{
		cleanupDelay: "0.2",
		cleanupReady: cleanupReady,
	})
	signalRecordsRunnerProcess(t, process, syscall.SIGTERM, false)
	waitRecordsRunnerMarker(t, cleanupReady, time.Second, func() {
		_ = syscall.Kill(-process.command.Process.Pid, syscall.SIGKILL)
		_ = process.command.Wait()
		t.Fatalf("records runner cleanup did not start; stdout %q, stderr %q", process.stdout.String(), process.stderr.String())
	})
	signalRecordsRunnerProcess(t, process, syscall.SIGTERM, true)
	waitRecordsRunnerSignalProcess(t, process, 143, 2*time.Second)
	assertRecordsRunnerCleanupAttempted(t, spec, fake)
	assertRecordsRunnerContainerCleanupCompleted(t, spec, fake)
	assertRecordsRunnerTMPDirEmpty(t, fake)
}

func TestRecordsRunnerLifecycleFirstGroupSignalDuringNormalCleanupCannotInterruptResources(t *testing.T) {
	t.Parallel()

	spec := recordsRunnerLifecycleSpecByName(t, "record platform child")
	fake := newRecordsRunnerFakeToolchain(t)
	cleanupReady := filepath.Join(filepath.Dir(fake.bin), "normal-cleanup-ready")
	process := startRecordsRunnerCleanupProcess(t, spec, fake, recordsRunnerRunOptions{
		cleanupDelay: "0.2",
		cleanupReady: cleanupReady,
	})
	waitRecordsRunnerMarker(t, cleanupReady, time.Second, func() {
		_ = syscall.Kill(-process.command.Process.Pid, syscall.SIGKILL)
		_ = process.command.Wait()
		t.Fatalf("records runner normal cleanup did not start; stdout %q, stderr %q", process.stdout.String(), process.stderr.String())
	})
	signalRecordsRunnerProcess(t, process, syscall.SIGTERM, true)
	waitRecordsRunnerSignalProcess(t, process, 0, 2*time.Second)
	assertRecordsRunnerCleanupAttempted(t, spec, fake)
	log := readRecordsRunnerLog(t, fake.dockerLog)
	if got := countRecordsRunnerLogLines(log, "rm-complete\t"); got != spec.wantContainers {
		t.Fatalf("normal cleanup completed containers = %d, log %q, want %d", got, log, spec.wantContainers)
	}
	assertRecordsRunnerTMPDirEmpty(t, fake)
}

func TestRecordsS3LifecycleGateForwardsParentOnlySignals(t *testing.T) {
	t.Parallel()

	spec := recordsRunnerLifecycleSpec{
		name:         "records s3 lifecycle gate",
		relativePath: filepath.Join(recordsRunnerRepositoryRoot(t), "scripts", "test-records-s3-lifecycle.sh"),
	}
	for _, signalCase := range []struct {
		name         string
		signal       syscall.Signal
		wantStatus   int
		options      recordsRunnerRunOptions
		maxElapsed   time.Duration
		wantCleanup  int
		wantVolumeRM int
	}{
		{
			name:         "INT",
			signal:       syscall.SIGINT,
			wantStatus:   130,
			options:      recordsRunnerRunOptions{signalBodyDuration: "1.5"},
			maxElapsed:   time.Second,
			wantCleanup:  5,
			wantVolumeRM: 1,
		},
		{
			name:         "TERM",
			signal:       syscall.SIGTERM,
			wantStatus:   143,
			options:      recordsRunnerRunOptions{signalBodyDuration: "1.5"},
			maxElapsed:   time.Second,
			wantCleanup:  5,
			wantVolumeRM: 1,
		},
		{
			name:         "ignored body with delayed child cleanup",
			signal:       syscall.SIGTERM,
			wantStatus:   143,
			options:      recordsRunnerRunOptions{ignoreSignalBody: true, signalBodyDuration: "3", cleanupDelay: "0.2"},
			maxElapsed:   2 * time.Second,
			wantCleanup:  5,
			wantVolumeRM: 1,
		},
	} {
		signalCase := signalCase
		t.Run(signalCase.name, func(t *testing.T) {
			fake := newRecordsRunnerFakeToolchain(t)
			process := startRecordsRunnerSignalProcess(t, spec, fake, signalCase.options)
			started := time.Now()
			signalRecordsRunnerProcess(t, process, signalCase.signal, false)
			waitRecordsRunnerSignalProcess(t, process, signalCase.wantStatus, 5*time.Second)
			if elapsed := time.Since(started); elapsed >= signalCase.maxElapsed {
				t.Fatalf("records S3 lifecycle gate signal elapsed = %s, want less than %s", elapsed, signalCase.maxElapsed)
			}
			log := readRecordsRunnerLog(t, fake.dockerLog)
			if got := countRecordsRunnerLogLines(log, "rm\t"); got != signalCase.wantCleanup {
				t.Fatalf("records S3 lifecycle gate container cleanup attempts = %d, log %q, want %d", got, log, signalCase.wantCleanup)
			}
			if got := countRecordsRunnerLogLines(log, "rm-complete\t"); got != signalCase.wantCleanup {
				t.Fatalf("records S3 lifecycle gate container cleanup completions = %d, log %q, want %d", got, log, signalCase.wantCleanup)
			}
			if got := countRecordsRunnerLogLines(log, "volume-rm\t"); got != signalCase.wantVolumeRM {
				t.Fatalf("records S3 lifecycle gate volume cleanup attempts = %d, log %q, want %d", got, log, signalCase.wantVolumeRM)
			}
			assertRecordsRunnerTMPDirEmpty(t, fake)
		})
	}
}

func TestRecordsRunnerLifecycleKeepModeReportsAndRetainsOnlyDebugState(t *testing.T) {
	t.Parallel()

	for _, spec := range recordsRunnerLifecycleSpecs(t) {
		spec := spec
		t.Run(spec.name, func(t *testing.T) {
			t.Parallel()
			fake := newRecordsRunnerFakeToolchain(t)
			code, _, stderr := runRecordsRunner(t, spec, fake, recordsRunnerRunOptions{keep: true})
			if code != 0 {
				t.Fatalf("keep lifecycle exit code = %d, stderr %q, want 0", code, stderr)
			}
			log := readRecordsRunnerLog(t, fake.dockerLog)
			if got := countRecordsRunnerLogLines(log, "rm\t"); got != spec.wantContainers {
				t.Fatalf("keep container cleanup attempts = %d, log %q, want %d", got, log, spec.wantContainers)
			}
			if got := countRecordsRunnerLogLines(log, "volume-rm\t"); got != 0 {
				t.Fatalf("keep volume cleanup attempts = %d, log %q, want 0", got, log)
			}
			wantWorkspaceRMs := 0
			if spec.outer {
				wantWorkspaceRMs = 1
			}
			if got := countRecordsRunnerLogLines(readRecordsRunnerLog(t, fake.rmLog), "workspace-rm\t"); got != wantWorkspaceRMs {
				t.Fatalf("keep workspace cleanup attempts = %d, want %d", got, wantWorkspaceRMs)
			}
			entries, err := os.ReadDir(fake.tmpParent)
			if err != nil {
				t.Fatalf("read keep TMPDIR: %v", err)
			}
			wantWorkspaces := 1
			if len(entries) != wantWorkspaces {
				t.Fatalf("keep TMPDIR entries = %v, want %d retained workspace(s)", entries, wantWorkspaces)
			}
			for _, entry := range entries {
				path := filepath.Join(fake.tmpParent, entry.Name())
				if !strings.Contains(stderr, "records runner retained workspace: "+path) {
					t.Fatalf("keep stderr = %q, want retained workspace %q", stderr, path)
				}
			}
			if spec.wantVolumes == 1 && !strings.Contains(stderr, "records runner retained volume: houfeng-records-") {
				t.Fatalf("keep stderr = %q, want retained volume", stderr)
			}
		})
	}
}

func TestRecordsRunnerLifecycleRejectsUnsafeRunIDBeforeDockerSetup(t *testing.T) {
	t.Parallel()

	for _, spec := range recordsRunnerLifecycleSpecs(t) {
		spec := spec
		t.Run(spec.name, func(t *testing.T) {
			t.Parallel()
			fake := newRecordsRunnerFakeToolchain(t)
			code, _, stderr := runRecordsRunner(t, spec, fake, recordsRunnerRunOptions{runID: "unsafe/run id"})
			if code != 2 {
				t.Fatalf("unsafe run id exit code = %d, stderr %q, want 2", code, stderr)
			}
			if !strings.Contains(stderr, "invalid HOUFENG_RECORDS_RUN_ID") {
				t.Fatalf("unsafe run id stderr = %q, want validation diagnostic", stderr)
			}
			if log := readRecordsRunnerLog(t, fake.dockerLog); log != "" {
				t.Fatalf("unsafe run id Docker log = %q, want no setup", log)
			}
			if log := readRecordsRunnerLog(t, fake.rmLog); log != "" {
				t.Fatalf("unsafe run id workspace log = %q, want rejection before mktemp", log)
			}
			assertRecordsRunnerTMPDirEmpty(t, fake)
		})
	}
}

func TestRecordsRunnerLifecycleDefaultRunIDComesFromOuterWorkspace(t *testing.T) {
	t.Parallel()

	spec := recordsRunnerLifecycleSpecByName(t, "integration s3")
	fake := newRecordsRunnerFakeToolchain(t)
	code, _, stderr := runRecordsRunner(t, spec, fake, recordsRunnerRunOptions{omitRunID: true})
	if code != 0 {
		t.Fatalf("default run id lifecycle exit code = %d, stderr %q, want 0", code, stderr)
	}
	rmLog := readRecordsRunnerLog(t, fake.rmLog)
	var outerWorkspace string
	for _, line := range strings.Split(strings.TrimSpace(rmLog), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) == 2 && strings.HasPrefix(filepath.Base(fields[1]), spec.workspacePrefix) {
			outerWorkspace = fields[1]
		}
	}
	if outerWorkspace == "" {
		t.Fatalf("default run id workspace log = %q, want outer workspace", rmLog)
	}
	wantRunID := filepath.Base(outerWorkspace)
	assertRecordsRunnerResourceLifecycle(t, spec, fake, wantRunID)
	assertRecordsRunnerTMPDirEmpty(t, fake)
}

func TestRecordsRunnerLifecycleSourcesVerifyExactOwnershipAndDoNotMaskCleanup(t *testing.T) {
	t.Parallel()

	root := recordsRunnerRepositoryRoot(t)
	for _, rel := range []string{
		filepath.Join("scripts", "run-records-integration.sh"),
		filepath.Join("scripts", "run-records-recovery.sh"),
		filepath.Join("scripts", "test-record-platform-integration.sh"),
	} {
		payload, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		script := string(payload)
		trackAt := strings.Index(script, `containers+=("$name")`)
		runAt := strings.Index(script, "docker run -d")
		if trackAt < 0 || runAt < 0 || trackAt > runAt {
			t.Fatalf("%s must track the exact container candidate before docker run", rel)
		}
		if !strings.Contains(script, `--label "com.houfeng.records.owner=$records_owner_id"`) {
			t.Fatalf("%s must label each container with the workspace-unique owner", rel)
		}
		if strings.Contains(script, "docker run --rm") {
			t.Fatalf("%s must not auto-remove lifecycle-managed containers", rel)
		}
		validateAt := strings.Index(script, "records_runner_validate_run_id_override")
		toolsAt := strings.Index(script, "records_runner_require_signal_tools")
		workspaceAt := strings.Index(script, "workspace=$(mktemp")
		if validateAt < 0 || workspaceAt < 0 || validateAt > workspaceAt {
			t.Fatalf("%s must reject an invalid run id before workspace creation", rel)
		}
		if toolsAt < 0 || toolsAt > workspaceAt {
			t.Fatalf("%s must validate signal-tool support before workspace or Docker side effects", rel)
		}
	}
	for _, rel := range []string{
		filepath.Join("scripts", "run-records-integration.sh"),
		filepath.Join("scripts", "run-records-recovery.sh"),
	} {
		payload, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		script := string(payload)
		nameAt := strings.Index(script, `minio_volume="houfeng-records-`)
		trackAt := strings.Index(script, `volumes+=("$minio_volume")`)
		createAt := strings.Index(script, "docker volume create")
		verifyAt := strings.Index(script, `records_runner_verify_volume_ownership "$minio_volume"`)
		mountAt := strings.Index(script, `start_minio "$minio_name"`)
		if nameAt < 0 || trackAt < 0 || createAt < 0 || verifyAt < 0 || mountAt < 0 ||
			!(nameAt < trackAt && trackAt < createAt && createAt < verifyAt && verifyAt < mountAt) {
			t.Fatalf("%s must name and track the exact owner volume before create, then verify before MinIO mount", rel)
		}
		if strings.Contains(script, `minio_volume=$(docker volume create`) {
			t.Fatalf("%s must not depend on post-create stdout to discover volume ownership", rel)
		}
	}

	payload, err := os.ReadFile(filepath.Join(root, "scripts", "lib", "records-runner-lifecycle.sh"))
	if err != nil {
		t.Fatalf("read records runner lifecycle helper: %v", err)
	}
	helper := string(payload)
	for _, want := range []string{
		"docker container inspect",
		"docker volume inspect",
		"com.houfeng.records.owner",
		`docker rm -f "$container_id"`,
		"local signal_timeout=30",
		"signal_timeout=5",
		"signal_timeout=60",
		"records_runner_pending_signal_status=$signal_status",
		`kill -KILL "$watchdog_pid"`,
		"records_runner_arm_cleanup_signals",
		"trap '' INT TERM",
	} {
		if !strings.Contains(helper, want) {
			t.Fatalf("records runner lifecycle helper must contain %q", want)
		}
	}
	for _, forbidden := range []string{
		"|| true",
		">/dev/null 2>&1",
		"docker system prune",
		"docker volume prune",
		"docker ps",
		"docker volume ls",
		"signal_timeout=0.7",
		"trap - EXIT INT TERM",
		`\"`,
	} {
		if strings.Contains(helper, forbidden) {
			t.Fatalf("records runner lifecycle helper must not contain %q", forbidden)
		}
	}
}

func TestRecordsRunnerLifecycleRealGateUsesUniqueLabelsAndExactEmergencyCleanup(t *testing.T) {
	t.Parallel()

	path := filepath.Join(recordsRunnerRepositoryRoot(t), "scripts", "test-records-s3-lifecycle.sh")
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read real records S3 lifecycle gate: %v", err)
	}
	script := string(payload)
	for _, want := range []string{
		`run-records-integration.sh" --profile s3`,
		`run-records-recovery.sh" --profile s3 --all`,
		`--filter "label=com.houfeng.records.run=$run_id"`,
		"docker volume ls -q",
		"docker rm -f \"$container\"",
		"docker volume rm \"$volume\"",
		"-u HOUFENG_RECORDS_KEEP_WORKSPACE",
		"records_runner_kind=records-s3-lifecycle",
		`source "$root/scripts/lib/records-runner-lifecycle.sh"`,
		"setsid env",
		"records_runner_body_pid=$!",
		"trap 'records_runner_signal 130' INT",
		"trap 'records_runner_signal 143' TERM",
		"records_runner_arm_cleanup_signals",
		"assert_no_residue",
		"roots=(/tmp/houfeng-records-*)",
		"find \"${roots[@]}\" -xdev -uid 0",
		"-printf '%p\\t%U:%G\\t%s\\t%T@\\n'",
		"-uid 0",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("real records S3 lifecycle gate missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"|| true",
		"docker system prune",
		"docker volume prune",
		`--filter "name=`,
		`rm -rf /tmp`,
		"-maxdepth 1",
		"trap - EXIT INT TERM",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("real records S3 lifecycle gate must not contain %q", forbidden)
		}
	}
}

func recordsRunnerLifecycleSpecs(t *testing.T) []recordsRunnerLifecycleSpec {
	t.Helper()
	root := recordsRunnerRepositoryRoot(t)
	return []recordsRunnerLifecycleSpec{
		{
			name:            "integration s3",
			relativePath:    filepath.Join(root, "scripts", "run-records-integration.sh"),
			args:            []string{"--profile", "s3"},
			runnerKind:      "integration",
			outer:           true,
			workspacePrefix: "houfeng-records-integration.",
			wantContainers:  5,
			wantVolumes:     1,
			failingName:     "minio",
		},
		{
			name:            "integration local",
			relativePath:    filepath.Join(root, "scripts", "run-records-integration.sh"),
			args:            []string{"--profile", "local"},
			runnerKind:      "integration",
			outer:           true,
			workspacePrefix: "houfeng-records-integration.",
			wantContainers:  4,
			failingName:     "houfeng-rp-app-",
		},
		{
			name:            "recovery s3",
			relativePath:    filepath.Join(root, "scripts", "run-records-recovery.sh"),
			args:            []string{"--profile", "s3", "--all"},
			runnerKind:      "recovery",
			outer:           true,
			workspacePrefix: "houfeng-records-recovery.",
			wantContainers:  5,
			wantVolumes:     1,
			failingName:     "minio",
		},
		{
			name:            "recovery local",
			relativePath:    filepath.Join(root, "scripts", "run-records-recovery.sh"),
			args:            []string{"--profile", "local", "--all"},
			runnerKind:      "recovery",
			outer:           true,
			workspacePrefix: "houfeng-records-recovery.",
			wantContainers:  4,
			failingName:     "houfeng-rp-app-",
		},
		{
			name:            "record platform child",
			relativePath:    filepath.Join(root, "scripts", "test-record-platform-integration.sh"),
			args:            []string{"postgres", "--", "go", "test", "./internal/center/recordbackup"},
			runnerKind:      "record-platform",
			workspacePrefix: "houfeng-record-platform.",
			wantContainers:  4,
			failingName:     "houfeng-rp-app-",
		},
	}
}

func recordsRunnerLifecycleSpecByName(t *testing.T, name string) recordsRunnerLifecycleSpec {
	t.Helper()
	for _, spec := range recordsRunnerLifecycleSpecs(t) {
		if spec.name == name {
			return spec
		}
	}
	t.Fatalf("records runner lifecycle spec %q not found", name)
	return recordsRunnerLifecycleSpec{}
}

func recordsRunnerRepositoryRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("read records runner test working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("could not locate repository root for records runners")
		}
		directory = parent
	}
}

func newRecordsRunnerFakeToolchain(t *testing.T) *recordsRunnerFakeToolchain {
	t.Helper()
	root := t.TempDir()
	fake := &recordsRunnerFakeToolchain{
		bin:       filepath.Join(root, "bin"),
		tmpParent: filepath.Join(root, "tmp"),
		dockerLog: filepath.Join(root, "docker.log"),
		rmLog:     filepath.Join(root, "rm.log"),
	}
	if err := os.MkdirAll(fake.bin, 0o755); err != nil {
		t.Fatalf("create records runner fake bin: %v", err)
	}
	if err := os.MkdirAll(fake.tmpParent, 0o755); err != nil {
		t.Fatalf("create records runner fake TMPDIR: %v", err)
	}
	for _, command := range []string{
		"awk", "bash", "basename", "cat", "cmp", "dirname", "env", "find", "mkdir", "od", "python3", "seq",
		"setsid", "sha256sum", "sort", "tr", "wc",
	} {
		target, err := exec.LookPath(command)
		if err != nil {
			t.Fatalf("locate host command %q for records runner fake: %v", command, err)
		}
		if err := os.Symlink(target, filepath.Join(fake.bin, command)); err != nil {
			t.Fatalf("link host command %q for records runner fake: %v", command, err)
		}
	}
	writeExecutable := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(fake.bin, name), []byte(body), 0o755); err != nil {
			t.Fatalf("write records runner fake command %q: %v", name, err)
		}
	}
	writeExecutable("mktemp", `#!/usr/bin/bash
set -euo pipefail
exec /usr/bin/mktemp "$@"
`)
	writeExecutable("ss", "#!/usr/bin/bash\nexit 1\n")
	writeExecutable("sleep", `#!/usr/bin/bash
set -euo pipefail
case "${1-}" in
  5)
    exec /usr/bin/sleep 0.2
    ;;
  30)
    exec /usr/bin/sleep 1.5
    ;;
  60)
    exec /usr/bin/sleep 3
    ;;
esac
exec /usr/bin/sleep "$@"
`)
	writeExecutable("git", `#!/usr/bin/bash
set -euo pipefail
printf '%s\n' 0123456789abcdef0123456789abcdef01234567
`)
	writeExecutable("go", `#!/usr/bin/bash
set -euo pipefail
if [ -n "${HOUFENG_RECORDS_FAKE_SIGNAL_READY-}" ]
then
  if [ "${HOUFENG_RECORDS_FAKE_IGNORE_SIGNAL_BODY-}" = "1" ]
  then
    trap '' INT TERM
  else
    trap 'exit 130' INT
    trap 'exit 143' TERM
  fi
  printf 'ready\n' > "$HOUFENG_RECORDS_FAKE_SIGNAL_READY"
	if [ -n "${HOUFENG_RECORDS_FAKE_SIGNAL_BODY_DURATION-}" ]
	then
	  sleep "$HOUFENG_RECORDS_FAKE_SIGNAL_BODY_DURATION"
	  exit 0
	fi
  while true
  do
    sleep 1
  done
fi
printf '%s' "${HOUFENG_RECORDS_FAKE_BODY_STDOUT-}"
printf '%s' "${HOUFENG_RECORDS_FAKE_BODY_STDERR-}" >&2
exit "${HOUFENG_RECORDS_FAKE_BODY_STATUS:-0}"
`)
	writeExecutable("grep", `#!/usr/bin/bash
set -euo pipefail
failure=${HOUFENG_RECORDS_FAKE_FAIL_SKIP_SCAN-}
if [ -n "$failure" ] && [[ " $* " == *"--- SKIP:"* ]] && [[ " $* " == *"$failure"* ]]
then
  printf 'fake evidence scan failure: %s\n' "$failure" >&2
  exit 47
fi
exec /usr/bin/grep "$@"
`)
	writeExecutable("tee", `#!/usr/bin/bash
set +e
/usr/bin/tee "$@"
tee_status=$?
target=${!#}
case "$target" in
  */child-stdout.log)
    if [ "${HOUFENG_RECORDS_FAKE_FAIL_STDOUT_TEE-}" = "1" ]
    then
      exit 41
    fi
    ;;
  */child-stderr.log)
    if [ "${HOUFENG_RECORDS_FAKE_FAIL_STDERR_TEE-}" = "1" ]
    then
      exit 42
    fi
    ;;
esac
exit "$tee_status"
`)
	writeExecutable("rm", `#!/usr/bin/bash
set -euo pipefail
log=${HOUFENG_RECORDS_FAKE_RM_LOG:?}
lifecycle_log=${HOUFENG_RECORDS_FAKE_DOCKER_LOG:?}
path=${!#}
case "${1-}" in
  -r|-rf|-fr)
    printf 'workspace-rm\t%s\n' "$path" >> "$log"
    printf 'workspace-rm\t%s\n' "$path" >> "$lifecycle_log"
    failure=${HOUFENG_RECORDS_FAKE_FAIL_WORKSPACE-}
    if [ -n "$failure" ] && [[ "$path" == *"$failure"* ]]
    then
      printf 'fake workspace cleanup failure: %s\n' "$path" >&2
      exit 54
    fi
    ;;
esac
exec /usr/bin/rm "$@"
`)
	writeExecutable("docker", `#!/usr/bin/bash
set -euo pipefail
log=${HOUFENG_RECORDS_FAKE_DOCKER_LOG:?}
label_value() {
  local labels=$1
  local key=$2
  local label
  local old_ifs=$IFS
  IFS=,
  for label in $labels
  do
    case "$label" in
      "$key"=*)
        printf '%s\n' "${label#*=}"
        IFS=$old_ifs
        return 0
        ;;
    esac
  done
  IFS=$old_ifs
  return 0
}
command=${1-}
if [ -z "$command" ]
then
  exit 2
fi
shift
case "$command" in
  volume)
    subcommand=${1-}
    shift
    case "$subcommand" in
      create)
        labels=
        name=
        while (($#))
        do
          case "$1" in
            --label)
              labels="${labels}${labels:+,}$2"
              shift 2
              ;;
            *)
              name=$1
              shift
              ;;
          esac
        done
        if [ -z "$name" ]
        then
          name="fake-volume-${RANDOM}${RANDOM}"
        fi
        printf 'volume-create\t%s\t%s\n' "$name" "$labels" >> "$log"
        if [ "${HOUFENG_RECORDS_FAKE_FAIL_VOLUME_CREATE-}" = "1" ]
        then
          printf 'fake partial volume create failure: %s\n' "$name" >&2
          exit 38
        fi
        printf '%s\n' "$name"
        ;;
      rm)
        name=${!#}
        printf 'volume-rm\t%s\n' "$name" >> "$log"
        if [ "${HOUFENG_RECORDS_FAKE_FAIL_VOLUME-}" = "1" ]
        then
          printf 'fake volume cleanup failure: %s\n' "$name" >&2
          exit 53
        fi
        ;;
      inspect)
        name=${!#}
        printf 'volume-inspect\t%s\n' "$name" >> "$log"
        if [ "${HOUFENG_RECORDS_FAKE_FOREIGN_VOLUME_COLLISION-}" = "1" ]
        then
          printf 'foreign-runner|foreign-run|foreign-owner\n'
          exit 0
        fi
        if [ "${HOUFENG_RECORDS_FAKE_REPLACED_VOLUME-}" = "1" ] && \
          [ "$(/usr/bin/awk -F '\t' -v name="$name" '$1 == "volume-inspect" && $2 == name { count++ } END { print count + 0 }' "$log")" -gt 1 ]
        then
          printf 'foreign-runner|foreign-run|foreign-owner\n'
          exit 0
        fi
        line=$(/usr/bin/awk -F '\t' -v name="$name" '$1 == "volume-create" && $2 == name { found=$0 } END { print found }' "$log")
        if [ -z "$line" ]
        then
          printf 'fake volume inspect missing: %s\n' "$name" >&2
          exit 44
        fi
        IFS=$'\t' read -r _ _ labels <<< "$line"
        printf '%s|%s|%s\n' \
          "$(label_value "$labels" com.houfeng.records.runner)" \
          "$(label_value "$labels" com.houfeng.records.run)" \
          "$(label_value "$labels" com.houfeng.records.owner)"
        ;;
      ls)
        ;;
      *)
        exit 2
        ;;
    esac
    ;;
  run)
    name=
    image=
    labels=
    mount=
    while (($#))
    do
      case "$1" in
        --name)
          name=$2
          shift 2
          ;;
        --label)
          labels="${labels}${labels:+,}$2"
          shift 2
          ;;
        --mount)
          mount=$2
          shift 2
          ;;
        --tmpfs|-e|-v)
          shift 2
          ;;
        --rm|-d|--network=host)
          shift
          ;;
        minio/*|postgres:*)
          if [ -z "$image" ]
          then
            image=$1
          fi
          shift
          ;;
        -c)
          shift 2
          ;;
        *)
          shift
          ;;
      esac
    done
    printf 'run\t%s\t%s\t%s\t%s\n' "$name" "$image" "$labels" "$mount" >> "$log"
    collision=${HOUFENG_RECORDS_FAKE_FOREIGN_COLLISION-}
    if [ -n "$collision" ] && [[ "$name" == *"$collision"* ]]
    then
      printf 'fake foreign container name collision: %s\n' "$name" >&2
      exit 125
    fi
    failure=${HOUFENG_RECORDS_FAKE_FAIL_SETUP-}
    if [ -n "$failure" ] && [[ "$name" == *"$failure"* ]]
    then
      printf 'fake container setup failure: %s\n' "$name" >&2
      exit 37
    fi
    printf 'fake-id-%s\n' "$name"
    ;;
  container)
    subcommand=${1-}
    shift
    case "$subcommand" in
      inspect)
        name=${!#}
        printf 'container-inspect\t%s\n' "$name" >> "$log"
        collision=${HOUFENG_RECORDS_FAKE_FOREIGN_COLLISION-}
        if [ -n "$collision" ] && [[ "$name" == *"$collision"* ]]
        then
          printf 'fake-foreign-id|foreign-runner|foreign-run|foreign-owner\n'
          exit 0
        fi
        replacement=${HOUFENG_RECORDS_FAKE_REPLACED_CONTAINER-}
        if [ -n "$replacement" ] && [[ "$name" == *"$replacement"* ]]
        then
          printf 'fake-foreign-id|foreign-runner|foreign-run|foreign-owner\n'
          exit 0
        fi
        line=$(/usr/bin/awk -F '\t' -v name="$name" '$1 == "run" && $2 == name { found=$0 } END { print found }' "$log")
        if [ -z "$line" ]
        then
          printf 'fake container inspect missing: %s\n' "$name" >&2
          exit 44
        fi
        IFS=$'\t' read -r _ _ _ labels _ <<< "$line"
        printf 'fake-id-%s|%s|%s|%s\n' \
          "$name" \
          "$(label_value "$labels" com.houfeng.records.runner)" \
          "$(label_value "$labels" com.houfeng.records.run)" \
          "$(label_value "$labels" com.houfeng.records.owner)"
        ;;
      *)
        exit 2
        ;;
    esac
    ;;
  exec)
    container=${1-}
    shift
    operation=${1-}
    printf 'exec\t%s\t%s\n' "$container" "$operation" >> "$log"
    if [ "$operation" = "psql" ]
    then
      printf '%s-system-identifier\n' "$container"
    fi
    ;;
  rm)
    name=${!#}
    printf 'rm\t%s\n' "$name" >> "$log"
	case "$name" in
	  fake-id-houfeng-rp-*)
	    if [ -n "${HOUFENG_RECORDS_FAKE_CLEANUP_READY-}" ]
	    then
	      printf 'ready\n' > "$HOUFENG_RECORDS_FAKE_CLEANUP_READY"
	    fi
	    if [ -n "${HOUFENG_RECORDS_FAKE_CLEANUP_DELAY-}" ]
	    then
	      /usr/bin/sleep "$HOUFENG_RECORDS_FAKE_CLEANUP_DELAY"
	    fi
	    ;;
	esac
    failure=${HOUFENG_RECORDS_FAKE_FAIL_CONTAINER-}
    if [ -n "$failure" ] && [[ "$name" == *"$failure"* ]]
    then
      printf 'fake container cleanup failure: %s\n' "$name" >&2
      exit 52
    fi
	printf 'rm-complete\t%s\n' "$name" >> "$log"
    ;;
  logs|ps)
    ;;
  *)
    exit 2
    ;;
esac
`)
	return fake
}

func runRecordsRunner(
	t *testing.T,
	spec recordsRunnerLifecycleSpec,
	fake *recordsRunnerFakeToolchain,
	options recordsRunnerRunOptions,
) (int, string, string) {
	t.Helper()
	command := exec.Command("/usr/bin/bash", append([]string{spec.relativePath}, spec.args...)...)
	command.Dir = recordsRunnerRepositoryRoot(t)
	command.Env = recordsRunnerEnvironment(fake, options)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err == nil {
		return 0, stdout.String(), stderr.String()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), stdout.String(), stderr.String()
	}
	t.Fatalf("run %s records lifecycle: %v", spec.name, err)
	return -1, "", ""
}

func startRecordsRunnerSignalProcess(
	t *testing.T,
	spec recordsRunnerLifecycleSpec,
	fake *recordsRunnerFakeToolchain,
	options recordsRunnerRunOptions,
) *recordsRunnerSignalProcess {
	t.Helper()
	ready := filepath.Join(filepath.Dir(fake.bin), "foreground-child-ready")
	process := &recordsRunnerSignalProcess{}
	process.command = exec.Command("/usr/bin/bash", append([]string{spec.relativePath}, spec.args...)...)
	process.command.Dir = recordsRunnerRepositoryRoot(t)
	process.command.Env = append(recordsRunnerEnvironment(fake, options),
		"HOUFENG_RECORDS_FAKE_SIGNAL_READY="+ready,
	)
	process.command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	process.command.Stdout = &process.stdout
	process.command.Stderr = &process.stderr
	if err := process.command.Start(); err != nil {
		t.Fatalf("start signal lifecycle runner: %v", err)
	}
	waitRecordsRunnerMarker(t, ready, 5*time.Second, func() {
		_ = syscall.Kill(-process.command.Process.Pid, syscall.SIGKILL)
		_ = process.command.Wait()
		t.Fatalf("foreground child was not ready; stdout %q, stderr %q", process.stdout.String(), process.stderr.String())
	})
	return process
}

func startRecordsRunnerCleanupProcess(
	t *testing.T,
	spec recordsRunnerLifecycleSpec,
	fake *recordsRunnerFakeToolchain,
	options recordsRunnerRunOptions,
) *recordsRunnerSignalProcess {
	t.Helper()
	process := &recordsRunnerSignalProcess{}
	process.command = exec.Command("/usr/bin/bash", append([]string{spec.relativePath}, spec.args...)...)
	process.command.Dir = recordsRunnerRepositoryRoot(t)
	process.command.Env = recordsRunnerEnvironment(fake, options)
	process.command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	process.command.Stdout = &process.stdout
	process.command.Stderr = &process.stderr
	if err := process.command.Start(); err != nil {
		t.Fatalf("start cleanup lifecycle runner: %v", err)
	}
	return process
}

func signalRecordsRunnerProcess(t *testing.T, process *recordsRunnerSignalProcess, signal syscall.Signal, processGroup bool) {
	t.Helper()
	targetPID := process.command.Process.Pid
	if processGroup {
		targetPID = -targetPID
	}
	if err := syscall.Kill(targetPID, signal); err != nil {
		t.Fatalf("signal lifecycle runner: %v", err)
	}
}

func waitRecordsRunnerSignalProcess(t *testing.T, process *recordsRunnerSignalProcess, wantStatus int, timeout time.Duration) {
	t.Helper()
	wait := make(chan error, 1)
	go func() { wait <- process.command.Wait() }()
	var waitErr error
	select {
	case waitErr = <-wait:
	case <-time.After(timeout):
		_ = syscall.Kill(-process.command.Process.Pid, syscall.SIGKILL)
		<-wait
		t.Fatalf("signal lifecycle runner did not exit; stdout %q, stderr %q", process.stdout.String(), process.stderr.String())
	}
	_ = syscall.Kill(-process.command.Process.Pid, syscall.SIGKILL)
	if wantStatus == 0 && waitErr == nil {
		return
	}
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) || exitErr.ExitCode() != wantStatus {
		t.Fatalf("signal lifecycle exit error = %v, stdout %q, stderr %q, want status %d", waitErr, process.stdout.String(), process.stderr.String(), wantStatus)
	}
}

func waitRecordsRunnerMarker(t *testing.T, marker string, timeout time.Duration, onTimeout func()) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(marker); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stat records runner marker %s: %v", marker, err)
		}
		if time.Now().After(deadline) {
			onTimeout()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func recordsRunnerEnvironment(fake *recordsRunnerFakeToolchain, options recordsRunnerRunOptions) []string {
	blocked := map[string]struct{}{
		"PATH":                                          {},
		"TMPDIR":                                        {},
		"HOUFENG_RECORDS_KEEP_WORKSPACE":                {},
		"HOUFENG_RECORDS_RUN_ID":                        {},
		"HOUFENG_RECORDS_FAKE_DOCKER_LOG":               {},
		"HOUFENG_RECORDS_FAKE_RM_LOG":                   {},
		"HOUFENG_RECORDS_FAKE_BODY_STATUS":              {},
		"HOUFENG_RECORDS_FAKE_BODY_STDOUT":              {},
		"HOUFENG_RECORDS_FAKE_BODY_STDERR":              {},
		"HOUFENG_RECORDS_FAKE_FAIL_CONTAINER":           {},
		"HOUFENG_RECORDS_FAKE_FAIL_VOLUME":              {},
		"HOUFENG_RECORDS_FAKE_FAIL_WORKSPACE":           {},
		"HOUFENG_RECORDS_FAKE_FAIL_SETUP":               {},
		"HOUFENG_RECORDS_FAKE_FAIL_STDOUT_TEE":          {},
		"HOUFENG_RECORDS_FAKE_FAIL_STDERR_TEE":          {},
		"HOUFENG_RECORDS_FAKE_FAIL_SKIP_SCAN":           {},
		"HOUFENG_RECORDS_FAKE_FOREIGN_COLLISION":        {},
		"HOUFENG_RECORDS_FAKE_REPLACED_CONTAINER":       {},
		"HOUFENG_RECORDS_FAKE_IGNORE_SIGNAL_BODY":       {},
		"HOUFENG_RECORDS_FAKE_CLEANUP_DELAY":            {},
		"HOUFENG_RECORDS_FAKE_CLEANUP_READY":            {},
		"HOUFENG_RECORDS_FAKE_SIGNAL_BODY_DURATION":     {},
		"HOUFENG_RECORDS_FAKE_FAIL_VOLUME_CREATE":       {},
		"HOUFENG_RECORDS_FAKE_FOREIGN_VOLUME_COLLISION": {},
		"HOUFENG_RECORDS_FAKE_REPLACED_VOLUME":          {},
		"HOUFENG_RECORDS_FAKE_SIGNAL_READY":             {},
	}
	environment := make([]string, 0, len(os.Environ())+20)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, skip := blocked[key]; !skip {
			environment = append(environment, entry)
		}
	}
	environment = append(environment,
		"PATH="+fake.bin,
		"TMPDIR="+fake.tmpParent,
		"HOUFENG_RECORDS_FAKE_DOCKER_LOG="+fake.dockerLog,
		"HOUFENG_RECORDS_FAKE_RM_LOG="+fake.rmLog,
		fmt.Sprintf("HOUFENG_RECORDS_FAKE_BODY_STATUS=%d", options.bodyStatus),
		"HOUFENG_RECORDS_FAKE_BODY_STDOUT="+options.bodyStdout,
		"HOUFENG_RECORDS_FAKE_BODY_STDERR="+options.bodyStderr,
		"HOUFENG_RECORDS_FAKE_FAIL_CONTAINER="+options.failContainer,
		fmt.Sprintf("HOUFENG_RECORDS_FAKE_FAIL_VOLUME=%d", boolInt(options.failVolume)),
		"HOUFENG_RECORDS_FAKE_FAIL_WORKSPACE="+options.failWorkspace,
		"HOUFENG_RECORDS_FAKE_FAIL_SETUP="+options.failSetup,
		fmt.Sprintf("HOUFENG_RECORDS_FAKE_FAIL_STDOUT_TEE=%d", boolInt(options.failStdoutTee)),
		fmt.Sprintf("HOUFENG_RECORDS_FAKE_FAIL_STDERR_TEE=%d", boolInt(options.failStderrTee)),
		"HOUFENG_RECORDS_FAKE_FAIL_SKIP_SCAN="+options.failSkipScan,
		"HOUFENG_RECORDS_FAKE_FOREIGN_COLLISION="+options.foreignCollision,
		"HOUFENG_RECORDS_FAKE_REPLACED_CONTAINER="+options.replacedContainer,
		fmt.Sprintf("HOUFENG_RECORDS_FAKE_IGNORE_SIGNAL_BODY=%d", boolInt(options.ignoreSignalBody)),
		"HOUFENG_RECORDS_FAKE_CLEANUP_DELAY="+options.cleanupDelay,
		"HOUFENG_RECORDS_FAKE_CLEANUP_READY="+options.cleanupReady,
		"HOUFENG_RECORDS_FAKE_SIGNAL_BODY_DURATION="+options.signalBodyDuration,
		fmt.Sprintf("HOUFENG_RECORDS_FAKE_FAIL_VOLUME_CREATE=%d", boolInt(options.failVolumeCreate)),
		fmt.Sprintf("HOUFENG_RECORDS_FAKE_FOREIGN_VOLUME_COLLISION=%d", boolInt(options.foreignVolumeCollision)),
		fmt.Sprintf("HOUFENG_RECORDS_FAKE_REPLACED_VOLUME=%d", boolInt(options.replacedVolume)),
	)
	if options.keep {
		environment = append(environment, "HOUFENG_RECORDS_KEEP_WORKSPACE=1")
	}
	if !options.omitRunID {
		runID := options.runID
		if runID == "" {
			runID = "records-test-run"
		}
		environment = append(environment, "HOUFENG_RECORDS_RUN_ID="+runID)
	}
	return environment
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func readRecordsRunnerLog(t *testing.T, path string) string {
	t.Helper()
	payload, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatalf("read records runner log %s: %v", path, err)
	}
	return string(payload)
}

func countRecordsRunnerLogLines(log, prefix string) int {
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
		if strings.HasPrefix(line, prefix) {
			count++
		}
	}
	return count
}

func assertRecordsRunnerResourceLifecycle(
	t *testing.T,
	spec recordsRunnerLifecycleSpec,
	fake *recordsRunnerFakeToolchain,
	wantRunID string,
) {
	t.Helper()
	log := readRecordsRunnerLog(t, fake.dockerLog)
	if got := countRecordsRunnerLogLines(log, "run\t"); got != spec.wantContainers {
		t.Fatalf("container creates = %d, log %q, want %d", got, log, spec.wantContainers)
	}
	if got := countRecordsRunnerLogLines(log, "rm\t"); got != spec.wantContainers {
		t.Fatalf("container removes = %d, log %q, want %d", got, log, spec.wantContainers)
	}
	if got := countRecordsRunnerLogLines(log, "volume-create\t"); got != spec.wantVolumes {
		t.Fatalf("volume creates = %d, log %q, want %d", got, log, spec.wantVolumes)
	}
	if got := countRecordsRunnerLogLines(log, "volume-rm\t"); got != spec.wantVolumes {
		t.Fatalf("volume removes = %d, log %q, want %d", got, log, spec.wantVolumes)
	}
	wantRunLabel := "com.houfeng.records.run=" + wantRunID
	wantOwnerLabel := "com.houfeng.records.owner="
	for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
		if strings.HasPrefix(line, "run\t") || strings.HasPrefix(line, "volume-create\t") {
			if !strings.Contains(line, wantRunLabel) {
				t.Fatalf("resource log line %q missing run label %q", line, wantRunLabel)
			}
			if !strings.Contains(line, wantOwnerLabel) {
				t.Fatalf("resource log line %q missing workspace-unique owner label", line)
			}
		}
	}
	if spec.wantVolumes == 1 {
		if !strings.Contains(log, "com.houfeng.records.runner="+spec.runnerKind) {
			t.Fatalf("outer resource log %q missing runner label %q", log, spec.runnerKind)
		}
		if !strings.Contains(log, "type=volume,source=houfeng-records-") || !strings.Contains(log, "target=/data") {
			t.Fatalf("outer resource log %q missing named volume mount", log)
		}
	}
	assertRecordsRunnerExactCreateRemovePairs(t, spec, log)
	assertRecordsRunnerCleanupAttempted(t, spec, fake)
}

func assertRecordsRunnerCleanupAttempted(t *testing.T, spec recordsRunnerLifecycleSpec, fake *recordsRunnerFakeToolchain) {
	t.Helper()
	log := readRecordsRunnerLog(t, fake.dockerLog)
	if got := countRecordsRunnerLogLines(log, "rm\t"); got != spec.wantContainers {
		t.Fatalf("container cleanup attempts = %d, log %q, want %d", got, log, spec.wantContainers)
	}
	if got := countRecordsRunnerLogLines(log, "volume-rm\t"); got != spec.wantVolumes {
		t.Fatalf("volume cleanup attempts = %d, log %q, want %d", got, log, spec.wantVolumes)
	}
	wantWorkspaceRMs := 1
	if spec.outer {
		wantWorkspaceRMs = 2
	}
	rmLog := readRecordsRunnerLog(t, fake.rmLog)
	if got := countRecordsRunnerLogLines(rmLog, "workspace-rm\t"); got != wantWorkspaceRMs {
		t.Fatalf("workspace cleanup attempts = %d, log %q, want %d", got, rmLog, wantWorkspaceRMs)
	}
	assertRecordsRunnerCleanupOrder(t, spec, log)
}

func assertRecordsRunnerContainerCleanupCompleted(t *testing.T, spec recordsRunnerLifecycleSpec, fake *recordsRunnerFakeToolchain) {
	t.Helper()
	log := readRecordsRunnerLog(t, fake.dockerLog)
	if got := countRecordsRunnerLogLines(log, "rm-complete\t"); got != spec.wantContainers {
		t.Fatalf("container cleanup completions = %d, log %q, want %d", got, log, spec.wantContainers)
	}
}

func assertRecordsRunnerExactCreateRemovePairs(t *testing.T, spec recordsRunnerLifecycleSpec, log string) {
	t.Helper()
	createdContainers := make(map[string]int, spec.wantContainers)
	removedContainers := make(map[string]int, spec.wantContainers)
	createdVolumes := make(map[string]int, spec.wantVolumes)
	removedVolumes := make(map[string]int, spec.wantVolumes)
	for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "run":
			createdContainers[fields[1]]++
			wantKind := "record-platform"
			if strings.Contains(fields[1], "minio") {
				wantKind = spec.runnerKind
			}
			if len(fields) < 4 || !strings.Contains(fields[3], "com.houfeng.records.runner="+wantKind) {
				t.Fatalf("container create record %q missing runner label %q", line, wantKind)
			}
		case "rm":
			removedContainers[fields[1]]++
		case "volume-create":
			createdVolumes[fields[1]]++
			if len(fields) < 3 || !strings.Contains(fields[2], "com.houfeng.records.runner="+spec.runnerKind) {
				t.Fatalf("volume create record %q missing runner label %q", line, spec.runnerKind)
			}
		case "volume-rm":
			removedVolumes[fields[1]]++
		}
	}
	for name, count := range createdContainers {
		containerID := "fake-id-" + name
		if count != 1 || removedContainers[containerID] != 1 {
			t.Fatalf("container %q create/remove counts = %d/%d, want 1/1 by immutable id %q; log %q", name, count, removedContainers[containerID], containerID, log)
		}
	}
	for name, count := range createdVolumes {
		if count != 1 || removedVolumes[name] != 1 {
			t.Fatalf("volume %q create/remove counts = %d/%d, want 1/1; log %q", name, count, removedVolumes[name], log)
		}
	}
}

func assertRecordsRunnerCleanupOrder(t *testing.T, spec recordsRunnerLifecycleSpec, log string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(log), "\n")
	if !spec.outer {
		lastContainer := recordsRunnerLastLogLineIndex(lines, "rm\t")
		workspace := recordsRunnerLastLogLineIndex(lines, "workspace-rm\t")
		if lastContainer < 0 || workspace < 0 || lastContainer > workspace {
			t.Fatalf("direct child cleanup order in log %q, want containers before workspace", log)
		}
		return
	}
	if spec.wantVolumes == 0 {
		lastContainer := recordsRunnerLastLogLineIndex(lines, "rm\t")
		childWorkspace := recordsRunnerLastLogLineContaining(lines, "workspace-rm\t", "houfeng-record-platform.")
		outerWorkspace := recordsRunnerLastLogLineContaining(lines, "workspace-rm\t", spec.workspacePrefix)
		if lastContainer < 0 || childWorkspace < 0 || outerWorkspace < 0 ||
			!(lastContainer < childWorkspace && childWorkspace < outerWorkspace) {
			t.Fatalf("local outer cleanup order in log %q, want child containers then child workspace then outer workspace", log)
		}
		return
	}
	outerContainer := recordsRunnerLastLogLineContaining(lines, "rm\t", "minio")
	volume := recordsRunnerLastLogLineIndex(lines, "volume-rm\t")
	outerWorkspace := recordsRunnerLastLogLineContaining(lines, "workspace-rm\t", spec.workspacePrefix)
	if outerContainer < 0 || volume < 0 || outerWorkspace < 0 || !(outerContainer < volume && volume < outerWorkspace) {
		t.Fatalf("outer cleanup order in log %q, want container then volume then workspace", log)
	}
}

func recordsRunnerLastLogLineIndex(lines []string, prefix string) int {
	index := -1
	for position, line := range lines {
		if strings.HasPrefix(line, prefix) {
			index = position
		}
	}
	return index
}

func recordsRunnerLastLogLineContaining(lines []string, prefix, fragment string) int {
	index := -1
	for position, line := range lines {
		if strings.HasPrefix(line, prefix) && strings.Contains(line, fragment) {
			index = position
		}
	}
	return index
}

func assertRecordsRunnerTMPDirEmpty(t *testing.T, fake *recordsRunnerFakeToolchain) {
	t.Helper()
	entries, err := os.ReadDir(fake.tmpParent)
	if err != nil {
		t.Fatalf("read records runner TMPDIR: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("records runner TMPDIR entries = %v, want none", entries)
	}
}
