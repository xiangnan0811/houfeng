package store

import (
	"encoding/json"
	"math"
	"testing"

	"houfeng/internal/center/runtimefacts"
)

func TestScanHostSampleNormalizesNonFiniteCPUValuesForJSON(t *testing.T) {
	t.Parallel()

	values := fakeHostSampleValues{
		CPUUsagePct:       math.NaN(),
		CPUIOWaitPct:      42,
		CPUStealPct:       math.Inf(1),
		MemUsedPct:        55,
		NetworkRatesValid: new(true),
	}
	var sample runtimefacts.HostSample
	if err := scanHostSample(fakeRuntimeFactsHostSampleRow(values), &sample); err != nil {
		t.Fatalf("scanHostSample() error = %v", err)
	}
	if sample.CPURatesValid == nil || *sample.CPURatesValid {
		t.Fatalf("CPURatesValid = %v, want explicit false", sample.CPURatesValid)
	}
	if sample.CPUUsagePct != 0 || sample.CPUStealPct != 0 {
		t.Fatalf("non-finite CPU values = usage %v steal %v, want zero placeholders", sample.CPUUsagePct, sample.CPUStealPct)
	}
	if sample.CPUIOWaitPct != 42 {
		t.Fatalf("finite CPU value = %v, want 42", sample.CPUIOWaitPct)
	}
	if sample.MemUsedPct != values.MemUsedPct {
		t.Fatalf("non-CPU value = %v, want %v", sample.MemUsedPct, values.MemUsedPct)
	}
	if _, err := json.Marshal(sample); err != nil {
		t.Fatalf("json.Marshal(normalized sample) error = %v", err)
	}
	if !math.IsNaN(values.CPUUsagePct) || !math.IsInf(values.CPUStealPct, 1) {
		t.Fatalf("source values were mutated: %#v", values)
	}
}

func TestScanHostSampleMarksFiniteOutOfRangeCPUFalseWithoutRewriting(t *testing.T) {
	t.Parallel()

	values := fakeHostSampleValues{CPUUsagePct: 101, CPURatesValid: new(true), CPUIOWaitPct: 2, CPUStealPct: 3}
	var sample runtimefacts.HostSample
	if err := scanHostSample(fakeRuntimeFactsHostSampleRow(values), &sample); err != nil {
		t.Fatalf("scanHostSample() error = %v", err)
	}
	if sample.CPURatesValid == nil || *sample.CPURatesValid {
		t.Fatalf("CPURatesValid = %v, want explicit false", sample.CPURatesValid)
	}
	if sample.CPUUsagePct != 101 {
		t.Fatalf("out-of-range CPU value = %v, want original 101", sample.CPUUsagePct)
	}
}
