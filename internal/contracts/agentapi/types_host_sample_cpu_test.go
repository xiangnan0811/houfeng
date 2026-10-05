package agentapi_test

import (
	"math"
	"testing"

	"houfeng/internal/contracts/agentapi"
)

func TestCPURatesUsable(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name   string
		valid  *bool
		values [3]float64
		want   bool
	}{
		{"legacy zero", nil, [3]float64{0, 0, 0}, true},
		{"valid boundaries", &yes, [3]float64{100, 0, 100}, true},
		{"missing zero", &no, [3]float64{0, 0, 0}, false},
		{"missing nonzero", &no, [3]float64{20, 3, 1}, false},
		{"negative usage", nil, [3]float64{-1, 0, 0}, false},
		{"large iowait", &yes, [3]float64{0, 101, 0}, false},
		{"nan steal", nil, [3]float64{0, 0, math.NaN()}, false},
		{"positive infinity", &yes, [3]float64{math.Inf(1), 0, 0}, false},
		{"negative infinity", nil, [3]float64{0, math.Inf(-1), 0}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := agentapi.CPURatesUsable(tc.valid, tc.values[0], tc.values[1], tc.values[2]); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
