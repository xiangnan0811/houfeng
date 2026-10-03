package ipquality

import (
	"testing"
	"time"
)

func TestCollectionBudgetsPartitionTotalWithoutMinimumFloor(t *testing.T) {
	tests := []struct {
		name      string
		total     time.Duration
		canonical time.Duration
		dependent time.Duration
		services  time.Duration
	}{
		{name: "fifteen seconds", total: 15 * time.Second, canonical: 5 * time.Second, dependent: 5 * time.Second, services: 5 * time.Second},
		{name: "one second remainder", total: time.Second, canonical: time.Second / 3, dependent: time.Second / 3, services: time.Second - (time.Second/3)*2},
		{name: "sub millisecond no floor", total: 2 * time.Nanosecond, canonical: 0, dependent: 0, services: 2 * time.Nanosecond},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			canonical, dependent, services := collectionBudgets(test.total)
			if canonical != test.canonical || dependent != test.dependent || services != test.services {
				t.Fatalf("budgets = (%s,%s,%s), want (%s,%s,%s)", canonical, dependent, services, test.canonical, test.dependent, test.services)
			}
			if canonical+dependent+services != test.total {
				t.Fatalf("budget sum = %s, want %s", canonical+dependent+services, test.total)
			}
		})
	}
}
