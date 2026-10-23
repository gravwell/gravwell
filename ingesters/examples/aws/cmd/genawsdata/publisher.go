package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Publisher sends events to a specific service resource.
type Publisher interface {
	// Publish sends events to the named resource and returns the result.
	Publish(ctx context.Context, resource string, events []Event) (Result, error)

	// ServiceName returns the display name of the service (e.g. "S3", "SQS", "Kinesis").
	ServiceName() string
}

// Result tracks the outcome of publishing events to a single resource.
type Result struct {
	Service  string
	Resource string
	Sent     int
	Failed   int
	Err      error
}

// Summary holds all results from a run. It is safe for concurrent use.
type Summary struct {
	mu      sync.Mutex
	Results []Result
}

// Add appends a result to the summary.
func (s *Summary) Add(r Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Results = append(s.Results, r)
}

// String formats the summary for display.
func (s *Summary) String() string {
	if len(s.Results) == 0 {
		return "No results."
	}

	var b strings.Builder
	b.WriteString("\n=== Results ===\n")

	// Group by service.
	services := make(map[string][]Result)
	var order []string
	for _, r := range s.Results {
		if _, ok := services[r.Service]; !ok {
			order = append(order, r.Service)
		}
		services[r.Service] = append(services[r.Service], r)
	}

	for _, svc := range order {
		results := services[svc]
		b.WriteString("\n  ")
		b.WriteString(svc)
		b.WriteString(":\n")
		for _, r := range results {
			status := "✓"
			detail := fmt.Sprintf("%d sent", r.Sent)
			if r.Err != nil {
				status = "✗"
				detail = fmt.Sprintf("error: %v", r.Err)
			} else if r.Failed > 0 {
				status = "!"
				detail = fmt.Sprintf("%d sent, %d failed", r.Sent, r.Failed)
			}
			fmt.Fprintf(&b, "    %s %s — %s\n", status, r.Resource, detail)
		}
	}

	return b.String()
}
