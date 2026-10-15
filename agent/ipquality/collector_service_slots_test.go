package ipquality

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type collectorSlotsTransport func(*http.Request) (*http.Response, error)

func (f collectorSlotsTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestServiceSemaphoreSharesSevenSlotsAcrossFamilies(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(func() {
		cancel()
		unblock()
	})
	started := make(chan int, 8)
	var mu sync.Mutex
	active, peak := 0, 0
	requests := [2]int{}
	client := func(index int, region string) *http.Client {
		return &http.Client{Transport: collectorSlotsTransport(func(request *http.Request) (*http.Response, error) {
			if request.URL.Host != "www.reddit.com" {
				return nil, fmt.Errorf("unexpected retained-service endpoint %s", request.URL)
			}
			mu.Lock()
			active++
			peak = max(peak, active)
			requests[index]++
			mu.Unlock()
			defer func() {
				mu.Lock()
				active--
				mu.Unlock()
			}()
			started <- index
			select {
			case <-release:
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/html"}},
				Body:       io.NopCloser(strings.NewReader(fmt.Sprintf(`<html><shreddit-app country="%s"></shreddit-app></html>`, region))),
				Request:    request,
			}, nil
		})}
	}
	collector := NewHTTPCollector(HTTPCollectorOptions{Client4: client(0, "US"), Client6: client(1, "JP")})
	slots := make(chan struct{}, 7)
	type familyResult struct {
		family   int
		outcomes []serviceProbeOutcome
	}
	done := make(chan familyResult, 2)
	// Public collection deduplicates service names. Call its actual scheduling
	// helper directly with four retained-service HTTP jobs per family so all
	// seven slots can be occupied without faking successful script execution.
	for _, family := range []int{4, 6} {
		go func(family int) {
			target := "198.51.100.10"
			if family == 6 {
				target = "2001:db8::10"
			}
			outcomes := collector.collectDefaultServiceUnlocksWithSemaphore(ctx, []string{"reddit", "reddit", "reddit", "reddit"}, family, slots, target)
			done <- familyResult{family: family, outcomes: outcomes}
		}(family)
	}
	firstFamilies := [2]int{}
	for range 7 {
		select {
		case index := <-started:
			firstFamilies[index]++
		case <-time.After(time.Second):
			t.Fatal("shared service pool did not occupy all seven slots")
		}
	}
	if firstFamilies[0] == 0 || firstFamilies[1] == 0 {
		t.Fatalf("seven occupied slots must span both families: %v", firstFamilies)
	}
	select {
	case <-started:
		t.Fatal("eighth HTTP job escaped the shared seven-slot limit")
	case <-time.After(50 * time.Millisecond):
	}
	unblock()
	for range 2 {
		select {
		case result := <-done:
			region := "US"
			if result.family == 6 {
				region = "JP"
			}
			if len(result.outcomes) != 4 {
				t.Fatalf("family %d outcomes=%#v", result.family, result.outcomes)
			}
			for _, outcome := range result.outcomes {
				if outcome.Result.Service != "reddit" || outcome.Result.Status != "unlocked" || outcome.Result.ProbeStatus != "success" || outcome.Result.Region != region {
					t.Fatalf("family %d lost real retained-service result: %#v", result.family, outcome)
				}
			}
		case <-time.After(time.Second):
			t.Fatal("service workers were not joined after releasing HTTP requests")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if peak != 7 || active != 0 || requests != [2]int{4, 4} || len(slots) != 0 {
		t.Fatalf("shared pool peak/active=%d/%d requests=%v occupied=%d", peak, active, requests, len(slots))
	}
}
